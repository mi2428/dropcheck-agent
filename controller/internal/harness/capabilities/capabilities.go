// Package capabilities provides Dropcheck Harness expectations for Wi-Fi device capabilities.
package capabilities

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	statuswifi "dropcheck/controller/internal/harness/wifi"
)

// Result is the Wi-Fi-capabilities-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.WifiCapabilities
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Fields are diagnostic fields reported by the agent.
	Fields []*controlpb.DiagnosticField
	// SupportedBands are the bands Android reports as supported.
	SupportedBands []string
	// UnsupportedBands are the bands Android reports as unsupported.
	UnsupportedBands []string
	// SupportedStandards are the Wi-Fi standards Android reports as supported.
	SupportedStandards []string
	// UnsupportedStandards are the Wi-Fi standards Android reports as unsupported.
	UnsupportedStandards []string
	// SupportedSecurityModes are the security modes Android reports as supported.
	SupportedSecurityModes []string
	// UnsupportedSecurityModes are the security modes Android reports as unsupported.
	UnsupportedSecurityModes []string
	// SupportedFeatures are the feature flags Android reports as supported.
	SupportedFeatures []string
	// UnsupportedFeatures are the feature flags Android reports as unsupported.
	UnsupportedFeatures []string
	// Errors are agent-reported capability collection errors.
	Errors []string
}

// ErrorCount matches the number of capability collection errors.
func ErrorCount() harness.OrderedMetric[int] {
	return harness.Ordered[int]("capabilities.error_count", func(result harness.Result) (int, bool, string) {
		capabilities, ok, reason := from(result)
		return len(capabilities.Errors), ok, reason
	})
}

// Band returns a selector for one Wi-Fi band capability.
func Band(value string) SupportSelector {
	return SupportSelector{
		metric:    "capabilities.band",
		value:     normalizeBand(value),
		supported: func(r Result) []string { return normalizeAll(r.SupportedBands, normalizeBand) },
		unsupported: func(r Result) []string {
			return normalizeAll(r.UnsupportedBands, normalizeBand)
		},
	}
}

// Standard returns a selector for one Wi-Fi standard capability.
func Standard(value string) SupportSelector {
	return SupportSelector{
		metric:      "capabilities.standard",
		value:       statuswifi.StandardName(value),
		supported:   func(r Result) []string { return normalizeAll(r.SupportedStandards, statuswifi.StandardName) },
		unsupported: func(r Result) []string { return normalizeAll(r.UnsupportedStandards, statuswifi.StandardName) },
	}
}

// Security returns a selector for one Wi-Fi security capability.
func Security(value string) SupportSelector {
	return SupportSelector{
		metric:      "capabilities.security",
		value:       normalizeToken(value),
		supported:   func(r Result) []string { return normalizeAll(r.SupportedSecurityModes, normalizeToken) },
		unsupported: func(r Result) []string { return normalizeAll(r.UnsupportedSecurityModes, normalizeToken) },
	}
}

// Feature returns a selector for one Wi-Fi feature capability.
func Feature(value string) SupportSelector {
	return SupportSelector{
		metric:      "capabilities.feature",
		value:       normalizeToken(value),
		supported:   func(r Result) []string { return normalizeAll(r.SupportedFeatures, normalizeToken) },
		unsupported: func(r Result) []string { return normalizeAll(r.UnsupportedFeatures, normalizeToken) },
	}
}

// Field returns matchers for one diagnostic field value.
func Field(key string) FieldSelector {
	return FieldSelector{key: key}
}

