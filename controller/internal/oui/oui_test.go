package oui

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotBytes(t *testing.T, rows string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, err := fmt.Fprint(w, header+"2026-11-08T00:00:00Z\n"+rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestOfflineLookupParityAndBundledSnapshot(t *testing.T) {
	fixture := snapshotBytes(t, "001122\tVendor24\n0011223\tVendor28\n001122334\tVendor36\n")
	data, err := decode(fixture)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{data: data}
	cases, err := os.ReadFile("../../../testdata/oui_lookup_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(cases)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		mac, want, ok := strings.Cut(line, "\t")
		if !ok || !strings.Contains(store.Vendor(mac), want) {
			t.Errorf("%q: %s, want %s", mac, store.Vendor(mac), want)
		}
	}
	info, err := (&Store{}).Info()
	if err != nil || info.Assignments < 50000 {
		t.Fatalf("bundled snapshot: %+v %v", info, err)
	}
	if got := (&Store{}).Vendor("e8:0a:b9:00:00:01"); !strings.Contains(got, "Cisco") {
		t.Fatal(got)
	}
}

func ieeeFixture(path string) []byte {
	switch path {
	case "oui/oui":
		return []byte("Organization\n00-11-22 (hex) Vendor24\n001122 (base 16) Vendor24\n")
	case "oui28/mam":
		return []byte("Organization\n00-11-22 (hex) Vendor28\n300000-3FFFFF (base 16) Vendor28\n")
	case "oui36/oui36":
		return []byte("Organization\n00-11-22 (hex) Vendor36\n334000-334FFF (base 16) Vendor36\n")
	default:
		return []byte("Organization\n00-50-C2 (hex) IAB vendor\n111000-111FFF (base 16) IAB vendor\n")
	}
}

func TestUpdateAtomicityCancellationAndCorruptCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oui", "registry.tsv.gz")
	store := &Store{path: path}
	good := func(_ context.Context, path string) ([]byte, error) { return ieeeFixture(path), nil }
	info, err := store.update(context.Background(), good)
	if err != nil || info.Assignments != 4 {
		t.Fatalf("update: %+v %v", info, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := (&Store{path: path}).Vendor("00:11:22:33:44:55"); !strings.Contains(got, "Vendor36") {
		t.Fatal(got)
	}
	for _, canceled := range []bool{false, true} {
		requests := 0
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		_, err := store.update(ctx, func(_ context.Context, path string) ([]byte, error) {
			requests++
			if path == "oui36/oui36" {
				return nil, errors.New("synthetic download failure")
			}
			return ieeeFixture(path), nil
		})
		cancel()
		if canceled && requests != 0 {
			t.Fatal("canceled update still downloaded a registry")
		}
		after, readErr := os.ReadFile(path)
		if err == nil || readErr != nil || !bytes.Equal(before, after) || !strings.Contains(store.Vendor("00:11:22:33:44:55"), "Vendor36") {
			t.Fatalf("failed update replaced database: %v %v", err, readErr)
		}
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := &Store{path: path}
	if _, err := corrupt.Info(); err == nil || !strings.Contains(corrupt.Vendor("00:11:22:33:44:55"), "database error") {
		t.Fatal("corrupt cache silently used bundled data")
	}
	if _, err := corrupt.update(context.Background(), good); err != nil || !strings.Contains(corrupt.Vendor("00:11:22:33:44:55"), "Vendor36") {
		t.Fatal("explicit update could not repair corrupt database", err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary file leaked: %v %v", files, err)
	}
}

func TestIEEEValidationAndAmbiguousAssignments(t *testing.T) {
	spaced, err := parseIEEE([]byte("Organization\n00-11-22 (hex) Café Labs\n001122 (base 16) Café\u00a0\u0085Labs\n"), 6)
	if err != nil || spaced["001122"] != "Café Labs" {
		t.Fatalf("Unicode whitespace changed assignee: %v %v", spaced, err)
	}
	duplicates := []byte("Organization\n08-00-30 (hex) CERN\n080030 (base 16) CERN\n08-00-30 (hex) Other\n080030 (base 16) Other\n")
	rows, err := parseIEEE(duplicates, 6)
	if err != nil || rows["080030"] != "multiple IEEE assignees: CERN / Other" {
		t.Fatalf("ambiguous assignment hidden: %v %v", rows, err)
	}
	for _, invalid := range [][]byte{[]byte("<html>error</html>"), []byte("Organization\n"), []byte("Organization\n001122 (base 16) missing hex\n"), []byte("Organization\n00-11-22 (hex) truncated\n"), []byte("Organization\n00-11-22 (hex) Bad\n001122 (base 16) Bad\x1b[2J\n"), []byte{0xff}} {
		if _, err := parseIEEE(invalid, 6); err == nil {
			t.Fatalf("accepted invalid IEEE input: %q", invalid)
		}
	}
	if _, err := parseIEEE([]byte("Organization\n00-11-22 (hex) Bad range\n334001-334FFF (base 16) Bad range\n"), 9); err == nil {
		t.Fatal("accepted unaligned range")
	}
	if _, err := decode(snapshotBytes(t, "001122\tA\n001122\tB\n")); err == nil {
		t.Fatal("accepted duplicate snapshot prefix")
	}
}

func TestOfficialIEEEUpdate(t *testing.T) {
	if os.Getenv("DROPCHECK_OUI_LIVE") != "1" {
		t.Skip("set DROPCHECK_OUI_LIVE=1 to download the public IEEE registries")
	}
	store := &Store{path: filepath.Join(t.TempDir(), "registry.tsv.gz")}
	info, err := store.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Assignments < 50000 || !strings.Contains(store.Vendor("e8:0a:b9:00:00:01"), "Cisco") {
		t.Fatalf("invalid live update: %+v", info)
	}
	t.Logf("validated and atomically saved %d IEEE assignments", info.Assignments)
}
