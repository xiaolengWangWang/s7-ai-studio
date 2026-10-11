package s7

import (
	"context"
	"s7-ai-studio/internal/device"
	"testing"
	"time"
)

func TestSplitWriteReportsEachSegment(t *testing.T) {
	target, tr := testPeer(t, "", 128)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("MW0", "INT", 100)
	v := device.Value{Type: "INT"}
	for i := 0; i < 100; i++ {
		v.Elements = append(v.Elements, device.Value{Type: "INT", Integer: "42"})
	}
	r, e := c.Write(context.Background(), a, v)
	if e != nil || r.State != "CONFIRMED" || len(r.Segments) < 2 || int32(len(r.Segments)) != tr.writes.Load() {
		t.Fatalf("%+v %v writes=%d", r, e, tr.writes.Load())
	}
	for _, s := range r.Segments {
		if s.State != "CONFIRMED" {
			t.Fatalf("%+v", s)
		}
	}
}
