package goinstall

import (
	"fmt"
	"strings"

	"github.com/anchore/binny/tool/gobuild"
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

	if gobuild.IsLocalModule(params.Module) {
		// local modules are built by go-build, so resolve their version the same way (from the git repo)
		return gobuild.DefaultVersionResolverConfig(gobuild.InstallerParameters{Module: params.Module})
	}

	return goproxy.ResolveMethod, goproxy.VersionResolutionParameters{
		Module: params.Module,
	}, nil
}
