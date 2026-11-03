package goproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/mod/module"

	"github.com/anchore/binny/internal"
)

// goListModule is the subset of `go list -m -json` output binny uses.
type goListModule struct {
	Version  string    `json:"Version"`
	Versions []string  `json:"Versions"`
	Time     time.Time `json:"Time"`
}

// goList asks the go command about a module, so lookups follow the same rules `go install` does: the full GOPROXY
// list (direct, off, fallthrough), GOPRIVATE/GONOPROXY over VCS, GOAUTH/.netrc, and the tool's own env on top.
func goList(ctx context.Context, env []string, mod string, args ...string) (*goListModule, error) {
	path, _, _ := strings.Cut(mod, "@")
	if err := module.CheckPath(path); err != nil {
		return nil, fmt.Errorf("invalid module path %q: %w", path, err)
	}

	out, err := internal.GoCommand(ctx, env, append(append([]string{"list", "-m", "-json"}, args...), mod)...)
	if err != nil {
		return nil, err
	}

	var m goListModule
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("failed to parse go list output: %w", err)
	}
	return &m, nil
}

// listVersions returns the tagged versions of a module.
func listVersions(ctx context.Context, env []string, mod string) ([]string, error) {
	m, err := goList(ctx, env, mod, "-versions")
	if err != nil {
		return nil, err
	}
	return m.Versions, nil
}

// queryVersion resolves a version or git ref to the canonical module version and its timestamp.
func queryVersion(ctx context.Context, env []string, mod, version string) (*versionInfo, error) {
	m, err := goList(ctx, env, mod+"@"+version)
	if err != nil {
		return nil, err
	}
	return &versionInfo{Version: m.Version, Time: m.Time}, nil
}
