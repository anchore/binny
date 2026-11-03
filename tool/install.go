package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny"
	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
)

var ErrAlreadyInstalled = errors.New("already installed")

// Install installs the tool into the store, through the cache when one is given. A nil cache means the cache is disabled.
func Install(ctx context.Context, tool binny.Tool, intent binny.VersionIntent, store *binny.Store, cache *binny.Cache, verifyConfig VerifyConfig) (err error) {
	prog, stage := trackInstallation(tool.Name(), intent.Want)
	defer func() {
		if err != nil && !errors.Is(err, ErrAlreadyInstalled) {
			prog.Set(0)
			prog.SetError(err)
		} else {
			prog.SetCompleted()
		}
	}()
	stage.Set("resolving version")

	resolvedVersion, err := tool.ResolveVersion(ctx, intent)
	if err != nil {
		return fmt.Errorf("failed to resolve version for tool %q: %w", tool.Name(), err)
	}

	stage.Set("validating")

	err = Check(store, cache, tool, resolvedVersion, verifyConfig)
	if errors.Is(err, binny.ErrMultipleInstallations) {
		return err
	}
	if err == nil {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion).Debug("already installed")
		return ErrAlreadyInstalled
	}

	log.WithFields("tool", tool.Name(), "version", resolvedVersion, "reason", err).Debug("tool check failed")

	if fingerprint, ok := cacheFingerprint(cache, tool, resolvedVersion); ok {
		return installThroughCache(ctx, tool, resolvedVersion, fingerprint, store, cache, stage)
	}

	log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("installing")

	tmpdir, cleanup, err := makeStagingArea(store.Root(), tool.Name())
	defer cleanup()
	if err != nil {
		return err
	}

	log.WithFields("tool", tool.Name(), "dir", tmpdir).Trace("tool staging directory")

	stage.Set(fmt.Sprintf("installing %q", resolvedVersion))

	// install the tool to a temp dir
	binPath, err := tool.InstallTo(ctx, resolvedVersion, tmpdir)
	if err != nil {
		return err
	}

	stage.Set("storing")

	// if the installation was successful, add the tool to the store
	if keepsArchiveFiles(tool) {
		err = store.AddToolDir(tool.Name(), resolvedVersion, tmpdir, binPath)
	} else {
		err = store.AddTool(tool.Name(), resolvedVersion, binPath)
	}
	if err != nil {
		return err
	}

	stage.Set("")

	return nil
}

// installThroughCache links the store entry to the tool's cache entry, installing into the cache first only when
// no entry exists yet.
func installThroughCache(ctx context.Context, tool binny.Tool, resolvedVersion, fingerprint string, store *binny.Store, cache *binny.Cache, stage *progress.AtomicStage) error {
	entry, err := cache.Get(tool.Name(), resolvedVersion, fingerprint)
	if err != nil {
		return err
	}

	if entry != nil {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion, "entry", entry.Dir).Debug("using cached install")
		stage.Set("verifying")
		if err := entry.Verify(); err != nil {
			return err
		}
	} else {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("installing")

		tmpdir, cleanup, err := cache.Stage(tool.Name())
		defer cleanup()
		if err != nil {
			return err
		}

		log.WithFields("tool", tool.Name(), "dir", tmpdir).Trace("tool staging directory")

		stage.Set(fmt.Sprintf("installing %q", resolvedVersion))

		binPath, err := tool.InstallTo(ctx, resolvedVersion, tmpdir)
		if err != nil {
			return err
		}

		stage.Set("caching")

		// a fresh entry needs no verify, its digests were just computed from these bytes
		entry, err = cache.Add(tool.Name(), resolvedVersion, fingerprint, tmpdir, binPath, keepsArchiveFiles(tool))
		if err != nil {
			return err
		}
	}

	stage.Set("linking")

	if err := store.LinkCacheEntry(*entry); err != nil {
		return err
	}

	stage.Set("")

	return nil
}

