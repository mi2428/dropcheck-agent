package render

import (
	"bytes"
	"encoding/json"
	"strings"

	"dropcheck/controller/internal/harness"
)

// SafeReportJSON exports a retained report without changing the typed evidence.
// Sanitize decoded string values, not JSON source (where escaping hides URLs).
func SafeReportJSON(report harness.Report) ([]byte, error) {
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.MarshalIndent(safeReportValue(value), "", "  ")
}

func safeReportValue(value any) any {
	switch v := value.(type) {
	case string:
		return safePresentationText(v)
	case []any:
		for i := range v {
			v[i] = safeReportValue(v[i])
		}
	case map[string]any:
		// Protobuf diagnostic fields carry a dynamic key and an unlabelled value.
		key, _ := v["Key"].(string)
		if key == "" {
			key, _ = v["key"].(string)
		}
		secretField := isSecretReportField(key)
		for name, item := range v {
			if isSecretReportField(name) || secretField && strings.EqualFold(name, "value") {
				v[name] = "<redacted>"
			} else if strings.EqualFold(name, "ssid") || strings.EqualFold(name, "bssid") {
				// A literal identity is not a free-text diagnostic or a credential.
				v[name] = item
			} else {
				v[name] = safeReportValue(item)
			}
		}
	}
	return value
}

func isSecretReportField(key string) bool {
	switch strings.ToLower(key) {
	case "passphrase", "password", "authorization", "access_token", "refresh_token", "token", "psk", "api_key", "apikey", "client_secret":
		return true
	}
	return false
}
