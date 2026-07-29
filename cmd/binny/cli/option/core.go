package option

import "fmt"

// Core options make up the static application configuration on disk.
type Core struct {
	Store `json:"" yaml:",inline" mapstructure:",squash"`
	// CooldownRaw is the raw config value for the global cooldown duration.
	// Use Cooldown field after PostLoad has been called.
	CooldownRaw any          `json:"cooldown" yaml:"cooldown,omitempty" mapstructure:"cooldown"`
	Cooldown    JSONDuration `json:"-" yaml:"-" mapstructure:"-"`
	Tools       Tools        `json:"tools" yaml:"tools" mapstructure:"tools"`
}

func DefaultCore() Core {
	return Core{
		Store: DefaultStore(),
	}
}

// PostLoad is called by fangs after config loading to parse raw config values.
func (c *Core) PostLoad() error {
	if err := c.Cooldown.ParseFrom(c.CooldownRaw); err != nil {
		return fmt.Errorf("invalid cooldown value: %w", err)
	}
	return nil
}
