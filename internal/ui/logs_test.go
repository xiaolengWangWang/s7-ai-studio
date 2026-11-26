package ui

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"s7-ai-studio/internal/service"
)

func TestDesktopLogSelectionFiltersAndOpensResponse(t *testing.T) {
	d, c := desktop(t, "")
	if err := c.StartDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	d.refresh(s)
	d.tabs.Select(d.logTab)
	l := d.logs
	l.category.SetSelected("连接")
	l.search.SetText("cotp")
	if len(l.shown) != 1 || l.shown[0].RequestID == 0 || l.exportBtn.Disabled() {
		t.Fatalf("unexpected handshake results: %+v", l.shown)
	}
	l.list.Select(0)
	selected := l.selected
	s.Revision++
	s.Logs = append(s.Logs, service.LogEntry{ID: s.Logs[len(s.Logs)-1].ID + 1, Time: time.Now(), Category: service.LogConnection, Level: service.LogError, Target: s.Target, Message: "COTP 测试提示"})
	d.refresh(s)
	if l.selected != selected || !strings.Contains(l.detail.Text, "COTP 连接握手") {
		t.Fatalf("live refresh moved the selection: %q", l.detail.Text)
	}
	test.Tap(l.copyResultsBtn)
	if text := d.app.Clipboard().Content(); !strings.Contains(text, "COTP 连接握手") || !strings.Contains(text, "COTP 测试提示") {
		t.Fatalf("copied logs: %q", text)
	}
	test.Tap(l.packetBtn)
	if d.tabs.Selected() != d.packetTab || !strings.Contains(d.wire.Text, "方向：RX") || !strings.Contains(d.wire.Text, "真实报文字节") {
		t.Fatalf("linked response was not displayed: %q", d.wire.Text)
	}
	d.tabs.Select(d.logTab)
	l.level.SetSelected("ERROR")
	if len(l.shown) != 1 || l.shown[0].Message != "COTP 测试提示" || l.selected != 0 || !l.packetBtn.Disabled() {
		t.Fatalf("level filter left stale selection: %+v", l.shown)
	}

	if dir := os.Getenv("S7_AI_SNAPSHOT"); dir != "" {
		l.search.SetText("")
		l.level.SetSelected("全部级别")
		l.category.SetSelected("全部分类")
		l.list.Select(0)
		d.Window().Resize(fyne.NewSize(1000, 720))
		d.Window().Canvas().Content().Resize(fyne.NewSize(1000, 720))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "windows-gui-logs.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, d.Window().Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
}
