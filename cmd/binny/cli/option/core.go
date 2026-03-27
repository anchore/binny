package option

// Core options make up the static application configuration on disk.
type Core struct {
	Store    `json:"" yaml:",inline" mapstructure:",squash"`
	Cooldown JSONDuration `json:"cooldown" yaml:"cooldown,omitempty" mapstructure:"cooldown"`
	Tools    Tools        `json:"tools" yaml:"tools" mapstructure:"tools"`
}

func DefaultCore() Core {
	return Core{
		Store: DefaultStore(),
	}
}
