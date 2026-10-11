package cli

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/simulator"
	"strings"
	"testing"
	"time"
)

func cliPeer(t *testing.T) (string, *simulator.Server, *simulator.Memory) {
	t.Helper()
	m := simulator.NewMemory()
	srv, e := simulator.New(simulator.Options{Memory: m})
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
	t.Cleanup(func() {
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("CLI peer leaked")
		}
	})
	return ln.Addr().String(), srv, m
}
func TestCLIReadExactInt64JSON(t *testing.T) {
	target, _, m := cliPeer(t)
	a, _ := device.ParseAddress("DB1.DBB0", "LINT", 1)
	if e := m.Set(a, []byte{0, 32, 0, 0, 0, 0, 0, 1}); e != nil {
		t.Fatal(e)
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"-target", target, "-type", "LINT", "-json", "read", "DB1.DBB0"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), `"integer": "9007199254740993"`) {
		t.Fatalf("exit=%d %s %s", code, &out, &errOut)
	}
}
func TestCLIRejectWriteWithoutAllowWrite(t *testing.T) {
	target, srv, _ := cliPeer(t)
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"-target", target, "-type", "INT", "write", "MW0", "42"}, &out, &errOut)
	if code != 2 || srv.Stats().Writes != 0 || !strings.Contains(errOut.String(), "allow-write") {
		t.Fatalf("exit=%d writes=%d %s", code, srv.Stats().Writes, &errOut)
	}
}
func TestCLIMixedResultExitCode(t *testing.T) {
	target, _, _ := cliPeer(t)
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"-target", target, "-type", "DINT", "-json", "read", "DB1.DBD0,DB9.DBD0"}, &out, &errOut)
	if code != 1 || !strings.Contains(out.String(), `"quality": "GOOD"`) || !strings.Contains(out.String(), `"quality": "BAD"`) {
		t.Fatalf("exit=%d %s %s", code, &out, &errOut)
	}
}
func TestCLIRecordPathWithSpaces(t *testing.T) {
	target, _, _ := cliPeer(t)
	directory := filepath.Join(t.TempDir(), "通信 历史")
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"-target", target, "-record", directory, "-type", "DINT", "read", "DB1.DBD0"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	files, e := filepath.Glob(filepath.Join(directory, "*.sqlite3"))
	if e != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, e)
	}
	r := &recorder.Recorder{}
	sessions, e := r.SessionsFile(files[0], 10)
	if e != nil || len(sessions) != 1 || sessions[0].End.IsZero() {
		t.Fatalf("%+v %v", sessions, e)
	}
	events, e := r.PacketsFile(files[0], sessions[0].ID, 100)
	if e != nil || len(events) < 6 || events[len(events)-1].Operation != "READ" || events[len(events)-1].Protocol != device.ProtocolS7TCP {
		t.Fatalf("%+v %v", events, e)
	}
}
func TestCLIUnsupportedRoute(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"-target", "127.0.0.1:9", "-route", "ppi", "read", "MW0"}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "未实现") {
		t.Fatalf("%d %s", code, &errOut)
	}
}
