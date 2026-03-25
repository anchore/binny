package binny

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_formatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{
			name: "zero",
			d:    0,
			want: "0s",
		},
		{
			name: "negative",
			d:    -1 * time.Hour,
			want: "0s",
		},
		{
			name: "sub-hour minutes",
			d:    45 * time.Minute,
			want: "45m0s",
		},
		{
			name: "exact hours no days",
			d:    5 * time.Hour,
			want: "5h0m0s",
		},
		{
			name: "exact days",
			d:    3 * 24 * time.Hour,
			want: "3d",
		},
		{
			name: "days and hours",
			d:    3*24*time.Hour + 5*time.Hour,
			want: "3d5h",
		},
		{
			name: "one day",
			d:    24 * time.Hour,
			want: "1d",
		},
		{
			name: "one day and one hour",
			d:    25 * time.Hour,
			want: "1d1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatDuration(tt.d))
		})
	}
}

func TestCooldownError_Error(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name            string
		err             CooldownError
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "with version and date info",
			err: CooldownError{
				Cooldown:      7 * 24 * time.Hour,
				LatestVersion: "v2.0.0",
				LatestDate:    timePtr(now.Add(-2 * 24 * time.Hour)),
			},
			wantContains: []string{
				`"v2.0.0"`,
				"published",
				"7d",
				"--ignore-cooldown",
				"try again in",
			},
		},
		{
			name: "without version info falls back to generic message",
			err: CooldownError{
				Cooldown: 7 * 24 * time.Hour,
			},
			wantContains: []string{
				"no version found",
				"7d",
				"--ignore-cooldown",
			},
		},
		{
			name: "remaining time is zero when cooldown has already passed",
			err: CooldownError{
				Cooldown:      1 * time.Hour,
				LatestVersion: "v1.0.0",
				LatestDate:    timePtr(now.Add(-2 * time.Hour)),
			},
			// the remaining time should show "0s" since cooldown has technically passed
			wantContains: []string{
				"try again in 0s",
			},
		},
		{
			name: "candidate limit info shown when limit was hit with version details",
			err: CooldownError{
				Cooldown:      7 * 24 * time.Hour,
				LatestVersion: "v3.0.0",
				LatestDate:    timePtr(now.Add(-1 * 24 * time.Hour)),
				CheckedCount:  10,
				TotalCount:    25,
			},
			wantContains: []string{
				`"v3.0.0"`,
				"only checked 10 of 25 candidates",
				"--ignore-cooldown",
			},
		},
		{
			name: "candidate limit info shown when limit was hit without version details",
			err: CooldownError{
				Cooldown:     7 * 24 * time.Hour,
				CheckedCount: 10,
				TotalCount:   25,
			},
			wantContains: []string{
				"no version found",
				"only checked 10 of 25 candidates",
			},
		},
		{
			name: "no candidate limit suffix when all candidates were checked",
			err: CooldownError{
				Cooldown:      7 * 24 * time.Hour,
				LatestVersion: "v2.0.0",
				LatestDate:    timePtr(now.Add(-1 * 24 * time.Hour)),
				CheckedCount:  5,
				TotalCount:    5,
			},
			wantContains: []string{
				`"v2.0.0"`,
				"--ignore-cooldown",
			},
			wantNotContains: []string{
				"only checked",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, s := range tt.wantContains {
				assert.Contains(t, msg, s)
			}
			for _, s := range tt.wantNotContains {
				assert.NotContains(t, msg, s)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
