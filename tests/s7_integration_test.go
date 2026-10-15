package tests

import (
	"context"
	"net"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/s7"
	"s7-ai-studio/internal/simulator"
	"testing"
	"time"
)

func fixture(t *testing.T, opts simulator.Options) (*simulator.Server, *s7.Client) {
	t.Helper()
	srv, e := simulator.New(opts)
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
	o := s7.DefaultOptions()
	o.LocalTSAP, o.RemoteTSAP = 0x100, 0x102
	o.Timeout = 100 * time.Millisecond
	c, e := s7.Dial(ctx, ln.Addr().String(), o)
	if e != nil {
		cancel()
		srv.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c.Close()
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("simulator did not stop")
		}
	})
	return srv, c
}
func TestTCPReadWriteRoundTrip(t *testing.T) {
	_, c := fixture(t, simulator.Options{})
	a, _ := device.ParseAddress("DB1.DBD0", "DINT", 1)
	w, e := c.Write(context.Background(), a, device.Value{Type: "DINT", Integer: "42"})
	if e != nil || w.State != "CONFIRMED" {
		t.Fatalf("%+v %v", w, e)
	}
	r, e := c.Read(context.Background(), []device.Address{a})
	if e != nil || r[0].Quality != "GOOD" || r[0].Value.Integer != "42" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestTelegrafTypedRead(t *testing.T) {
	m := simulator.NewMemory()
	_, c := fixture(t, simulator.Options{Memory: m})
	cases := []struct {
		text string
		raw  []byte
	}{
		{"DB1.C0", []byte{0xe4}},
		{"DB1.LI8", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xd6}},
		{"DB1.LR16", []byte{0x3f, 0xf4, 0, 0, 0, 0, 0, 0}},
		{"DB1.S32.16", append([]byte{16, 2, 'O', 'K'}, make([]byte, 14)...)},
	}
	var addresses []device.Address
	for _, tc := range cases {
		kind, _ := device.PlatformType(tc.text)
		a, err := device.ParseAddress(tc.text, kind, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Set(a, tc.raw); err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, a)
	}
	got, err := c.Read(context.Background(), addresses)
	if err != nil || len(got) != 4 {
		t.Fatalf("read = %+v, %v", got, err)
	}
	if got[0].Quality != "GOOD" || got[0].Value.Text != "ä" || got[1].Value.Integer != "-42" || got[2].Value.Real != 1.25 || got[3].Value.Text != "OK" {
		t.Fatalf("read = %+v", got)
	}
}
func TestSmallPDUAndMixedDBs(t *testing.T) {
	_, c := fixture(t, simulator.Options{PDULimit: 128})
	a, _ := device.ParseAddress("DB1.DBW0", "INT", 100)
	v := device.Value{Type: "INT"}
	for i := 0; i < 100; i++ {
		v.Elements = append(v.Elements, device.Value{Type: "INT", Integer: "7"})
	}
	if _, e := c.Write(context.Background(), a, v); e != nil {
		t.Fatal(e)
	}
	b := a
	b.DBNumber = 2
	b.Count = 1
	missing := b
	missing.DBNumber = 9
	r, e := c.Read(context.Background(), []device.Address{a, b, missing})
	if e != nil || r[0].Quality != "GOOD" || len(r[0].Value.Elements) != 100 || r[0].Value.Elements[99].Integer != "7" || r[1].Value.Integer != "0" || r[2].Quality != "BAD" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestWriteAppliedButAckLost(t *testing.T) {
	m := simulator.NewMemory()
	srv, c := fixture(t, simulator.Options{Memory: m, Fault: simulator.Fault{DropWriteAck: true}})
	a, _ := device.ParseAddress("M0.1", "BOOL", 1)
	w, e := c.Write(context.Background(), a, device.Value{Type: "BOOL", Boolean: true})
	got, me := m.Get(a)
	if e == nil || w.State != "UNKNOWN" || me != nil || got[0] != 1 || srv.Stats().Writes != 1 {
		t.Fatalf("%+v %v %x %v writes=%d", w, e, got, me, srv.Stats().Writes)
	}
}
