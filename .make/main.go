package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	. "github.com/anchore/go-make"
	"github.com/anchore/go-make/config"
	"github.com/anchore/go-make/file"
	"github.com/anchore/go-make/lang"
	"github.com/anchore/go-make/run"
	"github.com/anchore/go-make/tasks/golint"
	"github.com/anchore/go-make/tasks/goreleaser"
	"github.com/anchore/go-make/tasks/gotest"
)

func main() {
	Makefile(
		golint.Tasks(),
		goreleaser.Tasks(),
		gotest.Tasks(gotest.ExcludeGlob("**/test/cli/**")),
		gotest.FixtureTasks().RunOn("unit"),
		// cli tests exec the snapshot binary, so the race detector would only instrument the test harness (see
		// cli:race-smoke for the binary itself). The snapshot is built only when missing: in CI it is downloaded
		// from the snapshot job, and rebuilding would burn time and clobber it.
		Task{
			Name:        "cli",
			Description: "run cli tests",
			RunsOn:      lang.List("test"),
			Run: func() {
				bin := snapshotBinPath()
				if !file.Exists(bin) {
					Log("snapshot binary not found at %s, building a single-target snapshot", bin)
					Run("make snapshot:single-target")
				}
				Log("testing binary: %s", bin)
				Run("go test -count=1 -timeout=15m ./test/cli/...", run.Env("BINNY_BINARY_LOCATION", bin), run.Stdout(os.Stderr))
			},
		},
		Task{
			Name:        "cli:race-smoke",
			Description: "exercise the cli with the race detector",
			RunsOn:      lang.List("cli"),
			Run: func() {
				if !raceEnabled() {
					Log("race detector disabled, skipping race smoke")
					return
				}
				file.WithTempDir(func(root string) {
					Run("go run -race ./cmd/binny install -c .binny.yaml", run.Env("BINNY_ROOT", root), run.Stdout(os.Stderr))
				})
			},
		},
		// TODO: replace with go-make's built-in cross-compile assertion once upstream supports it.
		Task{
			Name:        "assert-windows-build",
			Description: "ensure binny compiles on Windows",
			RunsOn:      []string{"default"},
			Run: func() {
				Run("go build ./cmd/binny", run.Env("GOOS", "windows"))
			},
		},
	)
}

// snapshotBinPath is where goreleaser writes the snapshot binary for the current OS and arch.
func snapshotBinPath() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "amd64_v1"
	case "arm64":
		arch = "arm64_v8.0"
	}
	return filepath.Join(RootDir(), "snapshot", runtime.GOOS+"-build_"+runtime.GOOS+"_"+arch, "binny")
}

// raceEnabled defaults to on in CI (off on windows) and can be forced either way with RACE=true|false.
func raceEnabled() bool {
	if enabled, err := strconv.ParseBool(config.Env("RACE", "")); err == nil {
		return enabled
	}
	return config.CI && !config.Windows
}
