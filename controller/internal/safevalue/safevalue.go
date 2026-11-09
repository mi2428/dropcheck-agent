// Package safevalue projects acquired values for presentation and export.
// It never redacts literal network identities or changes typed measurements.
package safevalue

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var textURL = regexp.MustCompile(`(?i)https?://[^\s<>]+`)
var credential = regexp.MustCompile(`(?im)(^|[\s;,=({\[])(["']?(?:passphrase|password|authorization|access_token|refresh_token|token|psk|api_key|apikey|api-key|client_secret|x-amz-signature|x-amz-credential|x-amz-security-token)["']?)(\s*[:=]\s*)("(?:\\.|[^"\\])*(?:"|$)|'(?:\\.|[^'\\])*(?:'|$)|(?:Bearer|Basic)\s+[^\s,;}\]]+|[^\s,;}\]]+)`)

// Normalize removes terminal escapes and replaces control/bidi characters with
// spaces. Unlike Text, it preserves literal identities containing credential keys.
func Normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u061c' || r == '\u200b' || r == '\u200e' || r == '\u200f' || r == '\ufeff' || r == '\u2028' || r == '\u2029' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2060' && r <= '\u2069') {
			return ' '
		}
		return r
	}, ansi.Strip(value))
}

// Text normalizes free text and redacts credential fields and URL credentials.
// Logical newlines are preserved. Invalid URL parameter names fail closed.
func Text(value string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = Normalize(lines[i])
	}
	value = strings.Join(lines, "\n")
	value = credential.ReplaceAllString(value, "${1}${2}${3}<redacted>")
	return textURL.ReplaceAllStringFunc(value, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "<unavailable URL>"
		}
		if u.User != nil {
			u.User = url.User("redacted")
		}
		query, err := redactURLParameters(u.RawQuery)
		if err != nil {
			return "<invalid URL query redacted>"
		}
		u.RawQuery = query
		if fragment := u.Fragment; strings.Contains(fragment, "=") {
			prefix := ""
			if route, parameters, ok := strings.Cut(fragment, "?"); ok {
				prefix, fragment = route+"?", parameters
			}
			fragment, err = redactURLParameters(fragment)
			if err != nil {
				return "<invalid URL fragment redacted>"
			}
			fragment = prefix + fragment
			u.Fragment, err = url.PathUnescape(fragment)
			if err != nil {
				return "<invalid URL fragment redacted>"
			}
			u.RawFragment = fragment
		}
		return u.String()
	})
}

func credentialKey(value string) bool {
	switch strings.ToLower(value) {
	case "password", "passphrase", "token", "access_token", "refresh_token", "authorization", "psk", "api_key", "apikey", "api-key", "client_secret", "x-amz-signature", "x-amz-credential", "x-amz-security-token":
		return true
	}
	return false
}

func redactURLParameters(value string) (string, error) {
	parts := strings.Split(value, "&")
	for i, part := range parts {
		key, _, _ := strings.Cut(part, "=")
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			return "", err
		}
		if credentialKey(decoded) {
			parts[i] = key + "=%3Credacted%3E"
		}
	}
	return strings.Join(parts, "&"), nil
}

// Field uses the field's semantic label to preserve literal identities while
// redacting credentials. Control normalization is performed separately by Proto
// or by the display adapter for literal fields.
func Field(label, value string) string {
	name := strings.ToLower(strings.TrimSpace(Normalize(label)))
	if credentialKey(name) {
		return "<redacted>"
	}
	switch name {
	case "ssid", "bssid", "ap_mld_mac_address":
		return value
	default:
		return Text(value)
	}
}

// Proto returns an independently owned safe projection; measurements and
// evaluation must keep the acquired message instead. A nil message stays nil.
func Proto(message proto.Message) proto.Message {
	copy := proto.Clone(message)
	SanitizeProto(copy)
	return copy
}

// SanitizeProto edits an owned message in place. Callers must clone shared
// acquisition data first; numeric values and literal SSID/BSSID are preserved.
func SanitizeProto(message proto.Message) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return
	}
	var sanitize func(protoreflect.Message)
	sanitize = func(m protoreflect.Message) {
		if m.Descriptor().Name() == "DiagnosticField" {
			key := m.Descriptor().Fields().ByName("key")
			value := m.Descriptor().Fields().ByName("value")
			if key != nil && value != nil {
				m.Set(value, protoreflect.ValueOfString(Field(m.Get(key).String(), m.Get(value).String())))
			}
		}
		m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			clean := func(value string) string {
				switch field.Name() {
				case "message", "error", "errors", "warnings", "output", "raw", "raw_link_properties", "raw_capabilities", "url",
					"state", "detailed_state", "supplicant_state", "passpoint_fqdn", "passpoint_provider_friendly_name", "passpoint_unique_id",
					"applicable_redactions", "network_specifier", "http_proxy", "private_dns_server_name", "domains", "routes", "capabilities", "permissions":
					value = Text(value)
				}
				lines := strings.Split(value, "\n")
				for i := range lines {
					lines[i] = Normalize(lines[i])
				}
				return strings.Join(lines, "\n")
			}
			if field.IsList() {
				list := value.List()
				for i := 0; i < list.Len(); i++ {
					if field.Kind() == protoreflect.StringKind {
						list.Set(i, protoreflect.ValueOfString(clean(list.Get(i).String())))
					}
					if field.Kind() == protoreflect.MessageKind {
						sanitize(list.Get(i).Message())
					}
				}
			} else if field.Kind() == protoreflect.StringKind {
				m.Set(field, protoreflect.ValueOfString(clean(value.String())))
			} else if field.Kind() == protoreflect.MessageKind {
				sanitize(value.Message())
			}
			return true
		})
	}
	sanitize(message.ProtoReflect())
}
