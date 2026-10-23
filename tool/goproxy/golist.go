package goproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

// goListTimeout bounds a single `go list` call. A cold lookup of a private module goes through VCS, hence generous.
const goListTimeout = 2 * time.Minute

// maxErrorSize caps how much of the go command's stderr ends up in an error message.
const maxErrorSize = 512

// goListModule is the subset of `go list -m -json` output binny uses.
type goListModule struct {
	Version  string    `json:"Version"`
	Versions []string  `json:"Versions"`
	Time     time.Time `json:"Time"`
}

// goList asks the go command about a module, so lookups follow the same rules `go install` does: the full GOPROXY
// list (direct, off, fallthrough), GOPRIVATE/GONOPROXY over VCS, GOAUTH/.netrc, and the tool's own env on top. It
// runs in an empty dir so a go.mod or go.work in the cwd can't change the answer or trigger a toolchain switch.
func goList(ctx context.Context, env []string, mod string, args ...string) (*goListModule, error) {
	path, _, _ := strings.Cut(mod, "@")
	if err := module.CheckPath(path); err != nil {
		return nil, fmt.Errorf("invalid module path %q: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(ctx, goListTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "binny-golist-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, "go", append(append([]string{"list", "-m", "-json"}, args...), mod)...) //nolint:gosec // G204: arg vector, no shell, module path checked above
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), env...), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, quoteOutput(stderr.Bytes()))
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

// quoteOutput formats go command output for an error message: truncated and quoted so it can't flood the output or
// smuggle terminal escape sequences (it can carry text from a proxy or VCS host).
func quoteOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrorSize {
		s = s[:maxErrorSize] + "..."
	}
	return fmt.Sprintf("%q", s)
}
