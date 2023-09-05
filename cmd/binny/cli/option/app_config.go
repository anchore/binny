package option

type AppConfig struct {
	Store `json:"" yaml:",inline" mapstructure:",squash"`
	Tools Tools `json:"tools" yaml:"tools" mapstructure:"tools"`
	Check `json:"" yaml:",inline" mapstructure:",squash"`
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Store: DefaultStore(),
	}
}
