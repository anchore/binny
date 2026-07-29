package option

import (
	"time"

	"github.com/anchore/clio"
)

type Cooldown struct {
	IgnoreCooldown bool `json:"ignore-cooldown" yaml:"ignore-cooldown" mapstructure:"ignore-cooldown"`
}

func (o *Cooldown) AddFlags(flags clio.FlagSet) {
	flags.BoolVarP(&o.IgnoreCooldown, "ignore-cooldown", "", "Bypass the release cooldown check for all tools")
}

// resolveEffectiveCooldown returns the cooldown duration that should apply for a tool.
// Per-tool cooldown overrides the global cooldown. An unset value means "inherit".
// When ignoreCooldown is true, all cooldowns are bypassed (returns 0).
func resolveEffectiveCooldown(ignoreCooldown bool, global, perTool JSONDuration) time.Duration {
	if ignoreCooldown {
		return 0
	}
	if perTool.IsSet {
		return perTool.Duration
	}
	if global.IsSet {
		return global.Duration
	}
	return 0
}
