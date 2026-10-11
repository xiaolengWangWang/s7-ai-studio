package ui

import (
	"strings"
	"testing"

	"s7-ai-studio/internal/device"
)

func TestPacketDetailsTrackSelectedRow(t *testing.T) {
	d, c := desktop(t, "")
	s := c.Snapshot()
	s.Revision++
	s.Packets = []device.Event{{RequestID: 1, Dir: device.DirTX, Raw: []byte{1}}}
	d.refresh(s)
	d.packets.Select(0)
	s.Revision++
	s.Packets = []device.Event{{RequestID: 2, Dir: device.DirRX, Raw: []byte{2}}}
	d.refresh(s)
	if !strings.Contains(d.wire.Text, "请求 ID：2") || !strings.Contains(d.wire.Text, "02") {
		t.Fatalf("details no longer match selected row: %s", d.wire.Text)
	}
	s.Revision++
	s.Packets = nil
	d.refresh(s)
	if d.wire.Text != "" {
		t.Fatalf("removed packet retains stale bytes: %s", d.wire.Text)
	}
}
