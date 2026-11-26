package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"sort"
	"strings"
	"time"
)

func (c *Controller) Read(ctx context.Context, points []Point) error {
	return c.read(ctx, points, 0)
}

// ReadEach validates an imported point table one address at a time.
func (c *Controller) ReadEach(ctx context.Context, points []Point) error {
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	aa, err := resolve(s.p, points)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.stopPollLocked()
		s.readFault = device.Event{}
		c.snap.Results = nil
		c.snap.LastError = ""
		c.snap.ReadingEach = true
		c.snap.ReadCompleted = 0
		c.snap.ReadTotal = len(aa)
		c.snap.Revision++
	}
	c.mu.Unlock()
	rr, err := device.ReadEach(ctx, s.d, aa, func(r device.ReadResult) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.s == s && !c.closed {
			c.snap.Results = append(c.snap.Results, r)
			c.snap.ReadCompleted++
			c.snap.Revision++
		}
	})
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.Results = rr
		c.snap.ReadingEach = false
		if err != nil {
			c.snap.LastError = err.Error()
		}
		state := "完成"
		if err != nil {
			state = "中断"
		}
		issues := readIssues(rr)
		msg := fmt.Sprintf("逐点读取%s %d/%d · 异常 %d 点", state, c.snap.ReadCompleted, c.snap.ReadTotal, len(issues))
		level, kind := LogInfo, ""
		if err != nil || len(issues) > 0 {
			level, kind = LogWarning, recorder.EventReadFail
		}
		if err != nil {
			msg += " · " + err.Error()
		}
		c.logLocked(level, LogRead, msg, kind, s.readFault)
	}
	c.mu.Unlock()
	if err != nil {
		c.failure(s, err)
	}
	return err
}

func (c *Controller) read(ctx context.Context, points []Point, generation uint64) error {
	return c.readWithPollSeq(ctx, points, generation, 0)
}
func (c *Controller) readWithPollSeq(ctx context.Context, points []Point, generation, seq uint64) error {
	s, ctx, done, e := c.acquire(ctx, generation)
	if e != nil {
		return e
	}
	defer done()
	aa, e := resolve(s.p, points)
	if e != nil {
		return e
	}
	c.mu.Lock()
	if generation != 0 && (!c.snap.Polling || (seq != 0 && c.pollSeq != seq)) {
		c.mu.Unlock()
		return nil // A stopped poll may have been waiting behind point verification.
	}
	if c.s == s && !c.closed {
		s.readFault = device.Event{}
		c.snap.ReadingEach = false
		c.snap.ReadCompleted, c.snap.ReadTotal = 0, 0
		c.snap.Revision++
	}
	c.mu.Unlock()
	rr, e := s.d.Read(ctx, aa)
	c.mu.Lock()
	if c.s == s && !c.closed && (generation == 0 || (seq == 0 && c.snap.Polling) || (seq != 0 && (c.pollSeq == seq || (!c.snap.Polling && c.pollSeq == seq+1)))) {
		c.snap.Results = rr
		c.snap.LastError = ""
		if e != nil {
			c.snap.LastError = e.Error()
		}
		failures := readIssues(rr)
		msg := fmt.Sprintf("读取 %d 点，异常 %d 点", len(aa), len(failures))
		if e != nil {
			msg = "读取失败: " + e.Error()
		}
		if generation == 0 {
			level, kind := LogInfo, ""
			if e != nil || len(failures) > 0 {
				level, kind = LogWarning, recorder.EventReadFail
			}
			c.logLocked(level, LogRead, msg, kind, s.readFault)
		} else if e != nil || len(failures) > 0 {
			key := msg + "|" + strings.Join(failures, "|")
			if key != c.pollFailure {
				c.pollFailure = key
				if len(failures) > 0 {
					msg += " · " + strings.Join(failures, "; ")
				}
				c.logLocked(LogWarning, LogPolling, msg, recorder.EventReadFail, s.readFault)
			}
		} else if c.pollFailure != "" {
			c.pollFailure = ""
			c.logLocked(LogInfo, LogPolling, "轮询读取已恢复", recorder.EventReadOK, device.Event{})
		}
		c.snap.Revision++
	}
	c.mu.Unlock()
	if e != nil {
		c.mu.Lock()
		current := seq == 0 || c.pollSeq == seq || (!c.snap.Polling && c.pollSeq == seq+1)
		c.mu.Unlock()
		if current {
			c.failure(s, e)
		}
	}
	return e
}

