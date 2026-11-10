package tuiconfig

import (
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestExampleTUIPlanValidates(t *testing.T) {
	t.Setenv("DROPCHECK_WIFI_PSK", "synthetic-passphrase")
	plan, err := LoadFile("../../../examples/tui.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := tuirun.Validate(plan); err != nil {
		t.Fatal(err)
	}
}
