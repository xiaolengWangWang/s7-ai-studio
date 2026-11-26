package ui

import (
	"s7-ai-studio/internal/device"
	"strings"
	"testing"
)

func TestPacketFilteringAndDetails(t *testing.T) {
	events := []device.Event{{Operation: "CONNECT"}, {Operation: "READ"}, {Operation: "CONNECTION_STATUS"}, {Operation: "WRITE"}}
	if got := filteredPackets(events, "连接"); len(got) != 2 || got[1].Operation != "CONNECTION_STATUS" {
		t.Fatalf("connection filter: %#v", got)
	}
	if got := filteredPackets(events, "读取"); len(got) != 1 || got[0].Operation != "READ" {
		t.Fatalf("read filter: %#v", got)
	}
	setup := []byte{3, 0, 0, 25, 2, 0xf0, 0x80, 0x32, 1, 0, 0, 0, 1, 0, 8, 0, 0, 0xf0, 0, 0, 2, 0, 3, 1, 0xe0}
	text := packetText(device.Event{Operation: "SETUP", Raw: setup})
	for _, want := range []string{"PDU 引用：1", "MaxAmQ 调用方：2", "协商 PDU 长度：480", "单个 S7 会话允许并发的 PDU 作业数"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	for i := range setup {
		_ = packetText(device.Event{Operation: "SETUP", Raw: setup[:i]})
	}
}

func TestPacketFilterClearsSelection(t *testing.T) {
	d, c := desktop(t, "")
	s := c.Snapshot()
	s.Revision++
	s.Packets = []device.Event{{Operation: "CONNECT", RequestID: 1}, {Operation: "READ", RequestID: 2}}
	d.refresh(s)
	d.packets.Select(1)
	d.packetFilter = "连接"
	d.packets.UnselectAll()
	d.packetSelection = -1
	d.wire.SetText("")
	d.refreshPackets()
	if d.wire.Text != "" {
		t.Fatalf("stale details: %s", d.wire.Text)
	}
	d.packets.Select(0)
	if !strings.Contains(d.wire.Text, "请求 ID：1") {
		t.Fatalf("wrong packet: %s", d.wire.Text)
	}
}

func TestLivePacketSelectionKeepsRequestWhenRowsMove(t *testing.T) {
	d, c := desktop(t, "")
	s := c.Snapshot()
	first := device.Event{ConnectionID: "session-a", RequestID: 1, Dir: device.DirTX}
	selected := device.Event{ConnectionID: "session-a", RequestID: 2, Dir: device.DirTX}
	later := device.Event{ConnectionID: "session-a", RequestID: 3, Dir: device.DirTX}
	s.Revision++
	s.Packets = []device.Event{first, selected}
	d.refresh(s)
	d.packets.Select(1)
	s.Revision++
	s.Packets = []device.Event{selected, later}
	d.refresh(s)
	if d.packetSelection != 0 || !strings.Contains(d.wire.Text, "请求 ID：2") {
		t.Fatalf("selection moved to a different request: %q", d.wire.Text)
	}
	s.Revision++
	s.Packets = []device.Event{later}
	d.refresh(s)
	if d.wire.Text != "" || !d.copyPacketBtn.Disabled() {
		t.Fatalf("expired request is still selected: %q", d.wire.Text)
	}
}
