package festival

import (
	"cmp"
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

// Result is the generic input seen by every expectation.
type Result struct {
	// Network is the Wi-Fi network currently under test.
	Network Network
	// Check is the display name of the running check.
	Check string
	// Run is the raw execution result produced by the operation runner.
	Run RunResult
}

// RunResult is the subset of runner.Result that expectations need.
//
// It is an alias-shaped struct rather than the concrete runner.Result so tests
// can provide fake runners without depending on the control server.
type RunResult struct {
	// CommandID is the controller command identifier for this execution.
	CommandID string
	// Raw is the protobuf result returned by the Android agent.
	Raw *controlpb.CommandResult
}

// Expectation evaluates one or more findings against a check result.
type Expectation interface {
	Evaluate(Result) []Finding
}

// Assert evaluates a custom assertion against the generic Dropcheck Festival
// result.
//
// Prefer check-specific packages such as festival/ping when they expose a typed
// result view. Assert is useful for new check kinds or one-off conditions that
// need direct access to the raw controlpb.CommandResult.
func Assert(name string, fn func(Result) error) Expectation {
	return customAssertion{name: name, fn: fn}
}

type customAssertion struct {
	name string
	fn   func(Result) error
}

func (a customAssertion) Evaluate(result Result) []Finding {
	metric := "assert." + a.name
	if err := a.fn(result); err != nil {
		return []Finding{Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []Finding{Pass(metric, "passed", "custom assertion passed")}
}

// Observer extracts a metric value from a result.
type Observer[T any] func(Result) (value T, ok bool, reason string)

type orderedConstraint[T cmp.Ordered] struct {
	op    string
	value T
	pass  func(T, T) bool
}

// OrderedMetric is a chainable matcher for ordered values.
type OrderedMetric[T cmp.Ordered] struct {
	name        string
	observe     Observer[T]
	format      func(T) string
	constraints []orderedConstraint[T]
}

// Ordered creates a chainable ordered metric.
func Ordered[T cmp.Ordered](name string, observe Observer[T]) OrderedMetric[T] {
	return OrderedMetric[T]{name: name, observe: observe, format: formatValue[T]}
}

// Format sets the display formatter used in findings.
func (m OrderedMetric[T]) Format(format func(T) string) OrderedMetric[T] {
	m.format = format
	return m
}

// Eq requires the metric to equal value.
func (m OrderedMetric[T]) Eq(value T) OrderedMetric[T] {
	return m.with("==", value, func(got T, want T) bool { return got == want })
}

// Ne requires the metric not to equal value.
func (m OrderedMetric[T]) Ne(value T) OrderedMetric[T] {
	return m.with("!=", value, func(got T, want T) bool { return got != want })
}

// Gt requires the metric to be greater than value.
func (m OrderedMetric[T]) Gt(value T) OrderedMetric[T] {
	return m.with(">", value, func(got T, want T) bool { return got > want })
}

// Ge requires the metric to be greater than or equal to value.
func (m OrderedMetric[T]) Ge(value T) OrderedMetric[T] {
	return m.with(">=", value, func(got T, want T) bool { return got >= want })
}

// Lt requires the metric to be less than value.
func (m OrderedMetric[T]) Lt(value T) OrderedMetric[T] {
	return m.with("<", value, func(got T, want T) bool { return got < want })
}

// Le requires the metric to be less than or equal to value.
func (m OrderedMetric[T]) Le(value T) OrderedMetric[T] {
	return m.with("<=", value, func(got T, want T) bool { return got <= want })
}

// Between requires the metric to be within [min, max].
func (m OrderedMetric[T]) Between(min T, max T) OrderedMetric[T] {
	return m.Ge(min).Le(max)
}

func (m OrderedMetric[T]) with(op string, value T, pass func(T, T) bool) OrderedMetric[T] {
	m.constraints = append(m.constraints, orderedConstraint[T]{op: op, value: value, pass: pass})
	return m
}

// Evaluate evaluates every chained constraint.
func (m OrderedMetric[T]) Evaluate(result Result) []Finding {
	if len(m.constraints) == 0 {
		return nil
	}
	observed, ok, reason := m.observe(result)
	if !ok {
		findings := make([]Finding, 0, len(m.constraints))
		for _, constraint := range m.constraints {
			findings = append(findings, Fail(m.name, "<missing>", constraint.op+" "+m.format(constraint.value), reason))
		}
		return findings
	}
	findings := make([]Finding, 0, len(m.constraints))
	for _, constraint := range m.constraints {
		expected := constraint.op + " " + m.format(constraint.value)
		if constraint.pass(observed, constraint.value) {
			findings = append(findings, Pass(m.name, m.format(observed), expected))
			continue
		}
		findings = append(findings, Fail(m.name, m.format(observed), expected, "constraint failed"))
	}
	return findings
}

// BoolMetric is a chainable matcher for boolean values.
type BoolMetric struct {
	name        string
	observe     Observer[bool]
	constraints []boolConstraint
}

type boolConstraint struct {
	expected bool
}

// Bool creates a boolean metric.
func Bool(name string, observe Observer[bool]) BoolMetric {
	return BoolMetric{name: name, observe: observe}
}

// Eq requires the boolean metric to equal value.
func (m BoolMetric) Eq(value bool) BoolMetric {
	m.constraints = append(m.constraints, boolConstraint{expected: value})
	return m
}

// IsTrue requires the boolean metric to be true.
func (m BoolMetric) IsTrue() BoolMetric {
	return m.Eq(true)
}

// IsFalse requires the boolean metric to be false.
func (m BoolMetric) IsFalse() BoolMetric {
	return m.Eq(false)
}

// Evaluate evaluates the boolean metric.
func (m BoolMetric) Evaluate(result Result) []Finding {
	if len(m.constraints) == 0 {
		return nil
	}
	observed, ok, reason := m.observe(result)
	if !ok {
		findings := make([]Finding, 0, len(m.constraints))
		for _, constraint := range m.constraints {
			findings = append(findings, Fail(m.name, "<missing>", fmt.Sprintf("== %t", constraint.expected), reason))
		}
		return findings
	}
	findings := make([]Finding, 0, len(m.constraints))
	for _, constraint := range m.constraints {
		expected := fmt.Sprintf("== %t", constraint.expected)
		if observed == constraint.expected {
			findings = append(findings, Pass(m.name, fmt.Sprintf("%t", observed), expected))
			continue
		}
		findings = append(findings, Fail(m.name, fmt.Sprintf("%t", observed), expected, "constraint failed"))
	}
	return findings
}

func formatValue[T any](value T) string {
	return strings.TrimSpace(fmt.Sprint(value))
}
