package watch

import (
	"fmt"
	"regexp"
	"strings"
)

var targetTemplatePattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func resolveCheckForTarget(check Check, target Target) (Check, error) {
	resolved := check
	values := targetTemplateValues(target)
	stringFields := []*string{
		&resolved.Name,
		&resolved.Host,
		&resolved.Query,
		&resolved.Record,
		&resolved.URL,
		&resolved.Family,
		&resolved.ScanTarget,
		&resolved.Band,
	}
	for _, field := range stringFields {
		value, err := resolveTargetTemplate(*field, values)
		if err != nil {
			return Check{}, err
		}
		*field = value
	}
	if check.Expect == nil {
		if len(check.compiledExpect) > 0 {
			resolved.compiledExpect = append([]Matcher(nil), check.compiledExpect...)
		} else {
			resolved.compiledExpect = nil
		}
		resolved.Expect = nil
		return resolved, nil
	}
	rawExpect, err := resolveTargetTemplateValue(check.Expect, values)
	if err != nil {
		return Check{}, err
	}
	if rawExpect == nil {
		resolved.Expect = nil
	} else {
		expect, ok := rawExpect.(map[string]any)
		if !ok {
			return Check{}, fmt.Errorf("resolved expect is %T, want map[string]any", rawExpect)
		}
		resolved.Expect = expect
	}
	matchers, err := compileMatchers(resolved.Expect)
	if err != nil {
		return Check{}, err
	}
	resolved.compiledExpect = matchers
	return resolved, nil
}

func resolveTargetVars(target Target) (Target, error) {
	if len(target.Vars) == 0 {
		return target, nil
	}
	source := make(map[string]string, len(target.Vars))
	for key, value := range target.Vars {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		source[key] = strings.TrimSpace(value)
	}
	if len(source) == 0 {
		target.Vars = nil
		return target, nil
	}
	builtins := targetBuiltinValues(target)
	resolved := make(map[string]string, len(source))
	resolving := make(map[string]bool, len(source))
	var resolve func(string) (string, error)
	resolve = func(key string) (string, error) {
		if value, ok := resolved[key]; ok {
			return value, nil
		}
		raw, ok := source[key]
		if !ok {
			if value, ok := builtins[key]; ok {
				return value, nil
			}
			return "", fmt.Errorf("unknown target variable %q", key)
		}
		if resolving[key] {
			return "", fmt.Errorf("target variable cycle at %q", key)
		}
		resolving[key] = true
		value, err := resolveTemplateString(raw, func(ref string) (string, error) {
			return resolve(strings.TrimSpace(ref))
		})
		delete(resolving, key)
		if err != nil {
			return "", err
		}
		resolved[key] = value
		return value, nil
	}
	for key := range source {
		value, err := resolve(key)
		if err != nil {
			return Target{}, err
		}
		resolved[key] = value
	}
	target.Vars = resolved
	return target, nil
}

func targetTemplateValues(target Target) map[string]string {
	values := targetBuiltinValues(target)
	for key, value := range target.Vars {
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func targetBuiltinValues(target Target) map[string]string {
	values := map[string]string{
		"name":       strings.TrimSpace(target.Name),
		"short_name": strings.TrimSpace(target.ShortName),
		"agent":      strings.TrimSpace(target.Agent),
		"ssid":       strings.TrimSpace(target.SSID),
		"bssid":      strings.TrimSpace(target.BSSID),
		"band":       strings.TrimSpace(target.Band),
	}
	return values
}

func resolveTargetTemplateValue(raw any, values map[string]string) (any, error) {
	switch typed := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return resolveTargetTemplate(typed, values)
	case []any:
		resolved := make([]any, 0, len(typed))
		for _, item := range typed {
			value, err := resolveTargetTemplateValue(item, values)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, value)
		}
		return resolved, nil
	case []string:
		resolved := make([]string, 0, len(typed))
		for _, item := range typed {
			value, err := resolveTargetTemplate(item, values)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, value)
		}
		return resolved, nil
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, value := range typed {
			item, err := resolveTargetTemplateValue(value, values)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			resolved[key] = item
		}
		return resolved, nil
	default:
		return raw, nil
	}
}

func resolveTargetTemplate(value string, values map[string]string) (string, error) {
	return resolveTemplateString(value, func(key string) (string, error) {
		replacement, ok := values[key]
		if !ok {
			return "", fmt.Errorf("unknown target variable %q", key)
		}
		return replacement, nil
	})
}

func resolveTemplateString(value string, resolver func(string) (string, error)) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}
	var resolveErr error
	resolved := targetTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
		if resolveErr != nil {
			return ""
		}
		groups := targetTemplatePattern.FindStringSubmatch(match)
		if len(groups) != 2 {
			resolveErr = fmt.Errorf("invalid target variable syntax %q", match)
			return ""
		}
		key := strings.TrimSpace(groups[1])
		replacement, err := resolver(key)
		if err != nil {
			resolveErr = err
			return ""
		}
		return replacement
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return resolved, nil
}