func readIssues(results []device.ReadResult) []string {
	var failures []string
	for _, r := range results {
		if r.Quality == "GOOD" {
			continue
		}
		address := r.Address.Original
		if address == "" {
			address = r.Address.String()
		}
		failures = append(failures, strings.TrimSpace(fmt.Sprintf("%s · %s %s", address, r.Quality, r.Error)))
	}
	sort.Strings(failures)
	return failures
}
func (c *Controller) Write(ctx context.Context, req WriteRequest, approved bool) error {
	if !approved {
		return errors.New("写入默认禁用，须明确允许并确认")
	}
	if req.Generation == 0 {
		return errors.New("写入须绑定已确认的连接")
	}
	s, ctx, done, e := c.acquire(ctx, req.Generation)
	if e != nil {
		return e
	}
	defer done()
	aa, e := resolve(s.p, []Point{req.Point})
	if e != nil {
		return e
	}
	a := aa[0]
	v, e := device.ParseValue(a, req.Value)
	if e != nil {
		return e
	}
	want, _ := device.EncodeValue(a, v)
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.ReadCompleted, c.snap.ReadTotal = 0, 0
		c.snap.Revision++
	}
	c.mu.Unlock()
	result, e := s.d.Write(ctx, a, v)
	readback := "无法确认"
	var rr []device.ReadResult
	writeErr := e
	if result.State == "CONFIRMED" {
		rr, e = s.d.Read(ctx, []device.Address{a})
		if e == nil && len(rr) == 1 && rr[0].Quality == "GOOD" {
			got, encodeErr := device.EncodeValue(a, rr[0].Value)
			if encodeErr == nil {
				readback = "不一致"
				if bytes.Equal(want, got) {
					readback = "一致"
				}
			}
		}
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.WriteResult = &result
		c.snap.Readback = readback
		if len(rr) > 0 {
			c.snap.Results = rr
		}
		c.snap.LastError = ""
		if e != nil {
			c.snap.LastError = e.Error()
		}
		level := LogInfo
		if e != nil || result.State != "CONFIRMED" || readback != "一致" {
			level = LogWarning
		}
		msg := fmt.Sprintf("写入 %s · %s · 读回%s", req.Point.Address, result.State, readback)
		if result.Error != "" {
			msg += " · " + result.Error
		}
		c.logLocked(level, LogWrite, msg, "WRITE", device.Event{})
	}
	c.mu.Unlock()
	if e != nil {
		c.failure(s, e)
	}
	return errors.Join(writeErr, e)
}
func (c *Controller) StartPolling(ctx context.Context, points []Point, interval time.Duration) error {
	if interval < 100*time.Millisecond {
		return errors.New("轮询间隔不得小于 100 ms")
	}
	c.mu.Lock()
	if c.closed || c.s == nil || c.snap.State != "connected" {
		c.mu.Unlock()
		return errors.New("请先连接")
	}
	if _, e := resolve(c.s.p, points); e != nil {
		c.mu.Unlock()
		return e
	}
	c.stopPollLocked()
	pctx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	seq, gen := c.pollSeq, c.snap.Generation
	c.snap.Polling = true
	c.pollFailure = ""
	c.logLocked(LogInfo, LogPolling, fmt.Sprintf("轮询已启动 · %d 点 · 间隔 %s", len(points), interval), "POLL_START", device.Event{})
	c.snap.Revision++
	c.ops.Add(1)
	c.mu.Unlock()
	points = append([]Point(nil), points...)
	go func() {
		defer c.ops.Done()
		defer func() {
			c.mu.Lock()
			if c.pollSeq == seq {
				c.snap.Polling = false
				c.logLocked(LogInfo, LogPolling, "轮询已停止", "POLL_STOP", device.Event{})
				c.pollCancel = nil
				c.snap.Revision++
			}
			c.mu.Unlock()
		}()
		for {
			c.mu.Lock()
			valid := c.snap.Generation == gen && !c.closed
			c.mu.Unlock()
			if !valid || pctx.Err() != nil {
				return
			}
			// Stopping polling cancels scheduling, while an in-flight read may finish.
			// Disconnect and shutdown still cancel the session and its active read.
			if c.readWithPollSeq(ctx, points, gen, seq) != nil {
				c.mu.Lock()
				healthy := !c.closed && c.pollSeq == seq && c.snap.Generation == gen && c.snap.Polling && c.snap.State == "connected" && c.s != nil
				c.mu.Unlock()
				if !healthy || pctx.Err() != nil {
					return
				}
			}
			timer := time.NewTimer(interval)
			select {
			case <-pctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return nil
}
func (c *Controller) StopPolling() { c.mu.Lock(); c.stopPollLocked(); c.mu.Unlock() }
