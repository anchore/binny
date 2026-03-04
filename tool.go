package binny

import "context"

type Tool interface {
	Name() string
	Installer
	VersionResolver
}

type Installer interface {
	InstallTo(ctx context.Context, version, destDir string) (string, error)
}

type VersionResolver interface {
	ResolveVersion(ctx context.Context, want, constraint string) (string, error)
	UpdateVersion(ctx context.Context, want, constraint string) (string, error)
}

type VersionIntent struct {
	Want       string
	Constraint string
}
