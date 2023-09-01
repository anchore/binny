package goinstall

import (
	"fmt"
	"strings"

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

	return goproxy.ResolveMethod, goproxy.VersionResolutionParameters{
		Module: params.Module,
	}, nil
}
