package watch

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration decodes watch YAML durations from Go-style strings, day-suffix strings, or integer milliseconds.
type Duration struct {
	time.Duration
}

// UnmarshalYAML decodes a duration scalar from YAML.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
	default:
		return fmt.Errorf("duration must be a scalar")
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		d.Duration = 0
		return nil
	}
	if node.Tag == "!!int" {
		ms, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		d.Duration = time.Duration(ms) * time.Millisecond
		return nil
	}
	parsed, err := parseDuration(value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func parseDuration(value string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		count, err := strconv.ParseInt(days, 10, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func durationMillis(value Duration) string {
	if value.Duration <= 0 {
		return ""
	}
	return strconv.FormatInt(value.Duration.Milliseconds(), 10)
}
