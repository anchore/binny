package gobuild

import (
	"context"
	"fmt"
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

	// derive binary name from module/entrypoint
	binName := deriveBinaryName(i.config.Module, i.config.Entrypoint)
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(destDir, binName)

	// determine if this is a local module
	isLocal := IsLocalModule(i.config.Module)

	var workDir string
	var cleanup func()

	if isLocal {
		lgr.WithFields("module", i.config.Module, "version", version).Debug("building go module from local source")
		// for local modules, use the module path directly as the working directory
		workDir = i.config.Module
		cleanup = func() {} // no cleanup needed for local modules
	} else {
		lgr.WithFields("module", i.config.Module, "version", version, "source", string(i.config.Source)).Debug("building go module from source")
		// get source code for remote modules
		var err error
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
	return strings.HasPrefix(module, ".") || strings.HasPrefix(module, "/")
}

// deriveBinaryName extracts the binary name from the module path or entrypoint.
// If entrypoint is provided, uses the last segment of the entrypoint path.
// Otherwise, uses the last segment of the module path (stripping /v2, /v3, etc.).
func deriveBinaryName(module, entrypoint string) string {
	if entrypoint != "" {
		// use the last path segment of the entrypoint
		fields := strings.Split(entrypoint, "/")
		return fields[len(fields)-1]
	}

	// use the last segment of the module, but strip /vN suffix first
	module = stripVersionSuffix(module)
	fields := strings.Split(module, "/")
	return fields[len(fields)-1]
}

// stripVersionSuffix removes /v2, /v3, etc. from module paths.
func stripVersionSuffix(module string) string {
	fields := strings.Split(module, "/")
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		if len(last) >= 2 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9' {
			return strings.Join(fields[:len(fields)-1], "/")
		}
	}
	return module
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
