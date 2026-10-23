package internal

import (
	"fmt"
	"strings"
)

// ValidateEnvSlice validates that all env vars in the slice have the format "KEY=VALUE".
func ValidateEnvSlice(env []string) error {
	for _, e := range env {
		if !strings.Contains(e, "=") {
			return fmt.Errorf("invalid env format: %q", e)
		}
	}
	return nil
}

// StaticEnv returns the "KEY=VALUE" entries that aren't templated, for running a command before the values a
// template would render (e.g. the version) are known.
func StaticEnv(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.Contains(e, "=") && !strings.Contains(e, "{{") {
			out = append(out, e)
		}
	}
	return out
}
