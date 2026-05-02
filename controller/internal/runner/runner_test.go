package runner_test

import (
	"context"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/runner"
)

func TestRunnerRejectsNilServer(t *testing.T) {
	_, err := runner.New(nil).Run(context.Background(), control.AgentInfo{ID: "agent-1"}, command.WifiStatusOperation())
	if err == nil || !strings.Contains(err.Error(), "no control server") {
		t.Fatalf("Run() error = %v, want no control server", err)
	}
}
