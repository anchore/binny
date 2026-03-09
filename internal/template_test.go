package internal

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestTemplateString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		version string
		want    string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:    "simple string no template",
			input:   "hello world",
			version: "v1.0.0",
			want:    "hello world",
		},
		{
			name:    "template with version",
			input:   "version={{ .Version }}",
			version: "v1.2.3",
			want:    "version=v1.2.3",
		},
		{
			name:    "sprig function",
			input:   "{{ .Version | trimPrefix \"v\" }}",
			version: "v1.0.0",
			want:    "1.0.0",
		},
		{
			name:    "invalid template syntax",
			input:   "{{ .Invalid }",
			version: "v1.0.0",
			wantErr: require.Error,
		},
		{
			name:    "empty string",
			input:   "",
			version: "v1.0.0",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			got, err := TemplateString(tt.input, tt.version)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTemplateSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		version string
		want    []string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:    "empty slice",
			input:   []string{},
			version: "v1.0.0",
			want:    []string{},
		},
		{
			name:    "nil slice",
			input:   nil,
			version: "v1.0.0",
			want:    []string{},
		},
		{
			name:    "single element with template",
			input:   []string{"version={{ .Version }}"},
			version: "v1.0.0",
			want:    []string{"version=v1.0.0"},
		},
		{
			name:    "multiple elements",
			input:   []string{"-X main.version={{ .Version }}", "-trimpath"},
			version: "v2.0.0",
			want:    []string{"-X main.version=v2.0.0", "-trimpath"},
		},
		{
			name:    "error in one element",
			input:   []string{"valid", "{{ .Invalid }"},
			version: "v1.0.0",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			got, err := TemplateSlice(tt.input, tt.version)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			if d := cmp.Diff(tt.want, got); d != "" {
				t.Errorf("TemplateSlice mismatch (-want +got):\n%s", d)
			}
		})
	}
}

func TestTemplateFlags(t *testing.T) {
	tests := []struct {
		name    string
		ldFlags []string
		version string
		want    string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:    "simple flags",
			ldFlags: []string{"-X main.version=1.0.0"},
			version: "v1.0.0",
			want:    "-X main.version=1.0.0",
		},
		{
			name:    "template with version",
			ldFlags: []string{"-X main.version={{ .Version }}"},
			version: "v1.2.3",
			want:    "-X main.version=v1.2.3",
		},
		{
			name:    "multiple flags with templates",
			ldFlags: []string{"-X main.version={{ .Version }}", "-X main.commit=abc123"},
			version: "v2.0.0",
			want:    "-X main.version=v2.0.0 -X main.commit=abc123",
		},
		{
			name:    "invalid template",
			ldFlags: []string{"-X main.version={{ .Invalid }"},
			version: "v1.0.0",
			wantErr: require.Error,
		},
		{
			name:    "empty flags",
			ldFlags: []string{},
			version: "v1.0.0",
			want:    "",
		},
		{
			name:    "nil flags",
			ldFlags: nil,
			version: "v1.0.0",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			got, err := TemplateFlags(tt.ldFlags, tt.version)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}
