package render

import (
	"net/url"
	"regexp"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var presentationURL = regexp.MustCompile(`https?://[^\s<>]+`)
var presentationCredential = regexp.MustCompile(`(?im)(^|[\s;,(])(passphrase|password|authorization|access_token|refresh_token|token|psk|api_key)(\s*[:=]\s*)("[^"]*"|'[^']*'|(?:Bearer|Basic)\s+[^\s,;]+|[^\s,;]+)`)

func safePresentationText(value string) string {
	value = presentationCredential.ReplaceAllString(value, "${1}${2}${3}<redacted>")
	value = presentationURL.ReplaceAllStringFunc(value, func(raw string) string {
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
		if fragment := u.EscapedFragment(); strings.Contains(fragment, "=") {
			fragment, err = redactURLParameters(fragment)
			if err != nil {
				return "<invalid URL fragment redacted>"
			}
			u.Fragment, err = url.PathUnescape(fragment)
			if err != nil {
				return "<invalid URL fragment redacted>"
			}
			u.RawFragment = fragment
		}
		return u.String()
	})
	return value
}

func redactURLParameters(value string) (string, error) {
	parts := strings.Split(value, "&")
	for i, part := range parts {
		key, _, _ := strings.Cut(part, "=")
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(decoded) {
		case "password", "passphrase", "token", "access_token", "refresh_token", "authorization", "psk", "api_key", "apikey", "api-key", "client_secret", "x-amz-signature", "x-amz-credential", "x-amz-security-token":
			parts[i] = key + "=%3Credacted%3E"
		}
	}
	return strings.Join(parts, "&"), nil
}

// Clone before sanitizing: evaluation/measurements never see presentation edits.
func safePresentationProto(message proto.Message) proto.Message {
	copy := proto.Clone(message)
	var sanitize func(protoreflect.Message)
	sanitize = func(m protoreflect.Message) {
		if m.Descriptor().Name() == "DiagnosticField" {
			key := m.Descriptor().Fields().ByName("key")
			value := m.Descriptor().Fields().ByName("value")
			if key != nil && value != nil {
				name := strings.ToLower(m.Get(key).String())
				switch name {
				case "passphrase", "password", "token", "access_token", "refresh_token", "authorization", "psk", "api_key":
					m.Set(value, protoreflect.ValueOfString("<redacted>"))
				}
				if strings.HasSuffix(name, ".reason") || strings.HasSuffix(name, "_error") {
					m.Set(value, protoreflect.ValueOfString(safePresentationText(m.Get(value).String())))
				}
			}
		}
		m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			clean := func(value string) string {
				var lines []string
				switch field.Name() {
				case "message", "error", "output", "raw", "raw_link_properties", "raw_capabilities", "url":
					value = safePresentationText(value)
				}
				for line := range strings.SplitSeq(value, "\n") {
					lines = append(lines, cleanDisplayCell(line))
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
	sanitize(copy.ProtoReflect())
	return copy
}
