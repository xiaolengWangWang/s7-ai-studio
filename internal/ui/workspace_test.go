package ui

import (
	"context"
	"net"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/service"
	"s7-ai-studio/internal/simulator"
)

func simulatorTarget(t *testing.T) string {
	t.Helper()
	srv, e := simulator.New(simulator.Options{Memory: simulator.NewMemory()})
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { defer close(done); srv.Serve(context.Background(), l) }()
	t.Cleanup(func() { srv.Close(); <-done })
	return l.Addr().String()
}

func TestDesktopRemembersInputsAfterSuccessfulConnect(t *testing.T) {
	d, _ := desktop(t, "")
	target := simulatorTarget(t)
	d.target.SetText(target)
	d.family.SetSelected("s7-1500")
	d.timeout.SetText("3000")
	d.addresses.SetText("MW10")
	d.kind.SetSelected("INT")
	d.writeValue.SetText("999")
	test.Tap(d.connectBtn)
	d.workers.Wait()
	if d.controller.Snapshot().State != "connected" {
		t.Fatalf("connect failed: %+v", d.controller.Snapshot())
	}
	c := service.New(service.Options{RecordDir: t.TempDir()})
	next := NewDesktop(d.app, c, "0.3.0")
	t.Cleanup(func() { next.Shutdown(); next.Window().Close() })
	if next.target.Text != target || next.family.Selected != "s7-1500" || next.timeout.Text != "3000" || next.addresses.Text != "MW10" || next.kind.Selected != "INT" {
		t.Fatalf("inputs not restored: %q %q %q %q %q", next.target.Text, next.family.Selected, next.timeout.Text, next.addresses.Text, next.kind.Selected)
	}
	if next.writeValue.Text == "999" || next.allowWrite.Checked {
		t.Fatal("write intent must not be remembered")
	}
}

func TestDesktopDoesNotRememberDemoPortOrFailedConnect(t *testing.T) {
	d, c := desktop(t, "")
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.refresh(c.Snapshot())
	test.Tap(d.readBtn)
	d.workers.Wait()
	if got := d.app.Preferences().String("input.target"); got != "" {
		t.Fatalf("remembered ephemeral demo target %q", got)
	}
	if got := d.app.Preferences().String("input.addresses"); got != "DB1.DBD0" {
		t.Fatalf("successful read inputs not remembered: %q", got)
	}
	d.target.SetText("127.0.0.1:1")
	d.timeout.SetText("200")
	test.Tap(d.connectBtn)
	d.workers.Wait()
	if got := d.app.Preferences().String("input.target"); got != "" {
		t.Fatalf("remembered failed target %q", got)
	}
}

func TestDesktopEnterSubmitsOnlyEnabledActions(t *testing.T) {
	d, c := desktop(t, "")
	// 直接调用 OnSubmitted：测试驱动在后台 goroutine 执行 fyne.Do，TypedKey 之后的重绘会与之竞争。
	enter := func(e *widget.Entry) { e.OnSubmitted(e.Text) }
	enter(d.addresses)
	d.workers.Wait()
	if len(c.Snapshot().Results) != 0 {
		t.Fatal("Enter read while disconnected")
	}
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.refresh(c.Snapshot())
	before := c.Snapshot().Results[0].Time
	enter(d.addresses)
	d.workers.Wait()
	if !c.Snapshot().Results[0].Time.After(before) {
		t.Fatal("Enter did not read")
	}
	enter(d.writeValue)
	if d.confirmation != nil {
		t.Fatal("Enter opened write confirmation without permission")
	}
	d.allowWrite.SetChecked(true)
	enter(d.writeValue)
	if d.confirmation == nil {
		t.Fatal("Enter skipped write confirmation")
	}
	d.confirmation.Dismiss()
	d.workers.Wait()
	if c.Snapshot().WriteResult != nil {
		t.Fatal("Enter wrote without confirmation")
	}
}

func TestDesktopStatusColorsAndToggles(t *testing.T) {
	d, c := desktop(t, "")
	if d.emptyHint.Hidden || !d.lastError.Hidden || d.demoBtn.Hidden || !d.stopDemoBtn.Hidden || !d.stopPollBtn.Hidden {
		t.Fatal("initial layout wrong")
	}
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.refresh(c.Snapshot())
	if d.status.Importance != widget.SuccessImportance || !strings.Contains(d.status.Text, "已连接") {
		t.Fatalf("status %q importance %v", d.status.Text, d.status.Importance)
	}
	if !d.emptyHint.Hidden || !d.demoBtn.Hidden || d.stopDemoBtn.Hidden {
		t.Fatal("demo controls or empty hint not toggled")
	}
	if cellImportance(d.rows[0], 3) != widget.SuccessImportance || cellImportance([]string{"", "", "", "BAD", "", "x"}, 3) != widget.DangerImportance {
		t.Fatal("quality colors wrong")
	}
	s := c.Snapshot()
	s.Revision += 10
	s.State, s.LastError = "error", "connection reset"
	d.refresh(s)
	if d.status.Importance != widget.DangerImportance || d.lastError.Hidden || d.lastError.Text != "connection reset" {
		t.Fatal("error not visible")
	}
	if writeImportance("CONFIRMED", "一致") != widget.SuccessImportance || writeImportance("CONFIRMED", "不一致") != widget.WarningImportance || writeImportance("UNKNOWN", "无法确认") != widget.WarningImportance || writeImportance("REJECTED", "") != widget.DangerImportance {
		t.Fatal("write colors wrong")
	}
}

func TestPacketDetailsCanBeCopied(t *testing.T) {
	d, c := desktop(t, "")
	if !d.copyPacketBtn.Disabled() {
		t.Fatal("copy enabled without selection")
	}
	s := c.Snapshot()
	s.Revision++
	s.Packets = []device.Event{{RequestID: 7, Dir: device.DirTX, Raw: []byte{0xab}}}
	d.refresh(s)
	d.packets.Select(0)
	test.Tap(d.copyPacketBtn)
	if got := d.app.Clipboard().Content(); !strings.Contains(got, "请求 ID：7") || !strings.Contains(got, "ab") {
		t.Fatalf("clipboard %q", got)
	}
	if packetSummary(s.Packets) != "TX 1 · RX 0" {
		t.Fatal(packetSummary(s.Packets))
	}
}
