package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/profile"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/s7"
	"s7-ai-studio/internal/simulator"
	"sync"
	"time"
)

type Options struct {
	RecordDir string
	Dial      func(context.Context, string, s7.Options) (device.Driver, error)
}
type Snapshot struct {
	Revision, Generation                                                           uint64
	State, Target, Family, DemoTarget, LastError, RecordDir, RecordError, Readback string
	Polling                                                                        bool
	ReadingEach                                                                    bool
	ReadCompleted, ReadTotal                                                       int
	Results                                                                        []device.ReadResult
	WriteResult                                                                    *device.WriteResult
	Packets                                                                        []device.Event
	ConnectionPackets                                                              []device.Event
	Connection                                                                     ConnectionInfo
	Logs                                                                           []LogEntry
	Dropped                                                                        int64
}
type session struct {
	ctx       context.Context
	cancel    context.CancelFunc
	d         device.Driver
	gate      chan struct{}
	wg        sync.WaitGroup
	end       sync.Once
	id        int64
	cfg       Config
	p         profile.Profile
	readFault device.Event
}
type Controller struct {
	mu          sync.Mutex
	ops         sync.WaitGroup
	closed      bool
	once        sync.Once
	closeErr    error
	snap        Snapshot
	s           *session
	dial        func(context.Context, string, s7.Options) (device.Driver, error)
	rec         *recorder.Recorder
	pollCancel  context.CancelFunc
	pollSeq     uint64
	demo        *simulator.Server
	demoDone    chan struct{}
	logID       uint64
	pollFailure string
}

