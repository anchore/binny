package hostedshell

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/anchore/binny/tool/githubrelease"
)

const InstallMethod = "hosted-shell"

func IsInstallMethod(method string) bool {
	switch strings.ToLower(method) {
	case InstallMethod, "hostedshell", "hosted shell", "hostedscript", "hosted script", "hosted-script":
		return true
	}
	return false
}

func DefaultVersionResolverConfig(installParams any) (string, any, error) {
	params, ok := installParams.(InstallerParameters)
	if !ok {
		return "", nil, fmt.Errorf("invalid hosted shell parameters")
	}

	if strings.Contains(params.URL, "github.com") || strings.Contains(params.URL, "raw.githubusercontent.com") {
		u, err := url.Parse(params.URL)
		if err != nil {
			return "", nil, fmt.Errorf("failed to github release parse url %q: %v", params.URL, err)
		}

		fields := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(fields) < 2 {
			return "", nil, fmt.Errorf("invalid github release url %q", params.URL)
		}

		repo := fmt.Sprintf("%s/%s", fields[0], fields[1])

		return githubrelease.ResolveMethod, githubrelease.VersionResolutionParameters{
			Repo: repo,
		}, nil
	}

	return "", nil, fmt.Errorf("no default version resolver for hosted shell with the current configuration")
}
