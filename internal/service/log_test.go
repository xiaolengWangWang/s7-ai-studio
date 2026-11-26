package service

import (
	"context"
	"errors"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/s7"
	"s7-ai-studio/internal/simulator"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogSnapshotAndArchive(t *testing.T) {
	c := newController(t, Options{})
	_, target := peer(t, simulator.Fault{})
	connect(t, c, target)
	if err := c.Read(context.Background(), []Point{point}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	id := c.s.id
	for i := 0; i < 310; i++ {
		c.logLocked(LogInfo, LogSystem, "test", "", device.Event{})
	}
	c.mu.Unlock()
	snap := c.Snapshot()
	if len(snap.Logs) != 300 || snap.Logs[0].ID >= snap.Logs[299].ID {
		t.Fatalf("bounded IDs: %+v", snap.Logs)
	}
	snap.Logs[0].Message = "changed"
	if c.Snapshot().Logs[0].Message == "changed" {
		t.Fatal("snapshot aliases logs")
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	events, err := c.rec.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
	}
	if !kinds[recorder.EventConnect] || !kinds["SESSION_END"] {
		t.Fatalf("events: %+v", events)
	}
}

type recoverableDriver struct{ reads atomic.Int32 }

func (d *recoverableDriver) Capabilities() device.Capabilities { return device.Capabilities{} }
func (d *recoverableDriver) Close() error                      { return nil }
func (d *recoverableDriver) IsClosed() bool                    { return false }
func (d *recoverableDriver) Write(context.Context, device.Address, device.Value) (device.WriteResult, error) {
	return device.WriteResult{}, nil
}
func (d *recoverableDriver) Read(_ context.Context, aa []device.Address) ([]device.ReadResult, error) {
	if d.reads.Add(1) == 1 {
		return nil, errors.New("temporary error")
	}
	return []device.ReadResult{{Address: aa[0], Quality: "GOOD"}}, nil
}
func TestPollingRecoversAndArchivesStopOnDisconnect(t *testing.T) {
	d := &recoverableDriver{}
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) { return d, nil }})
	connect(t, c, "127.0.0.1:102")
	c.mu.Lock()
	id := c.s.id
	c.mu.Unlock()
	if err := c.StartPolling(context.Background(), []Point{point}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool {
		for _, l := range c.Snapshot().Logs {
			if l.Message == "轮询读取已恢复" {
				return true
			}
		}
		return false
	})
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	events, err := c.rec.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
	}
	if !kinds[recorder.EventReadFail] || !kinds[recorder.EventReadOK] || !kinds["POLL_STOP"] || kinds[recorder.EventDisconnect] {
		t.Fatalf("events: %+v", events)
	}
}

func TestPollingLogsOneWarningWithResponseForRepeatedPointError(t *testing.T) {
	srv, target := peer(t, simulator.Fault{})
	c := newController(t, Options{})
	connect(t, c, target)
	c.mu.Lock()
	id := c.s.id
	c.mu.Unlock()
	if err := c.StartPolling(context.Background(), []Point{{Address: "DB999.DBD0", Type: "DINT", Count: 1}}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return srv.Stats().Reads >= 3 })
	c.StopPolling()
	var warnings []LogEntry
	for _, entry := range c.Snapshot().Logs {
		if entry.Category == LogPolling && entry.Level == LogWarning {
			warnings = append(warnings, entry)
		}
	}
	if len(warnings) != 1 || warnings[0].RequestID == 0 || warnings[0].ConnectionID == "" || !strings.Contains(warnings[0].Message, "DB999.DBD0") {
		t.Fatalf("polling warning missing response or duplicated: %+v", warnings)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	events, err := c.rec.Events(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == recorder.EventReadFail && len(event.RX) > 0 && strings.Contains(event.Analysis, warnings[0].ConnectionID) {
			return
		}
	}
	t.Fatalf("archive missing linked response: %+v", events)
}