// cacheFingerprint returns the cache key for the tool when it should install through the cache. A resolved version
// that can move (latest, a branch, a go prefix query like v1.2), whichever resolver passed it through, does not pin the
// bytes, so it would be cached once and never rebuilt.
func cacheFingerprint(cache *binny.Cache, t binny.Tool, resolvedVersion string) (string, bool) {
	if cache == nil {
		return "", false
	}
	if !internal.IsExactVersion(resolvedVersion) {
		log.WithFields("tool", t.Name(), "version", resolvedVersion).Debug("version is not exact, the cache is not used for it")
		return "", false
	}
	// a version the resolver may have passed through unresolved is only trusted as plain semver, which the go command
	// reads as a tag and never as a branch
	if p, ok := t.(interface{ passesThroughUnresolved() bool }); ok && p.passesThroughUnresolved() && !internal.IsPlainVersion(resolvedVersion) {
		log.WithFields("tool", t.Name(), "version", resolvedVersion).Debug("version may be an unresolved branch, the cache is not used for it")
		return "", false
	}
	var fingerprint string
	if f, ok := t.(binny.Fingerprinter); ok {
		fingerprint = f.Fingerprint()
	}
	if fingerprint == "" {
		log.WithFields("tool", t.Name()).Debug("tool has no fingerprint, the cache is not used for it")
		return "", false
	}
	return cacheKey(runtime.GOOS, runtime.GOARCH, fingerprint, keepsArchiveFiles(t)), true
}

// cacheKeyVersion is folded into every cache key. Bump it when what the key covers changes on purpose, which re-keys
// every cache entry.
const cacheKeyVersion = 1

// cacheKey is the fingerprint dir of a cache entry: a sha256 over the tool's fingerprint and what decides the bytes and
// layout of every tool, the platform and keep-archive-files. Checkouts share an entry by this key alone, so it has to
// resist a config crafted to collide with another repo's tool, not only keep honest configs apart. 128 bits is plenty
// for that and keeps the path short.
func cacheKey(goos, goarch, fingerprint string, keepArchiveFiles bool) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d %q %q %t %q", cacheKeyVersion, goos, goarch, keepArchiveFiles, fingerprint))
	return hex.EncodeToString(sum[:16])
}

// makeStagingArea creates a temporary directory within the store root to stage the installation of a tool. A valid
// function should always be returned for cleanup, even if an error is returned.
func makeStagingArea(root string, toolName string) (string, func(), error) {
	cleanup := func() {}
	// note: we choose a staging directory that is within the store to ensure the final move is atomic
	// and not across filesystems (which would not succeed). We do this instead of a copy in case one of the binaries
	// being managed is in use, in which case a copy would fail with "text file busy" error, whereas a move would
	// allow for the in-use binary to continue to be used (since an unlink is performed on the path to the binary
	// or the path to the parent of the binary). We use the absolute path to the store root to ensure that there is
	// no confusion on how to interpret this path across different installer implementations.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", cleanup, fmt.Errorf("failed to get absolute path for %q: %w", root, err)
	}

	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		if err := os.MkdirAll(absRoot, 0755); err != nil {
			return "", cleanup, fmt.Errorf("failed to create %q: %w", absRoot, err)
		}
	}

	tmpdir, err := os.MkdirTemp(absRoot, fmt.Sprintf("binny-install-%s-", toolName))
	if err != nil {
		return "", cleanup, fmt.Errorf("failed to create temp directory in %q: %w", absRoot, err)
	}
	cleanup = func() {
		err := os.RemoveAll(tmpdir)
		if err != nil {
			log.WithFields("tool", toolName, "dir", tmpdir).
				Warnf("failed to remove temp directory: %v", err)
		}
	}

	return tmpdir, cleanup, nil
}

func trackInstallation(repo, version string) (*progress.Manual, *progress.AtomicStage) {
	fields := strings.Split(repo, "/")
	p := progress.NewManual(-1)
	p.Set(1) // show that this has started
	s := progress.NewAtomicStage("")
	name := fields[len(fields)-1]

	bus.Publish(partybus.Event{
		Type: event.ToolInstallationStartedEvent,
		Source: toolInfo{
			name:    name,
			version: version,
		},
		Value: struct {
			progress.Stager
			progress.Progressable
		}{
			Stager:       s,
			Progressable: p,
		},
	})

	return p, s
}

var _ event.Tool = (*toolInfo)(nil)

type toolInfo struct {
	name    string
	version string
}

func (t toolInfo) Name() string {
	return t.name
}

func (t toolInfo) Version() string {
	return t.version
}
