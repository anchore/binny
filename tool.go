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

// Fingerprinter is optionally implemented by a Tool to identify its install method and parameters, so cache entries
// of the same name and version built from a different config are kept apart.
type Fingerprinter interface {
	Fingerprint() string
}

// CacheBypasser is optionally implemented by a Tool that installs into the store even when the cache is enabled, such
// as one built from a local module, whose every commit would otherwise add a cache entry nothing else reuses.
type CacheBypasser interface {
	BypassesCache() bool
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
