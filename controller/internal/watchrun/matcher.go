package watchrun

import "dropcheck/controller/internal/controlpb"

// Result is the input to watch expectations and built-in Shell profile checks.
type Result struct {
	Network Network
	Check   string
	Run     RunResult
}

type RunResult struct {
	CommandID string
	Raw       *controlpb.CommandResult
}

type Expectation interface {
	Evaluate(Result) []Finding
}
