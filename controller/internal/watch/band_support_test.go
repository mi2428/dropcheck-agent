package watch

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestBandSupportSkipsUnsupportedTargetBand(t *testing.T) {
	support := bandSupportFromCapabilities(&controlpb.WifiCapabilities{
		SupportedBands:   []string{"2.4GHz", "5GHz"},
		UnsupportedBands: []string{"6GHz", "60GHz"},
	})
	if _, skip := support.skipReason("5ghz"); skip {
		t.Fatal("5GHz target should not be skipped when the device reports 5GHz support")
	}
	reason, skip := support.skipReason("6ghz")
	if !skip {
		t.Fatal("6GHz target should be skipped when the device reports 6GHz unsupported")
	}
	if !strings.Contains(reason, "6GHz") {
		t.Fatalf("skip reason = %q, want band name", reason)
	}
}

func TestBandSupportSkipsBandsMissingFromSupportedList(t *testing.T) {
	support := bandSupportFromCapabilities(&controlpb.WifiCapabilities{SupportedBands: []string{"2.4GHz", "5GHz"}})
	if _, skip := support.skipReason("6ghz"); !skip {
		t.Fatal("6GHz target should be skipped when supported bands are known and omit 6GHz")
	}
}

func TestBandSupportAllowsUnknownCapabilities(t *testing.T) {
	var support bandSupport
	if _, skip := support.skipReason("6ghz"); skip {
		t.Fatal("unknown capabilities should not skip targets")
	}
}
