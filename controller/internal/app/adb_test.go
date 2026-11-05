package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeADB(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+strings.TrimLeft(body, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
