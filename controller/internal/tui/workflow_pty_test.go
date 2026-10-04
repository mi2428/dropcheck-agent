package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/harness"
)

// The default suite runs workflow_test.go against real core events/reducers.
// This child is only launched by testdata/pty_smoke.py in a real local PTY.
func TestWorkflowPTYChild(t *testing.T) {
	if os.Getenv("DROPCHECK_TUI_PTY_CHILD") != "1" {
		t.Skip("local real-PTY smoke child; see testdata/pty_smoke.py")
	}
	fake := &workflowFake{transportFailure: true, blockAfter: 3}
	compiled, err := harness.Compile(workflowPlan(), workflowAgents()[:1])
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := RunWorkflow(ctx, WorkflowOptions{Compiled: compiled, Runner: fake}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	connects, cleanups := 0, 0
	for _, op := range fake.operations {
		if strings.HasSuffix(op, ":wifi.connect") {
			connects++
		}
		if strings.HasSuffix(op, ":wifi.disconnect") {
			cleanups++
		}
	}
	if connects != 3 || cleanups != 3 || fake.pingCalls != 3 {
		t.Fatalf("PTY keyboard path: connects=%d cleanup=%d ping=%d", connects, cleanups, fake.pingCalls)
	}
}
