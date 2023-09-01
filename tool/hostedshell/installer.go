package hostedshell

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/google/shlex"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

var _ binny.Installer = (*Installer)(nil)

type InstallerParameters struct {
	URL  string `json:"url" yaml:"url" mapstructure:"url"`
	Args string `json:"args" yaml:"args" mapstructure:"args"`
}

type Installer struct {
	config       InstallerParameters
	scriptRunner func(scriptPath string, argStr string) error
}

func NewInstaller(cfg InstallerParameters) Installer {
	return Installer{
		config:       cfg,
		scriptRunner: runScript,
	}
}

func (i Installer) InstallTo(version, destDir string) (string, error) {
	log.WithFields("url", i.config.URL, "version", version).Debug("installing from hosted shell script")

	const scriptName = "install.sh"

	scriptPath := filepath.Join(destDir, scriptName)
	if err := internal.DownloadFile(i.config.URL, scriptPath, ""); err != nil {
		return "", fmt.Errorf("failed to download script: %v", err)
	}

	argStr, err := templateFlags(i.config.Args, version, destDir)
	if err != nil {
		return "", fmt.Errorf("failed to template args: %v", err)
	}

	if err = i.scriptRunner(scriptPath, argStr); err != nil {
		return "", fmt.Errorf("failed to run script: %v", err)
	}

	lsResult, err := os.ReadDir(destDir)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %v", err)
	}

	var files []string
	for _, file := range lsResult {
		name := file.Name()
		if !strings.EqualFold(name, scriptName) {
			files = append(files, name)
		}
	}

	var binPath string
	switch len(files) {
	case 0:
		return "", fmt.Errorf("no files found in destination directory")
	case 1:
		binPath = filepath.Join(destDir, files[0])
	default:
		return "", fmt.Errorf("multiple files found in destination directory: %s", strings.Join(files, ", "))
	}

	return binPath, nil
}

func templateFlags(args string, version, destination string) (string, error) {
	tmpl, err := template.New("args").Parse(args)
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	err = tmpl.Execute(&buf, map[string]string{
		"Version":     version,
		"Destination": destination,
	})

	if err != nil {
		return "", err
	}

	result := buf.String()

	if !strings.Contains(result, version) {
		return "", fmt.Errorf("version not found in args template")
	}

	if !strings.Contains(result, destination) {
		return "", fmt.Errorf("destination not found in args template")
	}

	return result, nil
}

func runScript(scriptPath, argStr string) error {
	userArgs, err := shlex.Split(argStr)
	if err != nil {
		return fmt.Errorf("failed to parse args: %v", err)
	}

	args := []string{scriptPath}
	args = append(args, userArgs...)

	log.Trace("running: <script> " + strings.Join(args, " "))

	cmd := exec.Command("sh", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %v\nOutput: %s", err, output)
	}
	return nil
}
