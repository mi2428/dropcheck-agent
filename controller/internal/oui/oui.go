// Package oui resolves MAC addresses locally against IEEE public assignments.
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

//go:embed data/registry.tsv.gz
var bundled []byte

const maxBytes = 16 << 20
const header = "# dropcheck-oui-v1\t"

var sources = []struct {
	path   string
	digits int
}{{"oui/oui", 6}, {"oui28/mam", 7}, {"oui36/oui36", 9}, {"iab/iab", 9}}

var hexLine = regexp.MustCompile(`^([0-9A-F]{2}-[0-9A-F]{2}-[0-9A-F]{2})\s+\(hex\)\s+(.+)$`)
var baseLine = regexp.MustCompile(`^([0-9A-F]{6})(?:-([0-9A-F]{6}))?\s+\(base 16\)\s+(.+)$`)
var prefixPattern = regexp.MustCompile(`^(?:[0-9A-F]{6}|[0-9A-F]{7}|[0-9A-F]{9})$`)

type registry struct {
	fetched string
	entries map[string]string
}

type Info struct {
	FetchedAt   string
	Assignments int
	Source      string
}

// Store keeps an immutable loaded snapshot; failed updates never replace it.
type Store struct {
	path  string
	mu    sync.Mutex
	data  *registry
	local bool
}

var defaultStore = sync.OnceValues(func() (*Store, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(directory, "dropcheck", "oui", "registry.tsv.gz")}, nil
})

func Default() (*Store, error) { return defaultStore() }

func validName(name string) bool {
	return name != "" && len(name) <= 2048 && utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl)
}

func parseIEEE(data []byte, digits int) (map[string]string, error) {
	if len(data) > maxBytes || !utf8.Valid(data) || !bytes.Contains(data[:min(len(data), 1024)], []byte("Organization")) {
		return nil, fmt.Errorf("invalid IEEE registry header or size")
	}
	assignments := make(map[string]map[string]bool)
	pending := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if match := hexLine.FindStringSubmatch(line); match != nil {
			if pending != "" {
				return nil, fmt.Errorf("incomplete IEEE assignment")
			}
			pending = strings.ReplaceAll(match[1], "-", "")
		}
		if !strings.Contains(line, "(base 16)") {
			continue
		}
		match := baseLine.FindStringSubmatch(line)
		if match == nil || pending == "" {
			return nil, fmt.Errorf("malformed IEEE assignment")
		}
		prefix := match[1]
		if digits == 6 {
			if prefix != pending || match[2] != "" {
				return nil, fmt.Errorf("inconsistent IEEE MA-L assignment")
			}
		} else {
			n := digits - 6
			if match[2] == "" || match[1][n:] != strings.Repeat("0", 6-n) || match[2] != match[1][:n]+strings.Repeat("F", 6-n) {
				return nil, fmt.Errorf("inconsistent IEEE assignment range")
			}
			prefix = pending + match[1][:n]
		}
		name := strings.Join(strings.Fields(match[3]), " ")
		if !validName(name) {
			return nil, fmt.Errorf("invalid IEEE assignment %s", prefix)
		}
		if assignments[prefix] == nil {
			assignments[prefix] = make(map[string]bool)
		}
		assignments[prefix][name] = true
		pending = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(assignments) == 0 || pending != "" {
		return nil, fmt.Errorf("empty or incomplete IEEE registry")
	}
	entries := make(map[string]string, len(assignments))
	for prefix, names := range assignments {
		values := make([]string, 0, len(names))
		for name := range names {
			values = append(values, name)
		}
		sort.Strings(values)
		value := strings.Join(values, " / ")
		if len(values) > 1 {
			value = "multiple IEEE assignees: " + value
		}
		entries[prefix] = value
	}
	return entries, nil
}

