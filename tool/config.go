package tool

import (
	"fmt"

	"github.com/mitchellh/hashstructure/v2"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/goinstall"
	"github.com/anchore/binny/tool/goproxy"
	"github.com/anchore/binny/tool/hostedshell"
)

var _ binny.Tool = (*compositeTool)(nil)

type compositeTool struct {
	config Config
	binny.Installer
	binny.VersionResolver
}

type Config struct {
	Name                  string
	InstallerConfig       DetailConfig
	VersionResolverConfig DetailConfig
}

type DetailConfig struct {
	Method     string
	Parameters any
}

func (t *Config) normalize() error {
	// set the version resolution parameters
	if t.VersionResolverConfig.Method == "" {
		resolveMethod, versionParams, err := defaultVersionResolverConfig(t.InstallerConfig.Method, t.InstallerConfig.Parameters)
		if err != nil {
			return fmt.Errorf("failed to get default version resolution method: %w", err)
		}
		t.VersionResolverConfig.Method = resolveMethod
		t.VersionResolverConfig.Parameters = versionParams
	}
	return nil
}

func New(t Config) (binny.Tool, error) {
	if err := t.normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize tool config: %w", err)
	}

	installer, err := getInstaller(t.InstallerConfig.Method, t.InstallerConfig.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to get installer for tool %q: %w", t.Name, err)
	}

	resolver, err := getResolver(t.VersionResolverConfig.Method, t.VersionResolverConfig.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to get version resolver for tool %q: %w", t.Name, err)
	}

	return &compositeTool{
		config:          t,
		Installer:       installer,
		VersionResolver: resolver,
	}, nil
}

func getInstaller(method string, installParams any) (installer binny.Installer, err error) {
	switch {
	case goinstall.IsInstallMethod(method):
		params, ok := installParams.(goinstall.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid go install parameters")
		}

		installer = goinstall.NewInstaller(params)
	case hostedshell.IsInstallMethod(method):
		params, ok := installParams.(hostedshell.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid hosted shell install parameters")
		}

		installer = hostedshell.NewInstaller(params)
	case githubrelease.IsInstallMethod(method):
		params, ok := installParams.(githubrelease.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid github release install parameters")
		}

		installer = githubrelease.NewInstaller(params)
	}

	if err != nil {
		return nil, err
	}

	return installer, nil
}

func getResolver(method string, params any) (resolver binny.VersionResolver, err error) {
	switch {
	case goproxy.IsResolveMethod(method):
		config, ok := params.(goproxy.VersionResolutionParameters)
		if !ok {
			return nil, fmt.Errorf("invalid go proxy version resolution parameters")
		}
		resolver = goproxy.NewVersionResolver(config)
	case githubrelease.IsResolveMethod(method):
		config, ok := params.(githubrelease.VersionResolutionParameters)
		if !ok {
			return nil, fmt.Errorf("invalid github release version resolution parameters")
		}
		resolver = githubrelease.NewVersionResolver(config)
	}

	if err != nil {
		return nil, err
	}

	return resolver, nil
}

func defaultVersionResolverConfig(installMethod string, installParams any) (method string, parameters any, err error) {
	switch {
	case goinstall.IsInstallMethod(installMethod):
		return goinstall.DefaultVersionResolverConfig(installParams)
	case hostedshell.IsInstallMethod(installMethod):
		return hostedshell.DefaultVersionResolverConfig(installParams)
	case githubrelease.IsInstallMethod(installMethod):
		return githubrelease.DefaultVersionResolverConfig(installParams)
	}

	return "", nil, nil
}

func (c compositeTool) Name() string {
	return c.config.Name
}

func (c compositeTool) ID() string {
	f, err := hashstructure.Hash(c.config, hashstructure.FormatV2, &hashstructure.HashOptions{
		ZeroNil:      true,
		SlicesAsSets: true,
	})
	if err != nil {
		panic(fmt.Sprintf("could not hash tool config: %+v", err))
	}

	return fmt.Sprintf("%016x", f)
}
