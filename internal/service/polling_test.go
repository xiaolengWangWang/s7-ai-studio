package service

import (
	"context"
	"testing"
	"time"

	"s7-ai-studio/internal/simulator"
)

func TestStopPollingKeepsConnection(t *testing.T) {
	srv, target := peer(t, simulator.Fault{Delay: 400 * time.Millisecond})
	c := newController(t, Options{})
	cfg := DefaultConfig()
	cfg.Target = target
	if err := c.Connect(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := c.StartPolling(context.Background(), []Point{point}, time.Second); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return srv.Stats().Reads > 0 })
	c.StopPolling()
	until(t, func() bool { return len(c.Snapshot().Results) == 1 || c.Snapshot().State == "error" })
	s := c.Snapshot()
	if s.Polling || s.State != "connected" {
		t.Fatalf("stop invalidated healthy session: %+v", s)
	}
	reads := srv.Stats().Reads
	time.Sleep(200 * time.Millisecond)
	if got := srv.Stats().Reads; got != reads {
		t.Fatalf("polling continued after stop: %d -> %d", reads, got)
	}
	if err := c.Read(context.Background(), []Point{point}); err != nil {
		t.Fatal("same session unusable after stop", err)
	}
}