// Assert evaluates a custom Wi-Fi capabilities assertion against the typed result view.
func Assert(name string, fn func(Result) error) harness.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result harness.Result) []harness.Finding {
	capabilities, ok, reason := from(result)
	metric := "capabilities.assert." + a.name
	if !ok {
		return []harness.Finding{harness.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(capabilities); err != nil {
		return []harness.Finding{harness.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []harness.Finding{harness.Pass(metric, "passed", "custom assertion passed")}
}

// SupportSelector matches whether a capability is supported or unsupported.
type SupportSelector struct {
	metric      string
	value       string
	supported   func(Result) []string
	unsupported func(Result) []string
}

// Supported requires the selected capability to be supported.
func (s SupportSelector) Supported() harness.Expectation {
	return supportExpectation{selector: s, wantSupported: true}
}

// Unsupported requires the selected capability to be unsupported.
func (s SupportSelector) Unsupported() harness.Expectation {
	return supportExpectation{selector: s}
}

type supportExpectation struct {
	selector      SupportSelector
	wantSupported bool
}

func (e supportExpectation) Evaluate(result harness.Result) []harness.Finding {
	capabilities, ok, reason := from(result)
	if !ok {
		return []harness.Finding{harness.Fail(e.selector.metric, "<missing>", e.expected(), reason)}
	}
	if e.wantSupported {
		if slices.Contains(e.selector.supported(capabilities), e.selector.value) {
			return []harness.Finding{harness.Pass(e.selector.metric, e.selector.value, e.expected())}
		}
		return []harness.Finding{harness.Fail(e.selector.metric, describeSupport(e.selector.supported(capabilities), e.selector.unsupported(capabilities)), e.expected(), "capability is not supported")}
	}
	if slices.Contains(e.selector.unsupported(capabilities), e.selector.value) {
		return []harness.Finding{harness.Pass(e.selector.metric, e.selector.value, e.expected())}
	}
	return []harness.Finding{harness.Fail(e.selector.metric, describeSupport(e.selector.supported(capabilities), e.selector.unsupported(capabilities)), e.expected(), "capability is not unsupported")}
}

func (e supportExpectation) expected() string {
	if e.wantSupported {
		return "supported " + e.selector.value
	}
	return "unsupported " + e.selector.value
}

// FieldSelector matches one diagnostic field in the capabilities payload.
type FieldSelector struct {
	key string
}

// Eq requires the field value to equal value.
func (s FieldSelector) Eq(value string) harness.Expectation {
	return fieldExpectation{selector: s, op: "==", value: value, pass: func(got string) bool { return got == value }}
}

// Contains requires the field value to contain value.
func (s FieldSelector) Contains(value string) harness.Expectation {
	return fieldExpectation{selector: s, op: "contains", value: value, pass: func(got string) bool { return strings.Contains(got, value) }}
}

type fieldExpectation struct {
	selector FieldSelector
	op       string
	value    string
	pass     func(string) bool
}

func (e fieldExpectation) Evaluate(result harness.Result) []harness.Finding {
	capabilities, ok, reason := from(result)
	metric := "capabilities.field." + e.selector.key
	if !ok {
		return []harness.Finding{harness.Fail(metric, "<missing>", e.op+" "+e.value, reason)}
	}
	for _, field := range capabilities.Fields {
		if field.GetKey() != e.selector.key {
			continue
		}
		if e.pass(field.GetValue()) {
			return []harness.Finding{harness.Pass(metric, field.GetValue(), e.op+" "+e.value)}
		}
		return []harness.Finding{harness.Fail(metric, field.GetValue(), e.op+" "+e.value, "field constraint failed")}
	}
	return []harness.Finding{harness.Fail(metric, "<missing>", e.op+" "+e.value, "field not found")}
}

func from(result harness.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	value := raw.GetWifiCapabilities()
	if value == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not wifi capabilities", raw.GetPayload())
	}
	return Result{
		Raw:                      value,
		Status:                   raw.GetStatus(),
		Fields:                   value.GetFields(),
		SupportedBands:           value.GetSupportedBands(),
		UnsupportedBands:         value.GetUnsupportedBands(),
		SupportedStandards:       value.GetSupportedStandards(),
		UnsupportedStandards:     value.GetUnsupportedStandards(),
		SupportedSecurityModes:   value.GetSupportedSecurityModes(),
		UnsupportedSecurityModes: value.GetUnsupportedSecurityModes(),
		SupportedFeatures:        value.GetSupportedFeatures(),
		UnsupportedFeatures:      value.GetUnsupportedFeatures(),
		Errors:                   value.GetErrors(),
	}, true, ""
}

func describeSupport(supported []string, unsupported []string) string {
	return "supported=" + strings.Join(supported, ",") + " unsupported=" + strings.Join(unsupported, ",")
}

func normalizeAll(values []string, normalize func(string) string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, normalize(value))
	}
	return normalized
}

func normalizeBand(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "2.4", "24", "2.4g", "24g", "2.4ghz", "24ghz":
		return "2.4ghz"
	case "5", "5g", "5ghz":
		return "5ghz"
	case "6", "6g", "6ghz":
		return "6ghz"
	case "60", "60g", "60ghz":
		return "60ghz"
	default:
		return normalized
	}
}

func normalizeToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}
