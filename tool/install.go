package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny"
	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
)

var ErrAlreadyInstalled = errors.New("already installed")

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

	if fingerprint, ok := cacheFingerprint(cache, tool); ok {
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

	if entry == nil {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("installing")

		tmpdir, cleanup, err := makeStagingArea(cache.StagingRoot(), tool.Name())
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

		entry, err = cache.Add(tool.Name(), resolvedVersion, fingerprint, tmpdir, binPath, keepsArchiveFiles(tool))
		if err != nil {
			return err
		}
	} else {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion, "entry", entry.Dir).Debug("using cached install")
	}

	stage.Set("linking")

	if err := store.LinkCacheEntry(*entry); err != nil {
		return err
	}

	stage.Set("")

	return nil
}

// cacheFingerprint returns the tool's fingerprint when it should install through the cache.
func cacheFingerprint(cache *binny.Cache, t binny.Tool) (string, bool) {
	if cache == nil {
		return "", false
	}
	f, ok := t.(binny.Fingerprinter)
	if !ok {
		return "", false
	}
	return f.Fingerprint(), true
}

// makeStagingArea creates a temporary directory within the given root (the store root, or the cache's staging root)
// to stage the installation of a tool. A valid function should always be returned for cleanup, even if an error is
// returned.
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
