package main

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/service"
	"testing"
)

func TestNativeSmokeUsesOwnedDemoAndDrainsRecords(t *testing.T) {
	dir := t.TempDir()
	c := service.New(service.Options{RecordDir: filepath.Join(dir, "records")})
	defer c.Close()
	if e := runSmoke(context.Background(), c, func() image.Image { return image.NewRGBA(image.Rect(0, 0, 4, 4)) }, dir); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "smoke.json"))
	if e != nil {
		t.Fatal(e)
	}
	var r smokeReport
	if e = json.Unmarshal(b, &r); e != nil {
		t.Fatal(e)
	}
	if r.Initial != "42" || r.Readback != "123" || r.WriteState != "CONFIRMED" || !r.Passed {
		t.Fatalf("smoke evidence %+v", r)
	}
	if c.Snapshot().State != "closed" {
		t.Fatal("recorder not drained")
	}
	if _, e = os.Stat(filepath.Join(dir, "native-gui.png")); e != nil {
		t.Fatal(e)
	}
}
