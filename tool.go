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

// Fingerprinter is optionally implemented by a Tool to identify its install method and parameters, and anything else
// beyond the name and resolved version that changes the installed bytes (like the go toolchain), so cache entries built
// from a different config are kept apart. Any string works: binny hashes it together with the platform and
// keep-archive-files into the cache key. It is empty when the name and resolved version do not pin the bytes, such as
// for a tool built from a local module. A tool with no fingerprint is installed into the store even when the cache is
// enabled, and so is any tool whose resolved version is not exact (latest, a branch, v1.2).
type Fingerprinter interface {
	Fingerprint() string
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
