package option

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/mitchellh/mapstructure"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/goinstall"
	"github.com/anchore/binny/tool/goproxy"
	"github.com/anchore/binny/tool/hostedshell"
)

type Tool struct {
	Name    string            `json:"name" yaml:"name" mapstructure:"name"`
	Version ToolVersionConfig `json:"version" yaml:"version" mapstructure:"version"`

	InstallMethod string         `json:"method" yaml:"method,omitempty" mapstructure:"method"`
	Parameters    map[string]any `json:"with" yaml:"with,omitempty" mapstructure:"with"`
}

type ToolVersionConfig struct {
	Want          string `json:"want" yaml:"want" mapstructure:"want"`
	Constraint    string `json:"constraint" yaml:"constraint,omitempty" mapstructure:"constraint"`
	ResolveMethod string `json:"method" yaml:"method,omitempty" mapstructure:"method"`

	Parameters map[string]any `json:"with" yaml:"with,omitempty" mapstructure:"with"`
}

func (t Tool) ToTool() (binny.Tool, *binny.VersionIntent, error) {
	cfg, intent, err := t.ToConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read tool %q config: %w", t.Name, err)
	}

	toolObj, err := tool.New(*cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to inflate tool %q: %w", cfg.Name, err)
	}
	return toolObj, intent, nil
}

func (t Tool) ToConfig() (*tool.Config, *binny.VersionIntent, error) {
	installParams, err := deriveInstallParameters(t.Name, t.InstallMethod, t.Parameters, runtime.GOOS)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to derive install parameters for tool %q: %w", t.Name, err)
	}

	versionResolveMethod, versionResolveParams, err := deriveVersionResolveParameters(t.Version.ResolveMethod, t.Version.Parameters)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to derive version resolution parameters for tool %q: %w", t.Name, err)
	}

	cfg := &tool.Config{
		Name: t.Name,
		InstallerConfig: tool.DetailConfig{
			Method:     t.InstallMethod,
			Parameters: installParams,
		},
		VersionResolverConfig: tool.DetailConfig{
			Method:     versionResolveMethod,
			Parameters: versionResolveParams,
		},
	}

	intent := &binny.VersionIntent{
		Want:       t.Version.Want,
		Constraint: t.Version.Constraint,
	}

	return cfg, intent, nil
}

func deriveInstallParameters(name string, installMethod string, installParams map[string]any, goos string) (any, error) {
	switch {
	case goinstall.IsInstallMethod(installMethod):
		var params goinstall.InstallerParameters
		if err := mapstructure.Decode(installParams, &params); err != nil {
			return nil, err
		}
		return params, nil

	case hostedshell.IsInstallMethod(installMethod):
		var params hostedshell.InstallerParameters
		if err := mapstructure.Decode(installParams, &params); err != nil {
			return nil, err
		}
		return params, nil

	case githubrelease.IsInstallMethod(installMethod):
		var params githubrelease.InstallerParameters
		if err := mapstructure.Decode(installParams, &params); err != nil {
			return nil, err
		}
		if params.Binary == "" {
			// if not provided, assume that the binary name is the same as the configured tool name
			params.Binary = name
			if goos == "windows" {
				params.Binary += ".exe"
			}
		}
		return params, nil
	case installMethod == "":
		return nil, nil
	}
	return nil, fmt.Errorf("unknown install method: %s", installMethod)
}

func deriveVersionResolveParameters(resolveMethod string, versionParameters map[string]any) (string, any, error) {
	switch {
	case githubrelease.IsResolveMethod(resolveMethod):
		var params githubrelease.VersionResolutionParameters
		if err := mapstructure.Decode(versionParameters, &params); err != nil {
			return resolveMethod, nil, err
		}
		return resolveMethod, params, nil

	case goproxy.IsResolveMethod(resolveMethod):
		var params goproxy.VersionResolutionParameters
		if err := mapstructure.Decode(versionParameters, &params); err != nil {
			return resolveMethod, nil, err
		}
		return resolveMethod, params, nil
	case resolveMethod == "":
		return resolveMethod, nil, nil
	}

	return resolveMethod, nil, fmt.Errorf("unknown version resolution method: %s", resolveMethod)
}

type Tools []Tool

func (t Tools) GetOption(name string) *Tool {
	for _, tObj := range t {
		if tObj.Name == name {
			return &tObj
		}
	}
	return nil
}

func (t Tools) GetAllOptions(names []string) ([]Tool, error) {
	var notFound []string
	tools := make([]Tool, len(names))
	for i, name := range names {
		tObj := t.GetOption(name)
		if tObj == nil {
			notFound = append(notFound, name)
			continue
		}
		tools[i] = *tObj
	}

	if len(notFound) > 0 {
		return nil, fmt.Errorf("tools not configured: %s", strings.Join(notFound, ", "))
	}

	return tools, nil
}

func (t Tools) Names() []string {
	names := make([]string, len(t))
	for i, tObj := range t {
		names[i] = tObj.Name
	}
	return names
}
