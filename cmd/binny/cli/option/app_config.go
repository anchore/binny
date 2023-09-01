package option

type AppConfig struct {
	Store `json:"" yaml:",inline" mapstructure:",squash"`
	Tools Tools `json:"tools" yaml:"tools" mapstructure:"tools"`
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Store: DefaultStore(),
	}
}
