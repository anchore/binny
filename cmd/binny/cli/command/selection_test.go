package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/cmd/binny/cli/option"
)

func Test_validateToolNames(t *testing.T) {
	tools := option.Tools{
		{Name: "syft"},
		{Name: "grype"},
	}

	tests := []struct {
		name    string
		names   []string
		wantErr string
	}{
		{
			name: "no requested names selects every configured tool",
		},
		{
			name:  "configured names are accepted",
			names: []string{"syft", "grype"},
		},
		{
			name:    "unknown name is an error",
			names:   []string{"nope"},
			wantErr: "no tool configured with name: nope",
		},
		{
			name:    "unknown name mixed with a configured one is an error",
			names:   []string{"syft", "nope"},
			wantErr: "no tool configured with name: nope",
		},
		{
			name:    "several unknown names are listed in order",
			names:   []string{"zeta", "alpha"},
			wantErr: "no tool configured with name: alpha, zeta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToolNames(tools, tt.names)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}
