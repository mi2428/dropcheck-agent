package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
)

func TestDiscoveryUsesExplicitSerialAndFiltersInventory(t *testing.T) {
	targets, err := discoverADBTargets(t.Context(), adb.Client{Path: filepath.Join(t.TempDir(), "missing")}, "explicit")
	if err != nil || len(targets) != 1 || targets[0].Serial != "explicit" {
		t.Fatalf("explicit: %v %v", targets, err)
	}
	path := fakeADB(t, "printf 'List of devices attached\nready device model:Phone\noffline offline\nuntrusted unauthorized\n'\n")
	targets, err = discoverADBTargets(t.Context(), adb.Client{Path: path}, "")
	if err != nil || len(targets) != 1 || targets[0].Serial != "ready" {
		t.Fatalf("inventory: %v %v", targets, err)
	}
	path = fakeADB(t, "printf 'offline offline\n'\n")
	if _, err := discoverADBTargets(t.Context(), adb.Client{Path: path}, ""); err == nil {
		t.Fatal("empty ready set accepted")
	}
}

func TestCloseAggregatesAndRetainsEveryDeviceError(t *testing.T) {
	path := fakeADB(t, "printf 'synthetic remove failure %s\\n' \"$2\" >&2\nexit 9\n")
	s := &Session{port: 12345, reversed: []adb.Client{{Path: path, Serial: "first"}, {Path: path, Serial: "second"}}}
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Go(func() { errs[i] = s.Close() })
	}
	wg.Wait()
	for _, err := range errs {
		if err == nil || err != errs[0] || !strings.Contains(err.Error(), "serial=first") || !strings.Contains(err.Error(), "serial=second") {
			t.Fatalf("cleanup errors not retained: %v", err)
		}
	}
	if err := (&Session{}).Close(); err != nil {
		t.Fatal(err)
	}
	if err := (*Session)(nil).Close(); err != nil {
		t.Fatal(err)
	}
	good := &Session{port: 12345, reversed: []adb.Client{{Path: fakeADB(t, "exit 0\n"), Serial: "good"}}}
	for range 2 {
		if err := good.Close(); err != nil {
			t.Fatal("successful Close failed", err)
		}
	}
}

func TestCloseBoundsAllDeviceRemovals(t *testing.T) {
	path := fakeADB(t, "exec sleep 60\n")
	s := &Session{reversed: []adb.Client{{Path: path, Serial: "first"}, {Path: path, Serial: "second"}}}
	started := time.Now()
	err := s.Close()
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "serial=first") || !strings.Contains(err.Error(), "serial=second") || time.Since(started) > 7*time.Second {
		t.Fatalf("unbounded/incomplete cleanup: %v", err)
	}
	if s.Close() != err {
		t.Fatal("second Close lost its outcome")
	}
}

func TestPartialStartupReturnsCleanupFailure(t *testing.T) {
	path := fakeADB(t, `
if [ "$1" = "-s" ]; then shift 2; fi
if [ "$1" = "reverse" ] && [ "$2" = "--remove" ]; then echo 'synthetic cleanup failure' >&2; exit 8; fi
if [ "$1" = "shell" ]; then echo 'synthetic startup failure' >&2; exit 7; fi
`)
	s, err := Open(t.Context(), Options{ADBPath: path, Serial: "partial"})
	if s != nil || err == nil || !strings.Contains(err.Error(), "startup failure") || !strings.Contains(err.Error(), "cleanup failure") || !strings.Contains(err.Error(), "serial=partial") {
		t.Fatalf("partial startup suppressed cleanup: %v", err)
	}
}
