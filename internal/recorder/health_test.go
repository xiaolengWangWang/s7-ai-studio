package recorder

import (
	"path/filepath"
	"s7-ai-studio/internal/device"
	"testing"
	"time"
)

func TestRecorderHealthReportsFailure(t *testing.T) {
	r, e := Open(filepath.Join(t.TempDir(), "packets.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	id, e := r.StartSession(device.ProtocolS7TCP, "test", 1)
	if e != nil {
		t.Fatal(e)
	}
	r.dbMu.Lock()
	_, e = r.db.Exec("DROP TABLE packets")
	r.dbMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	r.Record(id, device.Event{Time: time.Now(), Protocol: device.ProtocolS7TCP, Dir: device.DirTX, Status: device.StatusSent})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dropped, err := r.Health()
		if dropped > 0 && err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background write error invisible")
}
