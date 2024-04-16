package hostedshell

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstaller_InstallTo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script based installer is not supported on windows")
	}

	type fields struct {
		config       InstallerParameters
		scriptRunner func(scriptPath string, argStr string) error
	}
	type args struct {
		version string
		destDir string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "happy path",
			fields: fields{
				config: InstallerParameters{
					URL: func() string {
						s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							require.Equal(t, "GET", r.Method)
							_, err := w.Write([]byte("set -eu; echo 'hello world'; touch $1/syft"))
							require.NoError(t, err)

							return
						}))
						t.Cleanup(s.Close)
						return s.URL
					}(),
					Args: "{{ .Destination }} {{ .Version }} ",
				},
				scriptRunner: func(scriptPath string, argStr string) error {
					contents, err := os.ReadFile(scriptPath)
					require.NoError(t, err)
					require.Equal(t, "set -eu; echo 'hello world'; touch $1/syft", string(contents))
					require.NotEmpty(t, argStr)
					require.Contains(t, argStr, "1.2.3")
					require.NoError(t, runScript(scriptPath, argStr))
					return nil
				},
			},
			args: args{
				version: "1.2.3",
				destDir: t.TempDir(),
			},
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := NewInstaller(tt.fields.config)
			i.scriptRunner = tt.fields.scriptRunner
			want := filepath.Join(tt.args.destDir, "syft")
			got, err := i.InstallTo(tt.args.version, tt.args.destDir)
			if !tt.wantErr(t, err, fmt.Sprintf("InstallTo(%v, %v)", tt.args.version, tt.args.destDir)) {
				return
			}
			assert.Equalf(t, want, got, "InstallTo(%v, %v)", tt.args.version, tt.args.destDir)
		})
	}
}
