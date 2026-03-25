package binny

import (
	"fmt"
	"time"
)

// CooldownError is returned when no version passes the release cooldown check.
type CooldownError struct {
	Cooldown      time.Duration
	LatestVersion string
	LatestDate    *time.Time

	// CheckedCount and TotalCount are set when the resolver hit a candidate limit
	// before checking all available versions.
	CheckedCount int
	TotalCount   int
}

func (e *CooldownError) Error() string {
	suffix := ""
	if e.CheckedCount > 0 && e.CheckedCount < e.TotalCount {
		suffix = fmt.Sprintf(" (only checked %d of %d candidates)", e.CheckedCount, e.TotalCount)
	}

	if e.LatestVersion != "" && e.LatestDate != nil {
		age := time.Since(*e.LatestDate).Truncate(time.Minute)
		remaining := e.Cooldown - age
		if remaining < 0 {
			remaining = 0
		}
		return fmt.Sprintf(
			"version %q was published %s ago, but the release cooldown requires %s (try again in %s, or use --ignore-cooldown to bypass)%s",
			e.LatestVersion, formatDuration(age), formatDuration(e.Cooldown), formatDuration(remaining), suffix,
		)
	}
	return fmt.Sprintf(
		"no version found that satisfies the release cooldown of %s (use --ignore-cooldown to bypass)%s",
		formatDuration(e.Cooldown), suffix,
	)
}

// formatDuration renders a duration in a human-friendly way using days and hours.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	default:
		return d.Truncate(time.Minute).String()
	}
}
