package internal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// goCommandTimeout bounds one go command. A cold lookup of a private module goes through VCS, and a go env probe may
// download the toolchain GOTOOLCHAIN names, hence generous.
const goCommandTimeout = 2 * time.Minute

// maxErrorSize caps how much of the go command's stderr ends up in an error message.
const maxErrorSize = 512

// GoCommand runs the go command with env on top of the process env, in an empty dir with GOWORK=off, so a go.mod or
// go.work in the cwd can't change the answer or trigger a toolchain switch. A go.mod in the temp root itself is ignored
// by the go command. A failure carries the command's stderr, truncated and quoted.
func GoCommand(ctx context.Context, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, goCommandTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "binny-go-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), env...), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, quoteOutput(stderr.Bytes()))
	}
	return out, nil
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
