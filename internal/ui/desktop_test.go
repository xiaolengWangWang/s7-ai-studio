package ui

import (
	"context"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"image/png"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/service"
	"s7-ai-studio/platform"
	"strings"
	"testing"
)

func desktop(t *testing.T, dir string) (*Desktop, *service.Controller) {
	t.Helper()
	a := test.NewApp()
	platform.ConfigureApp(a)
	if dir == "" {
		dir = t.TempDir()
	}
	c := service.New(service.Options{RecordDir: dir})
	d := NewDesktop(a, c, "0.2.0")
	t.Cleanup(func() { d.Shutdown(); d.Window().Close(); a.Quit() })
	return d, c
}
func TestDesktopReadOnlyByDefault(t *testing.T) {
	d, _ := desktop(t, "")
	if !d.writeBtn.Disabled() || !d.readBtn.Disabled() || d.allowWrite.Checked {
		t.Fatal("write or disconnected read enabled")
	}
	if !strings.Contains(d.heading.Text, "0.2.0") || !strings.Contains(d.heading.Text, "预览") {
		t.Fatal("missing version/preview")
	}
}
func TestDesktopDemoAndWriteConfirmation(t *testing.T) {
	d, c := desktop(t, "")
	test.Tap(d.demoBtn)
	d.workers.Wait()
	d.refresh(c.Snapshot())
	if len(d.rows) != 1 || d.rows[0][3] != "42" {
		t.Fatalf("demo rows %v", d.rows)
	}
	d.allowWrite.SetChecked(true)
	d.writeValue.SetText("123")
	test.Tap(d.writeBtn)
	if d.confirmation == nil {
		t.Fatal("no confirmation")
	}
	d.confirmation.Dismiss()
	d.workers.Wait()
	if c.Snapshot().WriteResult != nil {
		t.Fatal("cancel wrote")
	}
	test.Tap(d.writeBtn)
	d.confirmation.Confirm()
	d.workers.Wait()
	d.refresh(c.Snapshot())
	if !strings.Contains(d.writeStatus.Text, "CONFIRMED") || !strings.Contains(d.writeStatus.Text, "一致") || d.rows[0][3] != "123" {
		t.Fatalf("write status %q rows %v", d.writeStatus.Text, d.rows)
	}
}
func TestDesktopResultsAndRecordingError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	os.WriteFile(path, []byte("x"), 0600)
	d, c := desktop(t, path)
	if !strings.Contains(d.recordStatus.Text, "失败") {
		t.Fatal("record error hidden")
	}
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.refresh(service.Snapshot{Revision: 100, State: "connected", Results: c.Snapshot().Results, RecordError: "disk full"})
	if !strings.Contains(d.recordStatus.Text, "disk full") {
		t.Fatal("save error hidden")
	}
	for _, tc := range []struct{ typ, input string }{{"BOOL", "false"}, {"REAL", "0"}, {"LINT", "-9223372036854775808"}} {
		p := service.Point{Address: "DB1.DBB8", Type: device.DataType(tc.typ), Count: 1}
		if e := c.Write(context.Background(), service.WriteRequest{Point: p, Value: tc.input, Generation: c.Snapshot().Generation}, true); e != nil {
			t.Fatal(e)
		}
		s := c.Snapshot()
		s.Revision += 200
		d.refresh(s)
		if d.rows[0][3] != tc.input {
			t.Fatalf("%s lost: %v", tc.typ, d.rows)
		}
	}
}
func TestDesktopCompactLayout(t *testing.T) {
	d, c := desktop(t, "")
	if e := c.StartDemo(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.refresh(c.Snapshot())
	d.Window().Resize(fyne.NewSize(1000, 720))
	d.Window().Canvas().Content().Resize(fyne.NewSize(1000, 720))
	im := d.Window().Canvas().Capture()
	if im.Bounds().Dx() < 900 || d.readBtn.Hidden || d.writeBtn.Hidden {
		t.Fatal("unusable compact layout")
	}
	dir := os.Getenv("S7_AI_SNAPSHOT")
	if dir == "" {
		dir = t.TempDir()
	}
	os.MkdirAll(dir, 0755)
	f, e := os.Create(filepath.Join(dir, "windows-gui.png"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = png.Encode(f, im); e != nil {
		t.Fatal(e)
	}
}

func TestDesktopFitsHighDPIWorkArea(t *testing.T) {
	d, _ := desktop(t, "")
	var expand func(fyne.CanvasObject)
	expand = func(o fyne.CanvasObject) {
		switch o := o.(type) {
		case *widget.Accordion:
			o.Open(0)
		case *fyne.Container:
			for _, child := range o.Objects {
				expand(child)
			}
		case *container.Scroll:
			expand(o.Content)
		}
	}
	size := platform.FitSize(fyne.NewSize(1180, 820), 1366, 728, 1.5)
	for _, expanded := range []bool{false, true} {
		if expanded {
			expand(d.Window().Content())
		}
		min := d.Window().Content().MinSize()
		if min.Width > size.Width || min.Height > size.Height {
			t.Fatalf("expanded=%v: minimum %v exceeds available high-DPI size %v", expanded, min, size)
		}
		d.Window().Resize(size)
		if got := d.Window().Canvas().Size(); got.Width > size.Width || got.Height > size.Height {
			t.Fatalf("window exceeds desktop: %v > %v", got, size)
		}
	}
}
