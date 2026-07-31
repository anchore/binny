package goinstall

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

type InstallerParameters struct {
	Module     string   `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string   `json:"entrypoint" yaml:"entrypoint" mapstructure:"entrypoint"`
	LDFlags    []string `json:"ldflags" yaml:"ldflags" mapstructure:"ldflags"`
	Args       []string `json:"args" yaml:"args" mapstructure:"args"`
	Env        []string `json:"env" yaml:"env" mapstructure:"env"`
}

type Installer struct {
	config          InstallerParameters
	goInstallRunner func(spec, ldflags string, args []string, env []string, destDir string, isLocal bool, binName string) error
}

func NewInstaller(cfg InstallerParameters) Installer {
	return Installer{
		config:          cfg,
		goInstallRunner: runGoInstall,
	}
}

func (i Installer) InstallTo(ctx context.Context, version, destDir string) (string, error) {
	ctx, lgr := log.WithNested(ctx, "tool", fmt.Sprintf("%s@%s", i.config.Module, version)) //nolint: ineffassign,staticcheck // we want any future calls we add to always have the right context

	path := i.config.Module
	if i.config.Entrypoint != "" {
		path += "/" + i.config.Entrypoint
	}
	fields := strings.Split(path, "/")
	binName := fields[len(fields)-1]
	binPath := filepath.Join(destDir, binName)
	// TODO: probably refactor this somewhere
	if runtime.GOOS == "windows" {
		binPath = fmt.Sprintf("%s.exe", binPath)
	}

	spec := fmt.Sprintf("%s@%s", path, version)
	isLocal := strings.HasPrefix(i.config.Module, ".") || strings.HasPrefix(i.config.Module, "/")
	if isLocal {
		spec = path
		lgr.WithFields("module", i.config.Module, "version", version).Debug("installing go module (local)")
	} else {
		lgr.WithFields("module", i.config.Module, "version", version).Debug("installing go module (remote)")
	}

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

	if err := i.goInstallRunner(spec, ldflags, args, env, destDir, isLocal, binName); err != nil {
		return "", fmt.Errorf("failed to install: %v", err)
	}

	return binPath, nil
}

func runGoInstall(spec, ldflags string, userArgs, userEnv []string, destDir string, isLocal bool, binName string) error {
	var args []string
	if isLocal {
		// go install in module mode for a local module is not a good idea, so use go build in this case since the source is already local
		args = append(args, "build", "-o", filepath.Join(destDir, binName))
	} else {
		args = append(args, "install")
	}
	args = append(args, userArgs...)

	if ldflags != "" {
		args = append(args, fmt.Sprintf("-ldflags=%s", ldflags))
	}
	args = append(args, spec)

	log.WithFields("env-vars", len(userEnv)).Trace("running: go " + strings.Join(args, " "))

	cmd := exec.Command("go", args...)

	// set env vars...
	env := os.Environ()
	env = append(env, userEnv...)
	// always override any conflicting env vars
	if !isLocal {
		env = append(env, "GOBIN="+destDir)
	}
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %v\nOutput: %s", err, output)
	}
	return nil
}
