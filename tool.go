package binny

import (
	"context"
	"time"
)

type Tool interface {
	Name() string
	Installer
	VersionResolver
}

type Installer interface {
	InstallTo(ctx context.Context, version, destDir string) (string, error)
}

// ArchiveFileKeeper is optionally implemented by an Installer whose binary needs the rest of the
// installed files (e.g. an extracted archive) kept beside it.
type ArchiveFileKeeper interface {
	KeepArchiveFiles() bool
}

type VersionResolver interface {
	ResolveVersion(ctx context.Context, intent VersionIntent) (string, error)
	UpdateVersion(ctx context.Context, intent VersionIntent) (string, error)
}

type VersionIntent struct {
	Want       string
	Constraint string
	Cooldown   time.Duration
}
