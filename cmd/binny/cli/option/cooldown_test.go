package option

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_resolveEffectiveCooldown(t *testing.T) {
	tests := []struct {
		name           string
		ignoreCooldown bool
		global         JSONDuration
		perTool        JSONDuration
		want           time.Duration
	}{
		{
			name: "both unset returns zero",
			want: 0,
		},
		{
			name:   "global set, per-tool unset inherits global",
			global: JSONDuration{Duration: 7 * 24 * time.Hour, IsSet: true},
			want:   7 * 24 * time.Hour,
		},
		{
			name:    "per-tool set overrides global",
			global:  JSONDuration{Duration: 7 * 24 * time.Hour, IsSet: true},
			perTool: JSONDuration{Duration: 3 * 24 * time.Hour, IsSet: true},
			want:    3 * 24 * time.Hour,
		},
		{
			name:    "per-tool explicitly zero overrides non-zero global",
			global:  JSONDuration{Duration: 7 * 24 * time.Hour, IsSet: true},
			perTool: JSONDuration{Duration: 0, IsSet: true},
			want:    0,
		},
		{
			name:           "ignoreCooldown bypasses global",
			ignoreCooldown: true,
			global:         JSONDuration{Duration: 7 * 24 * time.Hour, IsSet: true},
			want:           0,
		},
		{
			name:           "ignoreCooldown bypasses per-tool",
			ignoreCooldown: true,
			perTool:        JSONDuration{Duration: 3 * 24 * time.Hour, IsSet: true},
			want:           0,
		},
		{
			name:           "ignoreCooldown bypasses both",
			ignoreCooldown: true,
			global:         JSONDuration{Duration: 7 * 24 * time.Hour, IsSet: true},
			perTool:        JSONDuration{Duration: 3 * 24 * time.Hour, IsSet: true},
			want:           0,
		},
		{
			name:    "per-tool set without global",
			perTool: JSONDuration{Duration: 5 * 24 * time.Hour, IsSet: true},
			want:    5 * 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEffectiveCooldown(tt.ignoreCooldown, tt.global, tt.perTool)
			require.Equal(t, tt.want, got)
		})
	}
}
