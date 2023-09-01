package goproxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMethods(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    bool
	}{
		{
			name:    "valid",
			methods: []string{"go-proxy", "go proxy", "goproxy"},
			want:    true,
		},
		{
			name:    "invalid",
			methods: []string{"made up"},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, method := range tt.methods {
				t.Run(method, func(t *testing.T) {
					t.Run("IsResolveMethod", func(t *testing.T) {
						assert.Equal(t, tt.want, IsResolveMethod(method))
					})
				})
			}
		})
	}
}
