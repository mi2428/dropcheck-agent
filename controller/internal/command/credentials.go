package command

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidPassphrase accepts 8..63 UTF-8 bytes without controls, or 64 hex digits.
func ValidPassphrase(value string) bool {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	if len(value) >= 8 && len(value) <= 63 {
		return true
	}
	if len(value) != 64 {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return !strings.ContainsRune("0123456789abcdefABCDEF", r) })
}
