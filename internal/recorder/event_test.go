package recorder

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"testing"
	"time"
)

func TestS7EventMetadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "S7 历史.sqlite3")
	r, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	id, e := r.StartSession(device.ProtocolS7TCP, "synthetic", 1)
	if e != nil {
		t.Fatal(e)
	}
	a := device.Address{Area: "DB", DBNumber: 1, ByteOffset: 70000, BitOffset: 7, Type: "BOOL", Count: 1}
	result := &device.ReadResult{Address: a, Quality: "BAD", Error: "denied"}
	event := device.Event{Time: time.Now(), Protocol: device.ProtocolS7TCP, Dir: device.DirRX, Operation: "READ", Targets: []device.Address{a}, Results: []device.EventResult{{Read: result}}, Raw: []byte{0x32, 3}, Status: device.StatusException}
	r.Record(id, event)
	event.Targets[0].ByteOffset = 0
	result.Error = "mutated"
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	got, e := r.PacketsFile(path, id, 10)
	if e != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, e)
	}
	if got[0].Targets[0].ByteOffset != 70000 || got[0].Targets[0].DBNumber != 1 || got[0].Targets[0].BitOffset != 7 || got[0].Legacy != nil || got[0].Results[0].Read.Error != "denied" || !bytes.Equal(got[0].Raw, []byte{0x32, 3}) {
		t.Fatalf("%+v", got[0])
	}
}
func legacyFile(t *testing.T, protocol string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.sqlite3")
	db, e := sql.Open(driverName, writeDSN(path))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE sessions(id INTEGER PRIMARY KEY,started_at INTEGER,ended_at INTEGER,protocol TEXT,target TEXT,window INTEGER);
CREATE TABLE packets(id INTEGER PRIMARY KEY,session_id INTEGER,time INTEGER,request_id INTEGER,direction TEXT,protocol TEXT,slave INTEGER,tx_id INTEGER,function INTEGER,address INTEGER,count INTEGER,raw BLOB,status TEXT,rtt_us INTEGER,error TEXT);
CREATE TABLE events(id INTEGER PRIMARY KEY,session_id INTEGER,time INTEGER,kind TEXT,detail TEXT);`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO sessions VALUES(1,1,NULL,?,'old',1)`, protocol)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO packets VALUES(1,1,1,1,'RX',?,4,6,3,70000,2,x'0102','SUCCESS',1,NULL)`, protocol)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestReadOnlyLegacyFileUnchanged(t *testing.T) {
	path := legacyFile(t, "MODBUS_TCP")
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	r := &Recorder{}
	got, e := r.PacketsFile(path, 1, 10)
	if e != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, e)
	}
	after, e := os.ReadFile(path)
	if e != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("read-only archive changed")
	}
	if got[0].Protocol != "MODBUS_TCP" || got[0].Legacy == nil || got[0].Legacy.Address != 70000 || len(got[0].Targets) != 0 {
		t.Fatalf("%+v", got[0])
	}
}
func TestLegacyProtocolRowsRemainLegacy(t *testing.T) {
	path := legacyFile(t, "MODBUS_TCP")
	r, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	got, e := r.Packets(1, 10)
	if e != nil || len(got) != 1 || got[0].Protocol != "MODBUS_TCP" || got[0].Legacy == nil || got[0].Legacy.Function != 3 {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestMigrationPreservesUnknownProtocol(t *testing.T) {
	path := legacyFile(t, "VENDOR_FUTURE")
	r, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	got, e := r.Packets(1, 10)
	if e != nil || len(got) != 1 || got[0].Protocol != "VENDOR_FUTURE" || len(got[0].Targets) != 0 || got[0].Legacy.Address != 70000 {
		t.Fatalf("%+v %v", got, e)
	}
}
