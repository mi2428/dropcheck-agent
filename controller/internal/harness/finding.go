package harness

import "fmt"

// Finding is the result of evaluating one expectation.
type Finding struct {
	Target   string `json:"target,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	CheckID  string `json:"check_id,omitempty"`
	// Check is the check that produced the finding.
	Check string
	// Metric is the stable metric or assertion name.
	Metric string
	// Observed is the value seen in the command result.
	Observed string
	// Expected is the constraint or assertion that should have held.
	Expected string
	// Passed reports whether the expectation succeeded.
	Passed        bool
	Missing       bool
	ObservedValue any `json:"observed_value,omitempty"`
	ExpectedValue any `json:"expected_value,omitempty"`
	// Message carries additional diagnostic context for failures.
	Message string
}

// Fail returns a failed expectation finding.
func Fail(metric string, observed string, expected string, message string) Finding {
	return Finding{Metric: metric, Observed: observed, Expected: expected, Passed: false, Message: message}
}

// Pass returns a passed expectation finding.
func Pass(metric string, observed string, expected string) Finding {
	return Finding{Metric: metric, Observed: observed, Expected: expected, Passed: true}
}

// MissingFinding describes an unavailable observation, not an observed zero.
func MissingFinding(metric, observed, expected, message string) Finding {
	return Finding{Metric: metric, Observed: observed, Expected: expected, Missing: true, Message: message}
}

func (f Finding) failureString() string {
	if f.Message != "" {
		return fmt.Sprintf("%s: observed=%s expected=%s: %s", f.Metric, f.Observed, f.Expected, f.Message)
	}
	return fmt.Sprintf("%s: observed=%s expected=%s", f.Metric, f.Observed, f.Expected)
}
