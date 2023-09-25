package tool

import (
	"errors"
	"fmt"
	"os"
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

	tmpdir, err := os.MkdirTemp("", fmt.Sprintf("binny-install-%s-", tool.Name()))
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		err := os.RemoveAll(tmpdir)
		if err != nil {
			log.WithFields("tool", tool.Name(), "dir", tmpdir).
				Warnf("failed to remove temp directory: %v", err)
		}
	}()

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
