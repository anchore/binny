package goinstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/gobuild"
)

var _ binny.Installer = (*Installer)(nil)

type InstallerParameters struct {
	Module     string   `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string   `json:"entrypoint" yaml:"entrypoint" mapstructure:"entrypoint"`
	LDFlags    []string `json:"ldflags" yaml:"ldflags" mapstructure:"ldflags"`
	Args       []string `json:"args" yaml:"args" mapstructure:"args"`
	Env        []string `json:"env" yaml:"env" mapstructure:"env"`
}

type Installer struct {
	config          InstallerParameters
	goInstallRunner func(ctx context.Context, spec, ldflags string, args []string, env []string, destDir string) error
}

func NewInstaller(cfg InstallerParameters) Installer {
	return Installer{
		config:          cfg,
		goInstallRunner: runGoInstall,
	}
}

func (i Installer) InstallTo(ctx context.Context, version, destDir string) (string, error) {
	// go finds the main module from the working directory, so a local module has to be built from inside
	// its directory, which is what go-build does: hand it off
	if gobuild.IsLocalModule(i.config.Module) {
		return gobuild.NewInstaller(gobuild.InstallerParameters{
			Module:     i.config.Module,
			Entrypoint: i.config.Entrypoint,
			LDFlags:    i.config.LDFlags,
			Args:       i.config.Args,
			Env:        i.config.Env,
		}).InstallTo(ctx, version, destDir)
	}

	ctx, lgr := log.WithNested(ctx, "tool", fmt.Sprintf("%s@%s", i.config.Module, version))

	importPath := path.Join(i.config.Module, i.config.Entrypoint)
	// path.Join also folds away "..", which would let an entrypoint name a different module
	if module := path.Clean(i.config.Module); importPath != module && !strings.HasPrefix(importPath, module+"/") {
		return "", fmt.Errorf("entrypoint %q leaves module %q (resolves to %q)", i.config.Entrypoint, i.config.Module, importPath)
	}
	binName, err := binaryName(importPath)
	if err != nil {
		return "", err
	}
	binPath := filepath.Join(destDir, binName)

	spec := fmt.Sprintf("%s@%s", importPath, version)
	lgr.WithFields("module", i.config.Module, "version", version).Debug("installing go module")

	ldflags, err := internal.TemplateFlags(i.config.LDFlags, version)
	if err != nil {
		return "", fmt.Errorf("failed to template ldflags: %v", err)
	}

	args, err := internal.TemplateSlice(i.config.Args, version)
	if err != nil {
		return "", fmt.Errorf("failed to template args: %v", err)
	}

	if err := internal.ValidateEnvSlice(i.config.Env); err != nil {
		return "", err
	}

	env, err := internal.TemplateSlice(i.config.Env, version)
	if err != nil {
		return "", fmt.Errorf("failed to template env: %v", err)
	}

	if err := i.goInstallRunner(ctx, spec, ldflags, args, env, destDir); err != nil {
		return "", fmt.Errorf("failed to install: %v", err)
	}

	return binPath, nil
}

// binaryName predicts the file go install writes to GOBIN for the given import path, mirroring
// cmd/go's DefaultExecName: the last element, unless it is a major version suffix (/v2, /v3, ...) in
// which case the element before it. It errors rather than return a name that is empty or a path.
func binaryName(importPath string) (string, error) {
	dir, name := path.Split(importPath)
	if dir != "" && isMajorVersionElement(name) {
		name = path.Base(dir)
	}
	// a backslash is a separator on windows, so filepath.Join(destDir, name) would treat it as a path
	if name == "" || name == "." || name == ".." || strings.Contains(name, `\`) {
		return "", fmt.Errorf("unable to derive a binary name from %q", importPath)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

// isMajorVersionElement reports whether s is a /vN module suffix: v followed only by digits, N >= 2
// (cmd/go's isVersionElement). "v1", "v0", "v2ray" and "v8go" are not.
func isMajorVersionElement(s string) bool {
	if len(s) < 2 || s[0] != 'v' || s[1] == '0' || s[1] == '1' && len(s) == 2 {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func runGoInstall(ctx context.Context, spec, ldflags string, userArgs, userEnv []string, destDir string) error {
	args := []string{"install"}
	args = append(args, userArgs...)

	if ldflags != "" {
		args = append(args, fmt.Sprintf("-ldflags=%s", ldflags))
	}
	args = append(args, spec)

	log.WithFields("env-vars", len(userEnv)).Trace("running: go " + strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "go", args...)

	// set env vars...
	env := os.Environ()
	env = append(env, userEnv...)
	// always override any conflicting env vars
	env = append(env, "GOBIN="+destDir)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %v\nOutput: %s", err, output)
	}
	return nil
}
