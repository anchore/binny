package command

import (
	"fmt"
	"strings"

	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"

	"github.com/anchore/binny/cmd/binny/cli/internal/yamlpatch"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/clio"
)

type AddGithubReleaseConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`

	VersionResolution option.VersionResolution `json:"version-resolver" yaml:"version-resolver" mapstructure:"version-resolver"`
}

func AddGithubRelease(app clio.Application) *cobra.Command {
	cfg := &AddGithubReleaseConfig{
		Core: option.DefaultCore(),
	}

	return app.SetupCommand(&cobra.Command{
		Use:   "github-release OWNER/REPO@VERSION",
		Short: "Add a new tool configuration that sources binaries from GitHub releases",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if !strings.Contains(args[0], "/") {
				return fmt.Errorf("invalid 'owner/project@version' format: %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGithubReleaseConfig(*cfg, args[0])
		},
	}, cfg)
}

func runGithubReleaseConfig(cmdCfg AddGithubReleaseConfig, repoVersion string) error {
	fields := strings.Split(repoVersion, "@")
	var repo, name, version string

	switch len(fields) {
	case 1:
		repo = repoVersion
		version = "latest"
	case 2:
		repo = fields[0]
		version = fields[1]
	default:
		return fmt.Errorf("invalid owner/project@version format: %s", repoVersion)
	}

	fields = strings.Split(repo, "/")
	if len(fields) != 2 {
		return fmt.Errorf("invalid owner/project format: %s", repo)
	}

	name = fields[1]

	if strset.New(cmdCfg.Tools.Names()...).Has(name) {
		// TODO: should this be an error?
		log.Warnf("tool %q already configured", name)
		return nil
	}

	vCfg := cmdCfg.VersionResolution

	coreInstallParams := githubrelease.InstallerParameters{
		Repo: repo,
	}

	installParamMap, err := toMap(coreInstallParams)
	if err != nil {
		return fmt.Errorf("unable to encode install params: %w", err)
	}

	installMethod := githubrelease.InstallMethod

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

	return yamlpatch.Write(cmdCfg.Config, yamlToolAppender{toolCfg: toolCfg})
}
