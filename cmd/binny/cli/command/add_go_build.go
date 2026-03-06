package command

import (
	"fmt"
	"strings"

	"github.com/google/shlex"
	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"

	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/gobuild"
	"github.com/anchore/clio"
)

type AddGoBuildConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`

	// CLI options
	Install struct {
		GoBuild option.GoBuild `json:"go-build" yaml:"go-build" mapstructure:"go-build"`
	} `json:"install" yaml:"install" mapstructure:"install"`

	VersionResolution option.VersionResolution `json:"version-resolver" yaml:"version-resolver" mapstructure:"version-resolver"`
}

func AddGoBuild(app clio.Application) *cobra.Command {
	cfg := &AddGoBuildConfig{
		Core: option.DefaultCore(),
	}

	return app.SetupCommand(&cobra.Command{
		Use:   "go-build NAME@VERSION --module GOMODULE [--entrypoint PATH] [--ldflags FLAGS]",
		Short: "Add a new tool configuration that builds from source using 'go build'",
		Long: `Add a new tool configuration that builds from source using 'go build'.

Unlike 'go-install', this method clones the source repository and builds
within that context, ensuring that replace directives in go.mod are honored.

Examples:
  # Add a tool that builds from GitHub source
  binny add go-build mytool@v1.0.0 --module github.com/owner/repo --entrypoint cmd/mytool

  # Add a tool with custom ldflags
  binny add go-build mytool@v1.0.0 --module github.com/owner/repo -l "-X main.version={{ .Version }}"

  # Add a tool using go proxy to download source
  binny add go-build mytool@v1.0.0 --module github.com/owner/repo --source goproxy`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if cfg.Install.GoBuild.Module == "" {
				return fmt.Errorf("go-build configuration requires '--module' option")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runAddGoBuildConfig(*cfg, args[0])
		},
	}, cfg)
}

func runAddGoBuildConfig(cmdCfg AddGoBuildConfig, nameVersion string) error {
	fields := strings.Split(nameVersion, "@")
	var name, version string

	switch len(fields) {
	case 1:
		name = nameVersion
	case 2:
		name = fields[0]
		version = fields[1]
	default:
		return fmt.Errorf("invalid name@version format: %s", nameVersion)
	}

	if strset.New(cmdCfg.Tools.Names()...).Has(name) {
		// TODO: should this be an error?
		log.Warnf("tool %q already configured", name)
		return nil
	}

	iCfg := cmdCfg.Install.GoBuild
	vCfg := cmdCfg.VersionResolution

	ldFlagsList, err := shlex.Split(iCfg.LDFlags)
	if err != nil {
		return fmt.Errorf("invalid ldflags: %w", err)
	}

	if err := internal.ValidateEnvSlice(iCfg.Env); err != nil {
		return err
	}

	var sourceMode gobuild.SourceMode
	if iCfg.Source != "" {
		sourceMode = gobuild.SourceMode(iCfg.Source)
		if sourceMode != gobuild.SourceModeGit && sourceMode != gobuild.SourceModeGoProxy {
			return fmt.Errorf("invalid source mode %q: must be 'git' or 'go-proxy'", iCfg.Source)
		}
	}

	coreInstallParams := gobuild.InstallerParameters{
		Module:     iCfg.Module,
		Entrypoint: iCfg.Entrypoint,
		LDFlags:    ldFlagsList,
		Args:       iCfg.Args,
		Env:        iCfg.Env,
		Source:     sourceMode,
		RepoURL:    iCfg.RepoURL,
	}

	installParamMap, err := toMap(coreInstallParams)
	if err != nil {
		return fmt.Errorf("unable to encode install params: %w", err)
	}

	installMethod := gobuild.InstallMethod

	log.WithFields("name", name, "version", version, "method", installMethod).Info("adding tool")

	toolCfg := option.Tool{
		Name: name,
		Version: option.ToolVersionConfig{
			Want:          version,
			Constraint:    vCfg.Constraint,
			ResolveMethod: vCfg.Method,
		},
		InstallMethod: installMethod,
		Parameters:    installParamMap,
	}

	return updateConfiguration(cmdCfg.Config, toolCfg)
}
