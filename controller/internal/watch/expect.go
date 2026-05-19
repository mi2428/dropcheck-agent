package watch

import (
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
)

// Matcher is one compiled expectation for a probe metric.
type Matcher struct {
	Metric string
	Op     string
	Want   string
	Values []string
	Mode   string
}

// Finding describes one expectation mismatch emitted by a failed check.
type Finding struct {
	Target   string `json:"target,omitempty"`
	Check    string `json:"check,omitempty"`
	Metric   string `json:"metric"`
	Observed string `json:"observed"`
	Expected string `json:"expected"`
	Message  string `json:"message,omitempty"`
}

func compileMatchers(values map[string]any) ([]Matcher, error) {
	if len(values) == 0 {
		return nil, nil
	}
	matchers := make([]Matcher, 0, len(values))
	for metric, raw := range values {
		matcher, err := compileMatcher(metric, raw)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)
	}
	return matchers, nil
}

func compileMatcher(metric string, raw any) (Matcher, error) {
	metric = strings.TrimSpace(metric)
	if metric == "" {
		return Matcher{}, fmt.Errorf("metric name is empty")
	}
	switch value := raw.(type) {
	case bool:
		return Matcher{Metric: metric, Op: "==", Want: strconv.FormatBool(value)}, nil
	case int:
		return Matcher{Metric: metric, Op: "==", Want: strconv.Itoa(value)}, nil
	case int64:
		return Matcher{Metric: metric, Op: "==", Want: strconv.FormatInt(value, 10)}, nil
	case uint64:
		return Matcher{Metric: metric, Op: "==", Want: strconv.FormatUint(value, 10)}, nil
	case float64:
		return Matcher{Metric: metric, Op: "==", Want: strconv.FormatFloat(value, 'f', -1, 64)}, nil
	case string:
		return compileStringMatcher(metric, value)
	case []any:
		values, err := stringValues(value)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s: %w", metric, err)
		}
		return Matcher{Metric: metric, Op: "exact_values", Values: values}, nil
	case map[string]any:
		return compileMapMatcher(metric, value)
	default:
		return Matcher{}, fmt.Errorf("%s uses unsupported expectation value %T", metric, raw)
	}
}

func compileMapMatcher(metric string, raw map[string]any) (Matcher, error) {
	mode, err := matcherMode(raw["mode"])
	if err != nil {
		return Matcher{}, fmt.Errorf("%s mode: %w", metric, err)
	}
	if value, ok := raw["cidr"]; ok {
		values, err := stringListValueFromRaw(value)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s cidr: %w", metric, err)
		}
		return Matcher{Metric: metric, Op: "cidr", Values: values, Mode: mode}, nil
	}
	if value, ok := raw["cidrs"]; ok {
		values, err := stringListValueFromRaw(value)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s cidrs: %w", metric, err)
		}
		return Matcher{Metric: metric, Op: "cidr", Values: values, Mode: mode}, nil
	}
	if value, ok := raw["contains"]; ok {
		values, err := stringListValueFromRaw(value)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s contains: %w", metric, err)
		}
		return Matcher{Metric: metric, Op: "contains_values", Values: values, Mode: mode}, nil
	}
	if value, ok := raw["exact"]; ok {
		values, err := stringListValueFromRaw(value)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s exact: %w", metric, err)
		}
		return Matcher{Metric: metric, Op: "exact_values", Values: values}, nil
	}
	return Matcher{}, fmt.Errorf("%s uses unsupported map expectation; use cidr, cidrs, contains, or exact", metric)
}

func matcherMode(raw any) (string, error) {
	mode := "at_least"
	if raw != nil {
		mode = strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
	}
	switch mode {
	case "", "any", "at_least", "at-least":
		return "at_least", nil
	case "all", "exact":
		return "exact", nil
	default:
		return "", fmt.Errorf("unsupported mode %q; use at_least or exact", mode)
	}
}

func stringListValueFromRaw(raw any) ([]string, error) {
	switch value := raw.(type) {
	case []any:
		return stringValues(value)
	case []string:
		return cleanStringValues(value), nil
	case string:
		return cleanStringValues([]string{value}), nil
	default:
		return cleanStringValues([]string{fmt.Sprint(raw)}), nil
	}
}

func stringValues(values []any) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			out = append(out, typed)
		case int, int64, uint64, float64, bool:
			out = append(out, fmt.Sprint(typed))
		default:
			return nil, fmt.Errorf("unsupported list value %T", value)
		}
	}
	return cleanStringValues(out), nil
}

func cleanStringValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func compileStringMatcher(metric string, value string) (Matcher, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Matcher{Metric: metric, Op: "==", Want: ""}, nil
	}
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		if after, ok := strings.CutPrefix(value, op); ok {
			return Matcher{Metric: metric, Op: op, Want: strings.TrimSpace(after)}, nil
		}
	}
	if rest, ok := strings.CutPrefix(value, "contains "); ok {
		return Matcher{Metric: metric, Op: "contains", Want: strings.TrimSpace(rest)}, nil
	}
	if value == "present" {
		return Matcher{Metric: metric, Op: "present"}, nil
	}
	return Matcher{Metric: metric, Op: "==", Want: value}, nil
}

func evaluateMatchers(target Target, check Check, metrics map[string]Value) []Finding {
	var findings []Finding
	for _, matcher := range check.compiledExpect {
		observed, ok := metrics[matcher.Metric]
		if matcher.Op == "present" {
			if !ok || observed.String() == "" {
				findings = append(findings, finding(target, check, matcher, observed, "metric is not present"))
			}
			continue
		}
		if !ok {
			findings = append(findings, finding(target, check, matcher, Value{}, "metric is missing"))
			continue
		}
		if !matcher.matches(observed) {
			findings = append(findings, finding(target, check, matcher, observed, "constraint failed"))
		}
	}
	return findings
}

func (m Matcher) matches(observed Value) bool {
	switch m.Op {
	case "contains":
		return strings.Contains(observed.String(), m.Want)
	case "contains_values":
		return containsObservedValues(observed.Strings(), m.Values, m.Mode)
	case "exact_values":
		return exactObservedValues(observed.Strings(), m.Values)
	case "cidr":
		return cidrMatch(observed.Strings(), m.Values, m.Mode)
	case "==", "!=":
		eq := valuesEqual(observed, m.Want)
		if m.Op == "!=" {
			return !eq
		}
		return eq
	case ">", ">=", "<", "<=":
		got, gotOK := observed.Float()
		want, err := strconv.ParseFloat(m.Want, 64)
		if !gotOK || err != nil {
			return false
		}
		switch m.Op {
		case ">":
			return got > want
		case ">=":
			return got >= want
		case "<":
			return got < want
		case "<=":
			return got <= want
		}
	}
	return false
}

func valuesEqual(observed Value, want string) bool {
	if got, ok := observed.Bool(); ok {
		wantBool, err := strconv.ParseBool(strings.ToLower(want))
		return err == nil && got == wantBool
	}
	if got, ok := observed.Float(); ok {
		wantFloat, err := strconv.ParseFloat(want, 64)
		return err == nil && math.Abs(got-wantFloat) < 0.000001
	}
	return observed.String() == want
}

func containsObservedValues(observed []string, wants []string, mode string) bool {
	if len(wants) == 0 {
		return false
	}
	observedSet := make(map[string]struct{}, len(observed))
	for _, value := range observed {
		observedSet[strings.TrimSpace(value)] = struct{}{}
	}
	matched := 0
	for _, want := range wants {
		if _, ok := observedSet[want]; ok {
			matched++
		}
	}
	if mode == "exact" {
		return matched == len(wants)
	}
	return matched > 0
}

func exactObservedValues(observed []string, wants []string) bool {
	observed = cleanStringValues(observed)
	wants = cleanStringValues(wants)
	if len(observed) != len(wants) {
		return false
	}
	observedCounts := make(map[string]int, len(observed))
	for _, value := range observed {
		observedCounts[value]++
	}
	for _, want := range wants {
		count := observedCounts[want]
		if count == 0 {
			return false
		}
		observedCounts[want] = count - 1
	}
	return true
}

func cidrMatch(observed []string, cidrs []string, mode string) bool {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return false
		}
		prefixes = append(prefixes, prefix)
	}
	if len(prefixes) == 0 {
		return false
	}
	matched := 0
	parsed := 0
	for _, value := range observed {
		addr, ok := parseIPLiteral(value)
		if !ok {
			continue
		}
		parsed++
		for _, prefix := range prefixes {
			if prefix.Contains(addr) {
				matched++
				break
			}
		}
	}
	if mode == "exact" {
		return parsed > 0 && parsed == matched
	}
	return matched > 0
}

func finding(target Target, check Check, matcher Matcher, observed Value, message string) Finding {
	expected := matcher.Op
	if matcher.Want != "" {
		expected += " " + matcher.Want
	} else if len(matcher.Values) > 0 {
		expected += " " + strings.Join(matcher.Values, ",")
		if matcher.Mode != "" {
			expected += " mode=" + matcher.Mode
		}
	}
	return Finding{
		Target:   target.DisplayName(),
		Check:    check.DisplayName(),
		Metric:   matcher.Metric,
		Observed: observed.String(),
		Expected: expected,
		Message:  message,
	}
}
