package main

import (
	. "github.com/anchore/go-make"
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
		gotest.Tasks(
			gotest.Name("cli"),
			gotest.IncludeGlob("./test/cli/..."),
			gotest.NoCoverage(),
		).DependsOn("snapshot"),
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
