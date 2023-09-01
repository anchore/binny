package option

type Store struct {
	Root string `json:"root" yaml:"root" mapstructure:"root"`
}

func DefaultStore() Store {
	return Store{
		Root: ".tool",
	}
}
