package gobuild

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

var _ binny.Installer = (*Installer)(nil)

// stagingBinName is the file go build writes into destDir.
const stagingBinName = "go-build-output"

// SourceMode indicates how to obtain the source code for building.
type SourceMode string

const (
	// SourceModeGit clones the repository using git.
	SourceModeGit SourceMode = "git"
	// SourceModeGoProxy downloads source via the go proxy.
	SourceModeGoProxy SourceMode = "go-proxy"
)

// InstallerParameters contains the configuration for building a Go module from source.
type InstallerParameters struct {
	Module     string     `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string     `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty" mapstructure:"entrypoint"`
	LDFlags    []string   `json:"ldflags,omitempty" yaml:"ldflags,omitempty" mapstructure:"ldflags"`
	Args       []string   `json:"args,omitempty" yaml:"args,omitempty" mapstructure:"args"`
	Env        []string   `json:"env,omitempty" yaml:"env,omitempty" mapstructure:"env"`
	Source     SourceMode `json:"source,omitempty" yaml:"source,omitempty" mapstructure:"source"`
	RepoURL    string     `json:"repo-url,omitempty" yaml:"repo-url,omitempty" mapstructure:"repo-url"`
}

// Installer builds Go binaries from source code obtained via git or go proxy.
type Installer struct {
	config        InstallerParameters
	goBuildRunner func(ctx context.Context, workDir, outputPath, entrypoint, ldflags string, args, env []string) error
	sourceGetter  func(ctx context.Context, module, version, repoURL string, mode SourceMode) (workDir string, cleanup func(), err error)
}

// NewInstaller creates a new go-build installer with the given configuration.
func NewInstaller(cfg InstallerParameters) Installer {
	// default to git source mode
	if cfg.Source == "" {
		cfg.Source = SourceModeGit
	}
	return Installer{
		config:        cfg,
		goBuildRunner: runGoBuild,
		sourceGetter:  getSource,
	}
}

// InstallTo builds the Go module and places the resulting binary in destDir.
func (i Installer) InstallTo(ctx context.Context, version, destDir string) (string, error) {
	ctx, lgr := log.WithNested(ctx, "tool", fmt.Sprintf("%s@%s", i.config.Module, version))

	// we choose the output path and the store renames it to the tool name, so the name only has to be
	// stable within destDir (not derived from config)
	binName := stagingBinName
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(destDir, binName)

	// determine if this is a local module
	isLocal := IsLocalModule(i.config.Module)

	var workDir string
	var cleanup func()
	var err error

	if isLocal {
		// go resolves the main module from the working directory, so the build runs inside the module
		workDir, err = resolveLocalModule(i.config.Module)
		if err != nil {
			return "", err
		}
		lgr.WithFields("module", i.config.Module, "dir", workDir, "version", version).Debug("building go module from local source")
		cleanup = func() {} // no cleanup needed for local modules
	} else {
		lgr.WithFields("module", i.config.Module, "version", version, "source", string(i.config.Source)).Debug("building go module from source")
		// get source code for remote modules
		workDir, cleanup, err = i.sourceGetter(ctx, i.config.Module, version, i.config.RepoURL, i.config.Source)
		if err != nil {
			return "", fmt.Errorf("failed to get source: %w", err)
		}
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	// template ldflags
	ldflags, err := internal.TemplateFlags(i.config.LDFlags, version)
	if err != nil {
		return "", fmt.Errorf("failed to template ldflags: %w", err)
	}

	// template and validate args
	args, err := internal.TemplateSlice(i.config.Args, version)
	if err != nil {
		return "", fmt.Errorf("failed to template args: %w", err)
	}

	// validate and template env
	if err := internal.ValidateEnvSlice(i.config.Env); err != nil {
		return "", err
	}
	env, err := internal.TemplateSlice(i.config.Env, version)
	if err != nil {
		return "", fmt.Errorf("failed to template env: %w", err)
	}

	// run go build
	if err := i.goBuildRunner(ctx, workDir, binPath, i.config.Entrypoint, ldflags, args, env); err != nil {
		return "", fmt.Errorf("failed to build: %w", err)
	}

	return binPath, nil
}

// IsLocalModule returns true if the module path refers to a local filesystem path.
func IsLocalModule(module string) bool {
	// filepath.IsAbs covers windows paths like C:\foo, the "/" prefix keeps rooted paths local everywhere
	return strings.HasPrefix(module, ".") || strings.HasPrefix(module, "/") || filepath.IsAbs(module)
}

// resolveLocalModule returns the absolute directory of a local module. Relative paths are taken
// from the working directory, the same as every other path in the binny config.
func resolveLocalModule(module string) (string, error) {
	dir, err := filepath.Abs(module)
	if err != nil {
		return "", fmt.Errorf("unable to resolve local module %q: %w", module, err)
	}
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("local module directory %q does not exist (from module %q)", dir, module)
	case err != nil:
		return "", fmt.Errorf("unable to read local module directory %q (from module %q): %w", dir, module, err)
	case !fi.IsDir():
		return "", fmt.Errorf("local module %q is not a directory (from module %q)", dir, module)
	}
	// no go.mod check here: the module may be a package dir within a module, or a go.work root, and go
	// reports a missing main module better than we can guess at it
	return dir, nil
}

func runGoBuild(ctx context.Context, workDir, outputPath, entrypoint, ldflags string, userArgs, userEnv []string) error {
	args := []string{"build", "-o", outputPath}
	args = append(args, userArgs...)

	if ldflags != "" {
		args = append(args, fmt.Sprintf("-ldflags=%s", ldflags))
	}

	// determine the build target (entrypoint relative to workDir or current directory)
	target := "."
	if entrypoint != "" {
		target = "./" + entrypoint
	}
	args = append(args, target)

	log.WithFields("workDir", workDir, "env-vars", len(userEnv)).Trace("running: go " + strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = workDir

	// set env vars
	env := os.Environ()
	env = append(env, userEnv...)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %v\nOutput: %s", err, output)
	}
	return nil
}
