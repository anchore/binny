package binny

type Tool interface {
	Name() string
	Installer
	VersionResolver
}

type Installer interface {
	InstallTo(version, destDir string) (string, error)
}

type VersionResolver interface {
	ResolveVersion(want, constraint string) (string, error)
	UpdateVersion(want, constraint string) (string, error)
}

type VersionIntent struct {
	Want       string
	Constraint string
}
