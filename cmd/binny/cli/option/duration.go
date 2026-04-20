package option

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// JSONDuration is a time.Duration that supports YAML/JSON unmarshaling from human-friendly
// duration strings like "7d" (days) in addition to standard Go durations like "168h".
// The IsSet field tracks whether the value was explicitly configured (distinguishing
// "not configured" from "explicitly set to zero").
type JSONDuration struct {
	Duration time.Duration
	IsSet    bool
}

func (d *JSONDuration) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" || s == "0" {
		d.Duration = 0
		d.IsSet = true
		return nil
	}

	// support "Nd" shorthand for days
	if before, ok := strings.CutSuffix(s, "d"); ok {
		prefix := before
		days, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", string(text), err)
		}
		if days < 0 {
			return fmt.Errorf("invalid duration %q: must be non-negative", string(text))
		}
		d.Duration = time.Duration(days) * 24 * time.Hour
		d.IsSet = true
		return nil
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	if parsed < 0 {
		return fmt.Errorf("invalid duration %q: must be non-negative", string(text))
	}
	d.Duration = parsed
	d.IsSet = true
	return nil
}

func (d JSONDuration) MarshalText() ([]byte, error) {
	if d.Duration == 0 {
		return []byte("0"), nil
	}
	// prefer days representation when it's an exact multiple
	if d.Duration%(24*time.Hour) == 0 {
		days := int(d.Duration / (24 * time.Hour))
		return []byte(fmt.Sprintf("%dd", days)), nil
	}
	return []byte(d.Duration.String()), nil
}

func (d *JSONDuration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(s))
}

// ParseFrom parses a JSONDuration from various input types that may come from
// config loading (string, int, float64, etc). This is needed because mapstructure
// doesn't support TextUnmarshaler interface, so we need to handle the raw config
// value in PostLoad.
func (d *JSONDuration) ParseFrom(v any) error {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		return d.UnmarshalText([]byte(val))
	case int:
		if val < 0 {
			return fmt.Errorf("invalid duration %d: must be non-negative", val)
		}
		d.Duration = time.Duration(val)
		d.IsSet = true
		return nil
	case int64:
		if val < 0 {
			return fmt.Errorf("invalid duration %d: must be non-negative", val)
		}
		d.Duration = time.Duration(val)
		d.IsSet = true
		return nil
	case float64:
		if val < 0 {
			return fmt.Errorf("invalid duration %v: must be non-negative", val)
		}
		d.Duration = time.Duration(val)
		d.IsSet = true
		return nil
	case JSONDuration:
		*d = val
		return nil
	case *JSONDuration:
		if val != nil {
			*d = *val
		}
		return nil
	default:
		return fmt.Errorf("cannot parse duration from type %T", v)
	}
}
