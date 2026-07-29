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
