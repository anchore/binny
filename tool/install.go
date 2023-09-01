package tool

import (
	"errors"
	"fmt"
	"os"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal/log"
)

func Install(tool binny.Tool, intent binny.VersionIntent, store *binny.Store) error {
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

	resolvedVersion, err := tool.ResolveVersion(intent.Want, intent.Constraint)
	if err != nil {
		return fmt.Errorf("failed to resolve version for tool %q: %w", tool.Name(), err)
	}

	err = Check(tool.Name(), resolvedVersion, store)
	if errors.Is(err, ErrMultipleInstallations) {
		return err
	}
	if err == nil {
		log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("already installed")
		return nil
	}

	log.WithFields("tool", tool.Name(), "version", resolvedVersion, "reason", err).Debug("tool check failed")
	log.WithFields("tool", tool.Name(), "version", resolvedVersion).Info("installing")

	// install the tool to a temp dir
	binPath, err := tool.InstallTo(resolvedVersion, tmpdir)
	if err != nil {
		return err
	}

	// if the installation was successful, add the tool to the store
	return store.AddTool(tool.Name(), resolvedVersion, binPath)
}
