package service

import (
	"context"
	"testing"
	"time"

	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/s7"
	"s7-ai-studio/internal/simulator"
)

func TestReadEachPublishesProgressAndContinuesAfterBadPoint(t *testing.T) {
	srv, target := peer(t, simulator.Fault{Delay: 200 * time.Millisecond})
	c := newController(t, Options{})
	cfg := DefaultConfig()
	cfg.Target = target
	if err := c.Connect(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- c.ReadEach(context.Background(), []Point{point, {Address: "DB9.DBD0", Type: "DINT", Count: 1}, {Address: "MD0", Type: "DINT", Count: 1}})
	}()
	until(t, func() bool {
		s := c.Snapshot()
		return s.ReadingEach && s.ReadCompleted == 1 && len(s.Results) == 1 && s.Results[0].Quality == "GOOD"
	})
	if s := c.Snapshot(); s.ReadTotal != 3 {
		t.Fatalf("progress total: %+v", s)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.ReadingEach || s.ReadCompleted != 3 || len(s.Results) != 3 || s.Results[0].Quality != "GOOD" || s.Results[1].Quality != "BAD" || s.Results[2].Quality != "GOOD" || srv.Stats().Reads != 3 || s.State != "connected" {
		t.Fatalf("final state: %+v, requests=%+v", s, srv.Stats())
	}
	if err := c.Read(context.Background(), []Point{point}); err != nil {
		t.Fatal(err)
	}
	if s = c.Snapshot(); s.ReadTotal != 0 || s.ReadingEach {
		t.Fatalf("ordinary read retained verification progress: %+v", s)
	}
}

type interruptedPointDriver struct {
	blockingDriver
	second chan struct{}
}

func (d *interruptedPointDriver) Read(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	if d.reads.Add(1) == 1 {
		return []device.ReadResult{{Address: aa[0], Quality: "GOOD", Value: device.Value{Type: "DINT", Integer: "42"}, Time: time.Now()}}, nil
	}
	close(d.second)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestReadEachDisconnectPreservesPublishedResults(t *testing.T) {
	d := &interruptedPointDriver{second: make(chan struct{})}
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) { return d, nil }})
	connect(t, c, "127.0.0.1:102")
	done := make(chan error, 1)
	go func() { done <- c.ReadEach(context.Background(), []Point{point, point, point}) }()
	select {
	case <-d.second:
	case <-time.After(3 * time.Second):
		t.Fatal("second read did not start")
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("disconnected verification reported success")
	}
	s := c.Snapshot()
	if s.State != "disconnected" || s.ReadingEach || s.ReadCompleted != 1 || len(s.Results) != 1 || s.Results[0].Quality != "GOOD" || d.reads.Load() != 2 {
		t.Fatalf("completed result lost or late result published: %+v", s)
	}
}

func TestStoppedPollingCannotOverwritePointVerification(t *testing.T) {
	srv, target := peer(t, simulator.Fault{})
	c := newController(t, Options{})
	connect(t, c, target)
	if err := c.ReadEach(context.Background(), []Point{point, {Address: "MD0", Type: "DINT", Count: 1}}); err != nil {
		t.Fatal(err)
	}
	before := srv.Stats().Reads
	// A poll already waiting for the session gate still carries this generation.
	_ = c.read(context.Background(), []Point{point}, c.Snapshot().Generation)
	s := c.Snapshot()
	if srv.Stats().Reads != before || len(s.Results) != 2 || s.ReadCompleted != 2 || s.ReadTotal != 2 {
		t.Fatalf("stopped poll replaced verification: %+v, requests %d -> %d", s, before, srv.Stats().Reads)
	}
}
