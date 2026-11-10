package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOUIStatusNeedsNoAndroidTargetAndCanceledUpdateDoesNotSucceed(t *testing.T) {
	state := &shellState{selected: "not-a-connected-agent", targetAll: true}
	out, err := captureStdout(t, func() error {
		_, err := runReplLine(context.Background(), state, "show oui | display json")
		return err
	})
	var body struct {
		OK    bool
		Lines []string
	}
	if err != nil || json.Unmarshal([]byte(out), &body) != nil || !body.OK || !strings.Contains(strings.Join(body.Lines, "\n"), "IEEE MA-L/MA-M/MA-S/IAB") {
		t.Fatalf("local OUI status: %s %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runReplLine(ctx, state, "request oui update"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled OUI update reported success: %v", err)
	}
}
