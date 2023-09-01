package goinstall

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal/log"
)

var _ binny.Installer = (*Installer)(nil)

type InstallerParameters struct {
	Module     string   `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string   `json:"entrypoint" yaml:"entrypoint" mapstructure:"entrypoint"`
	LDFlags    []string `json:"ldflags" yaml:"ldflags" mapstructure:"ldflags"`
}

type Installer struct {
	config          InstallerParameters
	goInstallRunner func(spec, ldflags, destDir string) error
}

func NewInstaller(cfg InstallerParameters) Installer {
	return Installer{
		config:          cfg,
		goInstallRunner: runGoInstall,
	}
}

func (i Installer) InstallTo(version, destDir string) (string, error) {
	path := i.config.Module
	if i.config.Entrypoint != "" {
		path += "/" + i.config.Entrypoint
	}
	fields := strings.Split(path, "/")
	binName := fields[len(fields)-1]
	binPath := filepath.Join(destDir, binName)

	log.WithFields("module", i.config.Module, "version", version).Debug("installing go module")

	spec := fmt.Sprintf("%s@%s", path, version)

	ldflags, err := templateFlags(i.config.LDFlags, version)
	if err != nil {
		return "", fmt.Errorf("failed to template ldflags: %v", err)
	}

	if err := i.goInstallRunner(spec, ldflags, destDir); err != nil {
		return "", fmt.Errorf("failed to install: %v", err)
	}

	return binPath, nil
}

func templateFlags(ldFlags []string, version string) (string, error) {
	flags := strings.Join(ldFlags, " ")

	tmpl, err := template.New("ldflags").Parse(flags)
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	err = tmpl.Execute(&buf, map[string]string{
		"Version": version,
	})

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func runGoInstall(spec, ldflags, destDir string) error {
	args := []string{"install"}
	if ldflags != "" {
		args = append(args, fmt.Sprintf("-ldflags=%s", ldflags))
	}
	args = append(args, spec)

	log.Trace("running: go " + strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOBIN="+destDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %v\nOutput: %s", err, output)
	}
	return nil
}
