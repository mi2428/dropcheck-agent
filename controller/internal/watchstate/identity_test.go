package watchstate

import "testing"

func TestDisplayCheckNameFormatsInternalSteps(t *testing.T) {
	tests := map[string]string{
		"connect":        "Connect",
		"wait_connected": "Wait Connected",
		"disconnect":     "Disconnect",
		"forget":         "Forget",
		"target":         "Target",
		"Ping CF IPv6":   "Ping CF IPv6",
	}
	for input, want := range tests {
		if got := DisplayCheckName(input); got != want {
			t.Fatalf("DisplayCheckName(%q) = %q, want %q", input, got, want)
		}
	}
}
