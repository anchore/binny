package goproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	internalhttp "github.com/anchore/binny/internal/http"
	"github.com/anchore/binny/internal/log"
)

const latest = "latest"

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config                   VersionResolutionParameters
	availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
}

type VersionResolutionParameters struct {
	Module                 string `json:"module" yaml:"module" mapstructure:"module"`
	AllowUnresolvedVersion bool   `json:"allow-unresolved-version" yaml:"allow-unresolved-version" mapstructure:"allow-unresolved-version"`
}

func NewVersionResolver(cfg VersionResolutionParameters) *VersionResolver {
	return &VersionResolver{
		config:                   cfg,
		availableVersionsFetcher: availableVersionsFetcher,
	}
}

func (v VersionResolver) ResolveVersion(ctx context.Context, want, _ string) (string, error) {
	log.FromContext(ctx).WithFields("module", v.config.Module, "version", want).Trace("resolving version from go proxy")
	if internal.IsSemver(want) {
		return want, nil
	}

	if want == latest && !v.config.AllowUnresolvedVersion {
		return v.findLatestVersion(ctx, "")
	}

	// TODO: dunno

	return want, nil
}

func (v VersionResolver) UpdateVersion(ctx context.Context, want, constraint string) (string, error) {
	if want == latest {
		if constraint != "" {
			return "", fmt.Errorf("cannot specify a version constraint with 'latest' go module version")
		}
		return want, nil
	}

	if internal.IsSemver(want) {
		return v.findLatestVersion(ctx, constraint)
	}

	// TODO: dunno

	return want, nil
}

func (v VersionResolver) findLatestVersion(ctx context.Context, versionConstraint string) (string, error) {
	lgr := log.FromContext(ctx)

	// ask the go proxy for the latest version
	url := "https://proxy.golang.org/" + v.config.Module + "/@v/list"
	versions, err := v.availableVersionsFetcher(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to get available versions from go proxy: %v", err)
	}

	latestVersion, err := internal.FilterToLatestVersion(versions, versionConstraint)
	if err != nil {
		return "", fmt.Errorf("failed to filter latest version: %v", err)
	}

	if latestVersion != "" {
		lgr.WithFields(latest, latestVersion, "module", v.config.Module).
			Trace("found latest version from the go proxy")
	} else {
		lgr.WithFields("module", v.config.Module).Trace("could not resolve latest version from go proxy")
	}

	if latestVersion == "" {
		if v.config.AllowUnresolvedVersion {
			// this can happen if the source repo has no tags, the proxy then won't know about it.
			lgr.WithFields("module", v.config.Module).Trace("using 'latest' as the version")
			return latest, nil
		}
		return "", fmt.Errorf("could not resolve latest version for module %q", v.config.Module)
	}

	return latestVersion, nil
}

func availableVersionsFetcher(ctx context.Context, url string) ([]string, error) {
	// TODO: honor GOPROXY env vars
	lgr := log.FromContext(ctx)
	client := internalhttp.ClientFromContext(ctx)

	lgr.WithFields("url", url).Trace("requesting latest version")

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get available versions from go proxy: %s", resp.Status)
	}

	// get the last entry in a newline delimited list
	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(contents), "\n")
	return lines, nil
}
