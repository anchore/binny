package tool

import (
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

func Install(tool binny.Tool, intent binny.VersionIntent, store *binny.Store, verifyConfig VerifyConfig) (err error) {
	prog, stage := trackInstallation(tool.Name(), intent.Want)
	defer func() {
		if err != nil && !errors.Is(err, ErrAlreadyInstalled) {
			prog.Set(0)
			prog.SetError(err)
		} else {
			prog.SetCompleted()
		}
	}()
	tmpdir, cleanup, err := makeStagingArea(store, tool.Name())
	defer cleanup()
	if err != nil {
		return err
	}

	log.WithFields("tool", tool.Name(), "dir", tmpdir).Trace("tool staging directory")

	stage.Set("resolving version")

	resolvedVersion, err := tool.ResolveVersion(intent.Want, intent.Constraint)
	if err != nil {
		return fmt.Errorf("failed to resolve version for tool %q: %w", tool.Name(), err)
	}

	stage.Set("validating")

	err = Check(store, tool.Name(), resolvedVersion, verifyConfig)
	if errors.Is(err, binny.ErrMultipleInstallations) {
		return err
	}
	if err == nil {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion).Debug("already installed")
		return ErrAlreadyInstalled
	}

	log.WithFields("tool", tool.Name(), "version", resolvedVersion, "reason", err).Debug("tool check failed")
	log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("installing")

	stage.Set(fmt.Sprintf("installing %q", resolvedVersion))

	// install the tool to a temp dir
	binPath, err := tool.InstallTo(resolvedVersion, tmpdir)
	if err != nil {
		return err
	}

	stage.Set("storing")

	// if the installation was successful, add the tool to the store
	if err = store.AddTool(tool.Name(), resolvedVersion, binPath); err != nil {
		return err
	}

	stage.Set("")

	return nil
}

// makeStagingArea creates a temporary directory within the store root to stage the installation of a tool. A valid
// function should always be returned for cleanup, even if an error is returned.
func makeStagingArea(store *binny.Store, toolName string) (string, func(), error) {
	cleanup := func() {}
	// note: we choose a staging directory that is within the store to ensure the final move is atomic
	// and not across filesystems (which would not succeed). We do this instead of a copy in case one of the binaries
	// being managed is in use, in which case a copy would fail with "text file busy" error, whereas a move would
	// allow for the in-use binary to continue to be used (since an unlink is performed on the path to the binary
	// or the path to the parent of the binary). We use the absolute path to the store root to ensure that there is
	// no confusion on how to interpret this path across different installer implementations.
	absRoot, err := filepath.Abs(store.Root())
	if err != nil {
		return "", cleanup, fmt.Errorf("failed to get absolute path for store root: %w", err)
	}

	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		if err := os.MkdirAll(absRoot, 0755); err != nil {
			return "", cleanup, fmt.Errorf("failed to create store root: %w", err)
		}
	}

	tmpdir, err := os.MkdirTemp(absRoot, fmt.Sprintf("binny-install-%s-", toolName))
	if err != nil {
		return "", cleanup, fmt.Errorf("failed to create temp directory: %w", err)
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
