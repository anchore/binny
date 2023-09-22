package command

import (
	"fmt"
	"strings"

	"github.com/google/shlex"
	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"

	"github.com/anchore/binny/cmd/binny/cli/internal/yamlpatch"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/goinstall"
	"github.com/anchore/clio"
)

type AddGoInstallConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`

	// CLI options
	Install struct {
		GoInstall option.GoInstall `json:"go-install" yaml:"go-install" mapstructure:"go-install"`
	} `json:"install" yaml:"install" mapstructure:"install"`

	VersionResolution option.VersionResolution `json:"version-resolver" yaml:"version-resolver" mapstructure:"version-resolver"`
}

func AddGoInstall(app clio.Application) *cobra.Command {
	cfg := &AddGoInstallConfig{
		Core: option.DefaultCore(),
	}

	return app.SetupCommand(&cobra.Command{
		Use:   "go-install NAME@VERSION --module GOMODULE [--entrypoint PATH] [--ldflags FLAGS]",
		Short: "Add a new tool configuration from 'go install ...' invocations",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if cfg.Install.GoInstall.Module == "" {
				return fmt.Errorf("go-install configuration requires '--module' option")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddGoInstallConfig(*cfg, args[0])
		},
	}, cfg)
}

func runAddGoInstallConfig(cmdCfg AddGoInstallConfig, nameVersion string) error {
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

	iCfg := cmdCfg.Install.GoInstall
	vCfg := cmdCfg.VersionResolution

	ldFlagsList, err := shlex.Split(iCfg.LDFlags)
	if err != nil {
		return fmt.Errorf("invalid ldflags: %w", err)
	}

	coreInstallParams := goinstall.InstallerParameters{
		Module:     iCfg.Module,
		Entrypoint: iCfg.Entrypoint,
		LDFlags:    ldFlagsList,
	}

	installParamMap, err := toMap(coreInstallParams)
	if err != nil {
		return fmt.Errorf("unable to encode install params: %w", err)
	}

	installMethod := goinstall.InstallMethod

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

	if err := yamlpatch.Write(cmdCfg.Config, yamlToolAppender{toolCfg: toolCfg}); err != nil {
		return fmt.Errorf("unable to write config: %w", err)
	}

	reportNewConfiguration(toolCfg)

	return nil
}
