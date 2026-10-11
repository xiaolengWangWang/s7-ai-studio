package service

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/s7"
	"s7-ai-studio/internal/simulator"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var point = Point{Address: "DB1.DBD0", Type: "DINT", Count: 1}

func newController(t *testing.T, opts Options) *Controller {
	t.Helper()
	if opts.RecordDir == "" {
		opts.RecordDir = t.TempDir()
	}
	c := New(opts)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Log(err)
		}
	})
	return c
}
func peer(t *testing.T, fault simulator.Fault) (*simulator.Server, string) {
	t.Helper()
	mem := simulator.NewMemory()
	a, _ := device.ParseAddress("DB1.DBD0", "DINT", 1)
	mem.Set(a, []byte{0, 0, 0, 42})
	srv, e := simulator.New(simulator.Options{Memory: mem, Fault: fault})
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go srv.Serve(context.Background(), l)
	t.Cleanup(func() { srv.Close() })
	return srv, l.Addr().String()
}
func connect(t *testing.T, c *Controller, target string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Target = target
	cfg.Timeout = 150 * time.Millisecond
	if e := c.Connect(context.Background(), cfg); e != nil {
		t.Fatal(e)
	}
}
func until(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not complete")
}

func TestConfigRejectsBeforeDial(t *testing.T) {
	var calls atomic.Int32
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) {
		calls.Add(1)
		return nil, errors.New("should not dial")
	}})
	for _, target := range []string{"", ":102", "host:0", "host:70000", "host"} {
		cfg := DefaultConfig()
		cfg.Target = target
		if c.Connect(context.Background(), cfg) == nil {
			t.Fatalf("accepted %q", target)
		}
	}
	cfg := DefaultConfig()
	cfg.Target = "127.0.0.1:102"
	cfg.RemoteTSAP = "nonsense"
	if c.Connect(context.Background(), cfg) == nil {
		t.Fatal("invalid TSAP accepted")
	}
	cfg = DefaultConfig()
	cfg.Target = "127.0.0.1:102"
	cfg.Family = "s7-200"
	if c.Connect(context.Background(), cfg) == nil {
		t.Fatal("missing TSAP accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("dialed invalid config")
	}
}
func TestValuesPreservePrecision(t *testing.T) {
	for _, tc := range []struct {
		kind, input, want string
		count             int
	}{{"BOOL", "false", "false", 1}, {"REAL", "0", "0", 1}, {"LINT", "-9223372036854775808", "-9223372036854775808", 1}, {"ULINT", "18446744073709551615", "18446744073709551615", 1}, {"INT", "1,-2,0", "1,-2,0", 3}, {"RAW", "AA,00", "aa,00", 2}, {"STRING[8]", "hello", "\"hello\"", 1}} {
		a, e := device.ParseAddress("DB1.DBB0", device.DataType(tc.kind), tc.count)
		if e != nil {
			t.Fatal(e)
		}
		v, e := ParseValue(a, tc.input)
		if e != nil {
			t.Fatal(e)
		}
		b, e := device.EncodeValue(a, v)
		if e != nil {
			t.Fatal(e)
		}
		v, e = device.DecodeValue(a, b)
		if e != nil {
			t.Fatal(e)
		}
		if got := FormatValue(v); got != tc.want {
			t.Errorf("%s got %q want %q", tc.kind, got, tc.want)
		}
	}
}
func TestWritePermissionAndReadback(t *testing.T) {
	srv, target := peer(t, simulator.Fault{})
	c := newController(t, Options{})
	connect(t, c, target)
	req := WriteRequest{Point: point, Value: "123", Generation: c.Snapshot().Generation}
	if c.Write(context.Background(), req, false) == nil || srv.Stats().Writes != 0 {
		t.Fatal("unauthorized write")
	}
	if e := c.Write(context.Background(), req, true); e != nil {
		t.Fatal(e)
	}
	s := c.Snapshot()
	if s.WriteResult == nil || s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" || srv.Stats().Writes != 1 {
		t.Fatalf("write outcome %+v", s)
	}
	req.Generation--
	if c.Write(context.Background(), req, true) == nil || srv.Stats().Writes != 1 {
		t.Fatal("stale confirmation wrote")
	}
}
func TestUnknownNeverRetries(t *testing.T) {
	srv, target := peer(t, simulator.Fault{DropWriteAck: true})
	c := newController(t, Options{})
	connect(t, c, target)
	e := c.Write(context.Background(), WriteRequest{point, "99", c.Snapshot().Generation}, true)
	s := c.Snapshot()
	if e == nil || s.WriteResult == nil || s.WriteResult.State != "UNKNOWN" || srv.Stats().Writes != 1 {
		t.Fatalf("unknown was lost or retried: %+v %v", s, e)
	}
}
func TestRejectedWritePreservesSessionAndPolling(t *testing.T) {
	_, target := peer(t, simulator.Fault{RejectArea: "M"})
	c := newController(t, Options{})
	connect(t, c, target)
	if err := c.StartPolling(context.Background(), []Point{point}, time.Second); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return len(c.Snapshot().Results) == 1 })
	gen := c.Snapshot().Generation
	err := c.Write(context.Background(), WriteRequest{Point{"MD0", "DINT", 1}, "123", gen}, true)
	s := c.Snapshot()
	if err == nil || s.WriteResult == nil || s.WriteResult.State != "REJECTED" {
		t.Fatalf("rejection not reported: %+v %v", s, err)
	}
	if s.State != "connected" || !s.Polling || s.Generation != gen {
		t.Fatalf("valid rejection invalidated healthy session: %+v", s)
	}
	if err = c.Read(context.Background(), []Point{point}); err != nil {
		t.Fatal("same session cannot read after rejection", err)
	}
	if got := c.Snapshot().Results; len(got) != 1 || FormatValue(got[0].Value) != "42" {
		t.Fatalf("read after rejection: %+v", got)
	}
}

