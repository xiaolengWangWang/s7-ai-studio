//go:build windows && win7compat

package update

import (
	"errors"
	"testing"
)

func TestWindows7BuildDisablesInstall(t *testing.T) {
	if CanInstall() {
		t.Fatal("compatibility build must not install standard updates")
	}
	if _, err := Install("unused"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Install: got %v, want ErrUnsupported", err)
	}
}
