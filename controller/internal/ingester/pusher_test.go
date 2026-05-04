package ingester

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPushgatewayPusherUsesStableGroupingPath(t *testing.T) {
	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotPath = r.URL.EscapedPath()
		gotBody = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	pusher := NewPushgatewayPusher(server.URL, "dropcheck_results")
	err := pusher.Push(context.Background(), MetricBatch{
		Grouping: map[string]string{
			"device_name":  "phone",
			"device_model": "Pixel 9",
			"festa":        "smoke",
			"wifi_group":   "lab",
			"wifi_essid":   "SHIZK RADIO",
			"wifi_bssid":   "aa:bb:cc:dd:ee:ff",
		},
		Samples: []MetricSample{
			{Name: MetricSuccess, Value: 1},
			{Name: MetricPingSuccess, Labels: map[string]string{"target": "1.1.1.1"}, Value: 1},
		},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	assertGroupingPath(t, gotPath, map[string]string{
		"device_name":  "phone",
		"device_model": "Pixel 9",
		"festa":        "smoke",
		"wifi_group":   "lab",
		"wifi_essid":   "SHIZK RADIO",
		"wifi_bssid":   "aa:bb:cc:dd:ee:ff",
	})
	if strings.Contains(gotPath, "+") {
		t.Fatalf("path encodes spaces as plus signs: %q", gotPath)
	}
	for _, forbidden := range []string{"run_id", "object_key", "step", "command", "result_status", "wifi_band", "wifi_security"} {
		if strings.Contains(gotPath, "/"+url.PathEscape(forbidden)+"/") || strings.Contains(gotBody, forbidden) {
			t.Fatalf("pushed payload contains forbidden identity %q: path=%q body=%s", forbidden, gotPath, gotBody)
		}
	}
	if !strings.Contains(gotBody, "dropcheck_success") {
		t.Fatalf("body missing %s: %s", MetricSuccess, gotBody)
	}
	if !strings.Contains(gotBody, "target") || !strings.Contains(gotBody, "1.1.1.1") {
		t.Fatalf("body missing target label: %s", gotBody)
	}
	if strings.Contains(gotBody, "wifi_group") || strings.Contains(gotBody, "wifi_essid") || strings.Contains(gotBody, "wifi_bssid") {
		t.Fatalf("grouping labels leaked into metric body: %s", gotBody)
	}
}

func assertGroupingPath(t *testing.T, path string, labels map[string]string) {
	t.Helper()
	segments := decodedPathSegments(t, path)
	for key, value := range labels {
		if !containsAdjacentPathSegments(segments, key, value) {
			t.Fatalf("path %q missing grouping %s=%q", path, key, value)
		}
	}
}

func decodedPathSegments(t *testing.T, path string) []string {
	t.Helper()
	raw := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		segment := raw[i]
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			t.Fatalf("decode path segment %q: %v", segment, err)
		}
		if label, ok := strings.CutSuffix(decoded, "@base64"); ok {
			segments = append(segments, label)
			i++
			if i >= len(raw) {
				t.Fatalf("base64 path label %q missing value in path %q", decoded, path)
			}
			value, err := base64.RawURLEncoding.DecodeString(raw[i])
			if err != nil {
				t.Fatalf("decode base64 path segment %q: %v", raw[i], err)
			}
			segments = append(segments, string(value))
			continue
		}
		segments = append(segments, decoded)
	}
	return segments
}

func containsAdjacentPathSegments(segments []string, key string, value string) bool {
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == key && segments[i+1] == value {
			return true
		}
	}
	return false
}
