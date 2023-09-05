package goinstall

import (
	"fmt"
	"strings"

	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/goproxy"
)

const InstallMethod = "go-install"

func IsInstallMethod(method string) bool {
	switch strings.ToLower(method) {
	case "go", "go install", "goinstall", "golang", InstallMethod:
		return true
	}
	return false
}

func DefaultVersionResolverConfig(installParams any) (string, any, error) {
	params, ok := installParams.(InstallerParameters)
	if !ok {
		return "", nil, fmt.Errorf("invalid go install parameters")
	}

	if strings.HasPrefix(params.Module, ".") || strings.HasPrefix(params.Module, "/") {
		// this is a path to a local repo, version updating should be disabled and the version should be
		// set to the current vcs value
		return git.ResolveMethod, git.VersionResolutionParameters{
			Path: params.Module,
		}, nil
	}

	return goproxy.ResolveMethod, goproxy.VersionResolutionParameters{
		Module: params.Module,
	}, nil
}
