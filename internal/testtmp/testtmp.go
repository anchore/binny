// Package testtmp is for test binaries that make caches. The cache refuses a root under a dir other users can write,
// sticky or not, which on linux is where t.TempDir() puts everything (/tmp).
package testtmp

import (
	"fmt"
	"os"
	"testing"
)

// Main runs the tests with TMPDIR pointed at a private dir under the user cache dir, so t.TempDir() is usable as a
// cache root, and exits with their result.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	base, err := os.UserCacheDir()
	if err == nil {
		err = os.MkdirAll(base, 0o700)
	}
	var dir string
	if err == nil {
		dir, err = os.MkdirTemp(base, "binny-test-")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to make a private temp dir, cache tests may fail under a shared one: %v\n", err)
		return m.Run()
	}
	defer os.RemoveAll(dir)

	if err := os.Setenv("TMPDIR", dir); err != nil {
		fmt.Fprintf(os.Stderr, "unable to set TMPDIR: %v\n", err)
	}
	return m.Run()
}
