package simulator

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSimulatorShutdown(t *testing.T) {
	srv, e := New(Options{})
	if e != nil {
		t.Fatal(e)
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	c, e := net.Dial("tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	cancel()
	srv.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve blocked after cancellation")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(make([]byte, 1)); e == nil {
		t.Fatal("connection not closed")
	}
}
func TestSimulatorRejectsInvalidOptions(t *testing.T) {
	for _, o := range []Options{{PDULimit: 1}, {PDULimit: 9999}, {Fault: Fault{Delay: -time.Second}}, {Fault: Fault{RejectArea: "INVALID"}}} {
		if _, e := New(o); e == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
}