func New(o Options) *Controller {
	if o.RecordDir == "" {
		dir, e := os.UserConfigDir()
		if e == nil {
			o.RecordDir = filepath.Join(dir, "S7AIStudio", "records")
		}
	}
	c := &Controller{snap: Snapshot{Revision: 1, State: "disconnected", RecordDir: o.RecordDir}, dial: o.Dial}
	if c.dial == nil {
		c.dial = func(ctx context.Context, target string, o s7.Options) (device.Driver, error) {
			return s7.Dial(ctx, target, o)
		}
	}
	var e error
	if o.RecordDir == "" {
		e = fmt.Errorf("无法确定记录目录")
	} else {
		c.rec, e = recorder.OpenDaily(o.RecordDir)
	}
	if e != nil {
		c.recordErrorLocked(e)
	}
	return c
}
func (c *Controller) begin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("程序已关闭")
	}
	c.ops.Add(1)
	return nil
}
func (c *Controller) healthLocked() {
	if c.rec != nil {
		d, e := c.rec.Health()
		c.recordErrorLocked(e)
		if d != c.snap.Dropped {
			firstDrop := c.snap.Dropped == 0 && d > 0
			c.snap.Dropped = d
			if d > 0 && e == nil {
				msg := fmt.Sprintf("记录丢弃 %d 条报文", d)
				c.snap.RecordError = msg
				if firstDrop {
					c.logLocked(LogWarning, LogSystem, msg, "", device.Event{})
				}
			}
			c.snap.Revision++
		}
	}
}
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthLocked()
	s := c.snap
	s.Logs = append([]LogEntry(nil), s.Logs...)
	s.Packets = clonePackets(c.snap.Packets)
	s.ConnectionPackets = clonePackets(c.snap.ConnectionPackets)
	s.Results = make([]device.ReadResult, len(c.snap.Results))
	for i, r := range c.snap.Results {
		e := device.Event{Results: []device.EventResult{{Read: &r}}}
		s.Results[i] = *e.Clone().Results[0].Read
	}
	if s.WriteResult != nil {
		r := *s.WriteResult
		r.Segments = append([]device.WriteSegment(nil), r.Segments...)
		s.WriteResult = &r
	}
	return s
}
func clonePackets(packets []device.Event) []device.Event {
	if packets == nil {
		return nil
	}
	out := make([]device.Event, len(packets))
	for i, packet := range packets {
		out[i] = packet.Clone()
	}
	return out
}
func (c *Controller) recordError(e error) {
	if e == nil {
		return
	}
	c.mu.Lock()
	c.recordErrorLocked(e)
	c.mu.Unlock()
}
func (c *Controller) end(s *session) {
	if s == nil {
		return
	}
	s.end.Do(func() {
		if c.rec != nil && s.id != 0 {
			c.recordError(c.rec.EndSession(s.id))
		}
	})
}
func (c *Controller) stopPollLocked() {
	if c.pollCancel != nil {
		c.logLocked(LogInfo, LogPolling, "轮询已停止", "POLL_STOP", device.Event{})
		c.pollCancel()
		c.pollCancel = nil
	}
	c.pollSeq++
	c.snap.Polling = false
	c.snap.Revision++
}
func (c *Controller) detachLocked(state string) *session {
	s := c.s
	c.stopPollLocked()
	c.s = nil
	c.snap.Generation++
	c.snap.State = state
	c.snap.Connection = ConnectionInfo{}
	c.snap.ReadingEach = false
	c.snap.Revision++
	if s != nil {
		s.cancel()
	}
	return s
}
func (c *Controller) finishSession(s *session) {
	if s == nil {
		return
	}
	if s.d != nil {
		s.d.Close()
	}
	s.wg.Wait()
	c.end(s)
}
func (c *Controller) Disconnect() error {
	if e := c.begin(); e != nil {
		return e
	}
	defer c.ops.Done()
	c.mu.Lock()
	c.logLocked(LogInfo, LogConnection, "已断开连接", "SESSION_END", device.Event{})
	s := c.detachLocked("disconnected")
	c.mu.Unlock()
	c.finishSession(s)
	return nil
}
func (c *Controller) Connect(ctx context.Context, cfg Config) error {
	p, o, e := cfg.validate()
	if e != nil {
		return e
	}
	if e = c.begin(); e != nil {
		return e
	}
	defer c.ops.Done()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("程序已关闭")
	}
	old := c.detachLocked("connecting")
	sctx, cancel := context.WithCancel(ctx)
	s := &session{ctx: sctx, cancel: cancel, gate: make(chan struct{}, 1), cfg: cfg, p: p}
	s.gate <- struct{}{}
	s.wg.Add(1)
	defer s.wg.Done()
	if c.rec != nil {
		s.id, e = c.rec.StartSession(device.ProtocolS7TCP, cfg.Target, 1)
		c.recordErrorLocked(e)
	}
	c.s = s
	c.snap.Target = cfg.Target
	c.snap.Family = p.Family
	c.snap.LastError = ""
	c.snap.Results = nil
	c.snap.ReadCompleted, c.snap.ReadTotal = 0, 0
	c.snap.WriteResult = nil
	c.snap.Readback = ""
	connection := connectionInfo(cfg, p, o)
	c.logLocked(LogInfo, LogConnection, fmt.Sprintf("正在连接 %s · %s · %s", cfg.Target, p.Family, connection.endpoint()), "", device.Event{})
	c.mu.Unlock()
	c.finishSession(old)
	o.Observer = device.ObserverFunc(func(event device.Event) {
		event.Family = p.Family
		if c.rec != nil && s.id != 0 {
			c.rec.Record(s.id, event)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.s != s {
			return
		}
		packet := event.Clone()
		c.snap.Packets = appendRecentPacket(c.snap.Packets, packet, 256)
		switch event.Operation {
		case "CONNECT", "SETUP", "CONNECTION_STATUS":
			c.snap.ConnectionPackets = appendRecentPacket(c.snap.ConnectionPackets, packet, 64)
		}
		if event.Dir == device.DirRX {
			if stage := handshakeStage(event.Operation); stage != "" {
				status := "成功"
				if event.Status != device.StatusSuccess {
					status = fmt.Sprintf("失败（%s）", event.Status)
					if event.Error != "" {
						status += " " + event.Error
					}
				}
				level := LogInfo
				if event.Status != device.StatusSuccess {
					level = LogWarning
				}
				c.logLocked(level, LogConnection, fmt.Sprintf("%s %s · 请求 #%d · RX %d 字节 · %s", stage, status, event.RequestID, len(event.Raw), event.RTT.Round(time.Microsecond)), "", event)
			} else if event.Status != device.StatusSuccess && event.Status != device.StatusCancelled {
				category := LogDiagnostic
				switch event.Operation {
				case "READ":
					s.readFault = packet
					category = LogRead
				case "WRITE":
					category = LogWrite
				}
				if category != LogRead || !c.snap.Polling {
					msg := fmt.Sprintf("%s 响应异常 · %s", event.Operation, event.Status)
					if event.Error != "" {
						msg += " · " + event.Error
					}
					c.logLocked(LogWarning, category, msg, "", event)
				}
			}
		}
		c.snap.Revision++
	})
	d, e := c.dial(sctx, cfg.Target, o)
	c.mu.Lock()
	if c.closed || c.s != s {
		c.mu.Unlock()
		if d != nil {
			d.Close()
		}
		return context.Canceled
	}
	if e != nil {
		c.snap.State = "error"
		c.snap.LastError = e.Error()
		c.logLocked(LogError, LogConnection, "连接失败: "+e.Error(), recorder.EventConnectFail, device.Event{})
		s.cancel()
		c.mu.Unlock()
		c.end(s)
		return e
	}
	s.d = d
	c.snap.State = "connected"
	connection.PDULength = d.Capabilities().PDULength
	c.snap.Connection = connection
	c.logLocked(LogInfo, LogConnection, fmt.Sprintf("已连接 %s · 协商 PDU %d 字节", cfg.Target, connection.PDULength), recorder.EventConnect, device.Event{})
	c.mu.Unlock()
	return nil
}
func (c *Controller) acquire(ctx context.Context, generation uint64) (*session, context.Context, func(), error) {
	c.mu.Lock()
	s := c.s
	if c.closed || s == nil || s.d == nil || c.snap.State != "connected" {
		c.mu.Unlock()
		return nil, nil, nil, errors.New("请先连接")
	}
	if generation != 0 && generation != c.snap.Generation {
		c.mu.Unlock()
		return nil, nil, nil, errors.New("连接已变化，请重新确认写入")
	}
	c.ops.Add(1)
	s.wg.Add(1)
	c.mu.Unlock()
	opctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	done := func() { stop(); cancel(); s.wg.Done(); c.ops.Done() }
	select {
	case <-opctx.Done():
		done()
		return nil, nil, nil, opctx.Err()
	case <-s.gate:
	}
	c.mu.Lock()
	valid := !c.closed && c.s == s && c.snap.State == "connected"
	c.mu.Unlock()
	if !valid || opctx.Err() != nil {
		s.gate <- struct{}{}
		done()
		return nil, nil, nil, context.Canceled
	}
	return s, opctx, func() { s.gate <- struct{}{}; done() }, nil
}
func (c *Controller) failure(s *session, e error) {
	if e == nil {
		return
	}
	if state, ok := s.d.(interface{ IsClosed() bool }); ok && !state.IsClosed() {
		return
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.State = "error"
		c.snap.LastError = e.Error()
		c.stopPollLocked()
		c.logLocked(LogError, LogConnection, "通讯失败，需重新连接: "+e.Error(), recorder.EventDisconnect, device.Event{})
		s.cancel()
	}
	c.mu.Unlock()
	s.d.Close()
}
func (c *Controller) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		s := c.detachLocked("closed")
		demo, done := c.demo, c.demoDone
		c.demo = nil
		c.snap.DemoTarget = ""
		c.mu.Unlock()
		c.finishSession(s)
		if demo != nil {
			demo.Close()
			<-done
		}
		c.ops.Wait()
		if c.rec != nil {
			c.closeErr = c.rec.Close()
		}
		c.mu.Lock()
		c.healthLocked()
		if c.snap.RecordError != "" {
			c.closeErr = errors.Join(c.closeErr, errors.New(c.snap.RecordError))
		}
		c.mu.Unlock()
	})
	return c.closeErr
}
