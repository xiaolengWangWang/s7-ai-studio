package service

import (
	"context"
	"errors"
	"net"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/simulator"
)

func (c *Controller) StartDemo(ctx context.Context) error {
	if e := c.begin(); e != nil {
		return e
	}
	defer c.ops.Done()
	c.mu.Lock()
	if c.demo != nil {
		c.mu.Unlock()
		return errors.New("本机演示已启动")
	}
	c.mu.Unlock()
	mem := simulator.NewMemory()
	a, _ := device.ParseAddress("DB1.DBD0", "DINT", 1)
	if e := mem.Set(a, []byte{0, 0, 0, 42}); e != nil {
		return e
	}
	srv, e := simulator.New(simulator.Options{Memory: mem})
	if e != nil {
		return e
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	done := make(chan struct{})
	c.mu.Lock()
	if c.closed || c.demo != nil {
		c.mu.Unlock()
		l.Close()
		srv.Close()
		return context.Canceled
	}
	c.demo = srv
	c.demoDone = done
	c.snap.DemoTarget = l.Addr().String()
	c.logLocked("本机演示：合成协议对端 " + c.snap.DemoTarget)
	c.mu.Unlock()
	go func() { defer close(done); srv.Serve(context.Background(), l) }()
	cfg := DefaultConfig()
	cfg.Target = l.Addr().String()
	if e = c.Connect(ctx, cfg); e != nil {
		return e
	}
	return c.Read(ctx, []Point{{"DB1.DBD0", "DINT", 1}})
}
func (c *Controller) StopDemo() error {
	if e := c.begin(); e != nil {
		return e
	}
	defer c.ops.Done()
	c.mu.Lock()
	srv, done := c.demo, c.demoDone
	target := c.snap.DemoTarget
	var s *session
	if srv != nil && c.snap.Target == target {
		s = c.detachLocked("disconnected")
	}
	c.demo = nil
	c.snap.DemoTarget = ""
	c.logLocked("本机演示已停止")
	c.mu.Unlock()
	c.finishSession(s)
	if srv != nil {
		srv.Close()
		<-done
	}
	return nil
}
