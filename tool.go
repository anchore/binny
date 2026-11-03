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
// of the same name and version built from a different config are kept apart. The fingerprint must change whenever the
// installed bytes or layout would (platform and keep-archive-files included), and be a file name without separators.
// It is empty when the name and resolved version do not pin the bytes, such as for a tool built from a local module.
// A tool with no fingerprint is installed into the store even when the cache is enabled.
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