func decode(data []byte) (*registry, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	plain, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || len(plain) > maxBytes || !utf8.Valid(plain) {
		return nil, fmt.Errorf("invalid OUI snapshot compression, size or encoding")
	}
	lines := strings.Split(strings.TrimSuffix(string(plain), "\n"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], header) {
		return nil, fmt.Errorf("invalid OUI snapshot header")
	}
	fetched := strings.TrimPrefix(lines[0], header)
	if _, err := time.Parse(time.RFC3339, fetched); err != nil {
		return nil, fmt.Errorf("invalid OUI snapshot date")
	}
	result := &registry{fetched: fetched, entries: make(map[string]string)}
	for _, line := range lines[1:] {
		prefix, name, ok := strings.Cut(line, "\t")
		if !ok || !prefixPattern.MatchString(prefix) || !validName(name) || result.entries[prefix] != "" {
			return nil, fmt.Errorf("invalid or duplicate OUI snapshot assignment")
		}
		result.entries[prefix] = name
	}
	return result, nil
}

func (s *Store) load() error {
	if s.data != nil {
		return nil
	}
	data := bundled
	if s.path != "" {
		file, err := os.Open(s.path)
		if err == nil {
			defer file.Close()
			data, err = io.ReadAll(io.LimitReader(file, maxBytes+1))
			if err != nil {
				return err
			}
			if len(data) > maxBytes {
				return fmt.Errorf("oversized OUI snapshot")
			}
			s.local = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	var err error
	s.data, err = decode(data)
	return err
}

func (s *Store) Info() (Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Info{}, err
	}
	source := "bundled IEEE snapshot"
	if s.local {
		source = "local IEEE update"
	}
	return Info{s.data.fetched, len(s.data.entries), source}, nil
}

func (s *Store) Vendor(address string) string {
	mac, err := net.ParseMAC(address)
	if err != nil || len(mac) != 6 {
		return "? (invalid MAC)"
	}
	if bytes.Equal(mac, make([]byte, 6)) {
		return "? (unspecified)"
	}
	if mac[0]&1 != 0 {
		return "? (group MAC)"
	}
	if mac[0]&2 != 0 {
		return "? (local MAC)"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return "? (OUI database error: " + err.Error() + ")"
	}
	key := strings.ToUpper(hex.EncodeToString(mac))
	for _, digits := range []int{9, 7, 6} {
		if name := s.data.entries[key[:digits]]; name != "" {
			return name
		}
	}
	return "? (unregistered)"
}

func Vendor(address string) string {
	store, err := Default()
	if err != nil {
		return "? (OUI database error: " + err.Error() + ")"
	}
	return store.Vendor(address)
}

func download(ctx context.Context, source string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://standards-oui.ieee.org/"+source+".txt", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "dropcheck-agent/1.0")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("IEEE registry redirect refused") }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IEEE registry HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err == nil && len(data) > maxBytes {
		err = fmt.Errorf("oversized IEEE registry")
	}
	return data, err
}

func (s *Store) Update(ctx context.Context) (Info, error) { return s.update(ctx, download) }

func (s *Store) update(ctx context.Context, fetch func(context.Context, string) ([]byte, error)) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return Info{}, fmt.Errorf("OUI update storage is not configured")
	}
	entries := make(map[string]string)
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return Info{}, err
		}
		data, err := fetch(ctx, source.path)
		if err != nil {
			return Info{}, fmt.Errorf("%s: %w", source.path, err)
		}
		rows, err := parseIEEE(data, source.digits)
		if err != nil {
			return Info{}, fmt.Errorf("%s: %w", source.path, err)
		}
		for prefix, name := range rows {
			if previous := entries[prefix]; previous != "" && previous != name {
				return Info{}, fmt.Errorf("conflicting IEEE prefix %s", prefix)
			}
			entries[prefix] = name
		}
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	fetched := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintln(writer, header+fetched)
	for _, key := range keys {
		fmt.Fprintf(writer, "%s\t%s\n", key, entries[key])
	}
	if err := writer.Close(); err != nil {
		return Info{}, err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return Info{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".oui-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(compressed.Bytes())
	syncErr := file.Sync()
	closeErr := file.Close()
	for _, err := range []error{writeErr, syncErr, closeErr, ctx.Err()} {
		if err != nil {
			return Info{}, err
		}
	}
	if err := os.Rename(file.Name(), s.path); err != nil {
		return Info{}, err
	}
	s.data, s.local = &registry{fetched, entries}, true
	return Info{fetched, len(entries), "local IEEE update"}, nil
}
