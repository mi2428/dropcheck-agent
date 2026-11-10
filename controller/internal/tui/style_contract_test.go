package tui

import (
	"strings"
	"testing"
)

func TestStyleHeadingRoles(t *testing.T) {
	for _, title := range []string{"logs", "causes", "failure history", "IPv6 RA"} {
		if got := detailSectionTitle(detailSection{Title: title}); got == "" || strings.HasSuffix(got, ":") {
			t.Fatalf("section heading has scalar syntax: %q", got)
		}
	}
}
