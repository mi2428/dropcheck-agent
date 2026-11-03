package command

import (
	"strings"
	"unicode"
)

func ValidPassphrase(value string) bool {
	if strings.ContainsFunc(value, unicode.IsControl) {
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
