package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"s7-ai-studio/internal/update"
)

func updateServer(t *testing.T, badChecksum bool) *atomic.Int32 {
	t.Helper()
	data := []byte("verified update package")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if badChecksum {
		hash = strings.Repeat("0", 64)
	}
	name := "S7AIStudio-0.6.1-Windows-x64.zip"
	if runtime.GOOS == "darwin" {
		name = "S7AIStudio-0.6.1-macOS-Intel.dmg"
		if runtime.GOARCH == "arm64" {
			name = "S7AIStudio-0.6.1-macOS-AppleSilicon.dmg"
		}
	}
	var downloads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/package" {
			downloads.Add(1)
			w.Write(data)
			return
		}
		json.NewEncoder(w).Encode(update.Release{Tag: "v0.6.1", Name: "S7 AI Studio 0.6.1", Body: "改进点位验证\n\n## SHA-256\n" + hash + "  " + name, Assets: []update.Asset{{Name: name, URL: server.URL + "/package", Size: int64(len(data))}}})
	}))
	t.Cleanup(server.Close)
	old := update.Sources
	update.Sources = []update.Source{{Name: "Gitee", LatestURL: server.URL + "/latest", PageURL: server.URL}}
	t.Cleanup(func() { update.Sources = old })
	return &downloads
}

func TestDesktopChecksUpdatesAndShowsRelease(t *testing.T) {
	updateServer(t, false)
	d, _ := desktop(t, "")
	test.Tap(d.updates.checkBtn)
	d.workers.Wait()
	if !strings.Contains(d.updates.status.Text, "0.6.1") || d.updates.release.Source != "Gitee" || len(d.updates.notes.Segments) == 0 {
		t.Fatalf("release not shown: %s", d.updates.status.Text)
	}
	if strings.Contains(d.updates.status.Text, "sun_xuanqi") {
		t.Fatal("account name exposed in update status")
	}
}

func TestDesktopCancelsUpdateCheck(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	old := update.Sources
	update.Sources = []update.Source{{Name: "Gitee", LatestURL: srv.URL}}
	t.Cleanup(func() { update.Sources = old })
	d, _ := desktop(t, "")
	test.Tap(d.updates.checkBtn)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("check did not start")
	}
	test.Tap(d.updates.cancelBtn)
	d.workers.Wait()
	if !strings.Contains(d.updates.status.Text, "取消") || d.updates.checkBtn.Disabled() {
		t.Fatalf("cancel did not restore controls: %s", d.updates.status.Text)
	}
}

func TestDesktopInstallsVerifiedUpdateAfterConfirmation(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("installation is supported on Windows and packaged macOS only")
	}
	downloads := updateServer(t, false)
	d, c := desktop(t, "")
	u := d.updates
	u.cacheDir = t.TempDir()
	u.canInstall = func() bool { return true }
	u.install = func(pkg string) (string, error) {
		b, err := os.ReadFile(pkg)
		if err != nil || string(b) != "verified update package" {
			t.Errorf("wrong package: %q %v", b, err)
		}
		return "new-program", err
	}
	restarted := make(chan string, 1)
	u.restart = func(target string) error {
		if c.Snapshot().State != "closed" {
			t.Error("controller must close and flush records before restart")
		}
		restarted <- target
		return errors.New("restart intercepted by test")
	}
	test.Tap(u.checkBtn)
	d.workers.Wait()
	if err := c.StartDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.refresh(c.Snapshot())
	if !u.installBtn.Disabled() {
		t.Fatal("installation allowed during PLC session")
	}
	if err := c.StopDemo(); err != nil {
		t.Fatal(err)
	}
	d.refresh(c.Snapshot())
	test.Tap(u.installBtn)
	u.confirmation.Dismiss()
	if downloads.Load() != 0 {
		t.Fatal("cancelled confirmation downloaded an update")
	}
	test.Tap(u.installBtn)
	u.confirmation.Confirm()
	select {
	case target := <-restarted:
		if target != "new-program" || downloads.Load() != 1 {
			t.Fatalf("unexpected restart target %q, downloads %d", target, downloads.Load())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("update did not restart: %s", u.status.Text)
	}
}

func TestDesktopRefusesUpdateWithWrongChecksum(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("installation is supported on Windows and packaged macOS only")
	}
	updateServer(t, true)
	d, _ := desktop(t, "")
	u := d.updates
	u.cacheDir = t.TempDir()
	u.canInstall = func() bool { return true }
	u.install = func(string) (string, error) { t.Error("unverified package reached installer"); return "", nil }
	test.Tap(u.checkBtn)
	d.workers.Wait()
	test.Tap(u.installBtn)
	u.confirmation.Confirm()
	d.workers.Wait()
	if !strings.Contains(u.status.Text, "校验失败") {
		t.Fatalf("checksum failure hidden: %s", u.status.Text)
	}
}

func TestDesktopUpdateConfirmationBindsShownVersion(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("installation is supported on Windows and packaged macOS only")
	}
	downloads := updateServer(t, false)
	d, _ := desktop(t, "")
	u := d.updates
	u.canInstall = func() bool { return true }
	u.cacheDir = t.TempDir()
	u.install = func(string) (string, error) { return "", errors.New("unexpected installer call") }
	test.Tap(u.checkBtn)
	d.workers.Wait()
	test.Tap(u.installBtn)
	// A second check may complete while the confirmation is open.
	u.release.Tag = "v0.6.2"
	u.confirmation.Confirm()
	d.workers.Wait()
	if downloads.Load() != 0 || !strings.Contains(u.status.Text, "重新确认") {
		t.Fatalf("confirmation installed a different version: %s, downloads %d", u.status.Text, downloads.Load())
	}
}
