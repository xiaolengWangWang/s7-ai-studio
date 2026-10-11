package ui

import (
	"s7-ai-studio/internal/device"
	"strings"
	"testing"
)

func TestPacketDetailShowsWireReference(t *testing.T) {
	e := device.Event{RequestID: 3, Raw: []byte{3, 0, 0, 17, 2, 0xf0, 0x80, 0x32, 1, 0, 0, 0, 2, 0, 0, 0, 0}}
	text := packetText(e)
	if !strings.Contains(text, "请求 ID：3") || !strings.Contains(text, "PDU 引用：2") {
		t.Fatalf("wire reference confused with request ID: %s", text)
	}
	e.Raw = []byte{3}
	if strings.Contains(packetText(e), "PDU 引用：2") {
		t.Fatal("invented PDU reference")
	}
}
