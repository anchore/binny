package cli

import (
	"path/filepath"
	"testing"
)

func TestInstallCmd(t *testing.T) {

	type step struct {
		name string
		// root is the store dir within the test dir, so steps can install into more than one store
		root       string
		args       []string
		env        map[string]string
		assertions []traitAssertion
	}

	tests := []struct {
		name string
		// cache enables the cache for every step, rooted at "cache" within the test dir
		cache bool
		steps []step
	}{
		{
			name: "use go-install method",
			steps: []step{
				{
					name: "install",
					args: []string{"install", "-c", "testdata/go-install-method.yaml"},
					assertions: []traitAssertion{
						assertSuccessfulReturnCode,
						assertFileInStoreExists(".binny.state.json"),
						assertFileInStoreExists("binny"),
						assertManagedToolOutput("binny", []string{"--version"}, "binny v0.7.0\n"),
					},
				},
				{
					name: "list",
					args: []string{"list", "-c", "testdata/go-install-method.yaml", "-o", "json"},
					assertions: []traitAssertion{
						assertSuccessfulReturnCode,
						assertJson,
						assertInOutput(`"installedVersion": "v0.7.0"`),
					},
				},
			},
		},
		{
			name:  "use shared cache",
			cache: true,
			steps: []step{
				{
					name: "install into first store",
					root: "store-a",
					args: []string{"install", "-c", "testdata/go-install-method.yaml"},
					assertions: []traitAssertion{
						assertSuccessfulReturnCode,
						assertManagedToolOutput("binny", []string{"--version"}, "binny v0.7.0\n"),
					},
				},
				{
					name: "install into second store",
					root: "store-b",
					args: []string{"install", "-c", "testdata/go-install-method.yaml"},
					assertions: []traitAssertion{
						assertSuccessfulReturnCode,
						assertManagedToolOutput("binny", []string{"--version"}, "binny v0.7.0\n"),
						assertDirEntryCount(filepath.Join("..", "cache", "binny", "v0.7.0"), 1),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// we always have a clean slate for every test, but a shared state for each step
			d := t.TempDir()

			for _, s := range test.steps {
				t.Run(s.name, func(t *testing.T) {
					if s.env == nil {
						s.env = make(map[string]string)
					}
					root := filepath.Join(d, s.root)
					s.env["BINNY_ROOT"] = root
					if test.cache {
						s.env["BINNY_CACHE_ENABLED"] = "true"
						s.env["BINNY_CACHE_ROOT"] = filepath.Join(d, "cache")
					}

					cmd, stdout, stderr := runBinny(t, s.env, s.args...)
					for _, traitFn := range s.assertions {
						traitFn(t, root, stdout, stderr, cmd.ProcessState.ExitCode())
					}

					logOutputOnFailure(t, cmd, stdout, stderr)
				})
			}
		})
	}
}