func TestCloseReportsRecordingFailures(t *testing.T) {
	t.Run("session metadata", func(t *testing.T) {
		dir := t.TempDir()
		c := newController(t, Options{RecordDir: dir, Dial: func(context.Context, string, s7.Options) (device.Driver, error) {
			return &blockingDriver{}, nil
		}})
		connect(t, c, "127.0.0.1:102")
		files, err := filepath.Glob(filepath.Join(dir, "*.sqlite3"))
		if err != nil || len(files) != 1 {
			t.Fatalf("archives: %v %v", files, err)
		}
		driver := "sqlite"
		for _, name := range sql.Drivers() {
			if name == "sqlite3" {
				driver = name
			}
		}
		db, err := sql.Open(driver, files[0])
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TRIGGER reject_session_end BEFORE UPDATE ON sessions BEGIN SELECT RAISE(FAIL, 'blocked session metadata'); END`)
		closeErr := db.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("trigger: %v %v", err, closeErr)
		}
		if err = c.Close(); err == nil || !strings.Contains(err.Error(), "blocked session metadata") {
			t.Fatalf("shutdown hid metadata failure: %v", err)
		}
	})
	t.Run("initialization", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		c := newController(t, Options{RecordDir: path})
		if err := c.Close(); err == nil {
			t.Fatal("shutdown hid initialization failure")
		}
	})
	t.Run("queue drops", func(t *testing.T) {
		c := newController(t, Options{})
		c.rec.Dropped.Add(1)
		if err := c.Close(); err == nil || !strings.Contains(err.Error(), "1") {
			t.Fatalf("shutdown hid dropped packet: %v", err)
		}
	})
}
func TestDemoRecorderLifecycle(t *testing.T) {
	dir := t.TempDir()
	c := newController(t, Options{RecordDir: dir})
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	s := c.Snapshot()
	if len(s.Results) != 1 || FormatValue(s.Results[0].Value) != "42" || s.DemoTarget == "" {
		t.Fatalf("demo %+v", s)
	}
	if e := c.Write(context.Background(), WriteRequest{point, "123", s.Generation}, true); e != nil {
		t.Fatal(e)
	}
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", s.DemoTarget)
	if e != nil {
		t.Fatal("demo listener leaked", e)
	}
	l.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "*.sqlite3"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	r, e := recorder.Open(files[0])
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	ss, e := r.Sessions(10)
	if e != nil || len(ss) != 1 || ss[0].End.IsZero() || ss[0].Packets < 10 {
		t.Fatalf("archive %+v %v", ss, e)
	}
}

type blockingDriver struct {
	started, release chan struct{}
	mode             string
	reads            atomic.Int32
}

func (d *blockingDriver) Capabilities() device.Capabilities {
	return device.Capabilities{Protocol: device.ProtocolS7TCP}
}
func (d *blockingDriver) Close() error { return nil }
func (d *blockingDriver) Write(context.Context, device.Address, device.Value) (device.WriteResult, error) {
	return device.WriteResult{State: "CONFIRMED"}, nil
}
func (d *blockingDriver) Read(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	d.reads.Add(1)
	if d.started != nil {
		close(d.started)
		d.started = nil
	}
	if d.release != nil {
		<-d.release
	}
	if d.mode == "cancel" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	rr := []device.ReadResult{{Address: aa[0], Quality: "GOOD", Value: device.Value{Type: "DINT", Integer: "42"}, Time: time.Now()}}
	if d.mode == "partial" {
		return rr, errors.New("transport closed")
	}
	return rr, nil
}
func TestDisconnectSuppressesLateResult(t *testing.T) {
	d := &blockingDriver{started: make(chan struct{}), release: make(chan struct{})}
	started := d.started
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) { return d, nil }})
	connect(t, c, "127.0.0.1:102")
	done := make(chan error, 1)
	go func() { done <- c.Read(context.Background(), []Point{point}) }()
	<-started
	disconnected := make(chan error, 1)
	go func() { disconnected <- c.Disconnect() }()
	until(t, func() bool { return c.Snapshot().State == "disconnected" })
	close(d.release)
	<-done
	<-disconnected
	if len(c.Snapshot().Results) != 0 {
		t.Fatal("late result republished")
	}
}
func TestPartialReadStopsPolling(t *testing.T) {
	d := &blockingDriver{mode: "partial"}
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) { return d, nil }})
	connect(t, c, "127.0.0.1:102")
	if e := c.StartPolling(context.Background(), []Point{point}, 100*time.Millisecond); e != nil {
		t.Fatal(e)
	}
	until(t, func() bool { return c.Snapshot().State == "error" })
	s := c.Snapshot()
	if s.Polling || len(s.Results) != 1 || s.Results[0].Quality != "GOOD" {
		t.Fatalf("partial data lost %+v", s)
	}
}
func TestCloseCancelsDialAndRead(t *testing.T) {
	for _, dial := range []bool{true, false} {
		t.Run(map[bool]string{true: "dial", false: "read"}[dial], func(t *testing.T) {
			started := make(chan struct{})
			d := &blockingDriver{mode: "cancel", started: started}
			c := newController(t, Options{Dial: func(ctx context.Context, _ string, _ s7.Options) (device.Driver, error) {
				if dial {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return d, nil
			}})
			if dial {
				go func() { cfg := DefaultConfig(); cfg.Target = "127.0.0.1:102"; c.Connect(context.Background(), cfg) }()
			} else {
				connect(t, c, "127.0.0.1:102")
				go c.Read(context.Background(), []Point{point})
			}
			<-started
			done := make(chan struct{})
			go func() { c.Close(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("shutdown deadlocked")
			}
		})
	}
}
func TestReadbackMismatch(t *testing.T) {
	d := &blockingDriver{}
	c := newController(t, Options{Dial: func(context.Context, string, s7.Options) (device.Driver, error) { return d, nil }})
	connect(t, c, "127.0.0.1:102")
	if e := c.Write(context.Background(), WriteRequest{point, "123", c.Snapshot().Generation}, true); e != nil {
		t.Fatal(e)
	}
	if c.Snapshot().Readback != "不一致" {
		t.Fatal("mismatch reported as confirmed")
	}
}
func TestVAddressRequiresMapping(t *testing.T) {
	_, target := peer(t, simulator.Fault{})
	c := newController(t, Options{})
	cfg := DefaultConfig()
	cfg.Family = "s7-200-smart"
	cfg.RemoteTSAP = "0x0200"
	cfg.Target = target
	if e := c.Connect(context.Background(), cfg); e != nil {
		t.Fatal(e)
	}
	if c.Read(context.Background(), []Point{{"VD0", "DINT", 1}}) == nil {
		t.Fatal("missing V mapping accepted")
	}
}
