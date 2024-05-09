package cli

import (
	"testing"
)

func TestInstallCmd(t *testing.T) {

	type step struct {
		name       string
		args       []string
		env        map[string]string
		assertions []traitAssertion
	}

	tests := []struct {
		name  string
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
					s.env["BINNY_ROOT"] = d

					cmd, stdout, stderr := runBinny(t, s.env, s.args...)
					for _, traitFn := range s.assertions {
						traitFn(t, d, stdout, stderr, cmd.ProcessState.ExitCode())
					}

					logOutputOnFailure(t, cmd, stdout, stderr)
				})
			}
		})
	}
}
