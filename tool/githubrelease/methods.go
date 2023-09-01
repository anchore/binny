package githubrelease

import (
	"fmt"
	"strings"
)

const (
	ResolveMethod = "github-release"
	InstallMethod = ResolveMethod
)

func IsResolveMethod(method string) bool {
	return IsInstallMethod(method)
}

func IsInstallMethod(method string) bool {
	switch strings.ToLower(method) {
	case "github", "github release", "githubrelease", InstallMethod:
		return true
	}
	return false
}

func DefaultVersionResolverConfig(installParams any) (string, any, error) {
	params, ok := installParams.(InstallerParameters)
	if !ok {
		return "", nil, fmt.Errorf("invalid go install parameters")
	}

	return ResolveMethod, VersionResolutionParameters{ // nolint: gosimple
		Repo: params.Repo,
	}, nil
}
