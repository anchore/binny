package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateEnvSlice(t *testing.T) {
	tests := []struct {
		name    string
		env     []string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name: "valid env vars",
			env:  []string{"FOO=bar", "BAZ=qux"},
		},
		{
			name: "empty slice",
			env:  []string{},
		},
		{
			name: "nil slice",
			env:  nil,
		},
		{
			name:    "missing equals sign",
			env:     []string{"FOO"},
			wantErr: require.Error,
		},
		{
			name: "empty value is valid",
			env:  []string{"FOO="},
		},
		{
			name: "equals sign in value",
			env:  []string{"FOO=bar=baz"},
		},
		{
			name:    "second element invalid",
			env:     []string{"FOO=bar", "BAZ"},
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			err := ValidateEnvSlice(tt.env)
			tt.wantErr(t, err)
		})
	}
}
