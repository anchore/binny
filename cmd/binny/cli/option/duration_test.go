package option

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJSONDuration_UnmarshalText(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      time.Duration
		wantIsSet bool
		wantErr   require.ErrorAssertionFunc
	}{
		{
			name:      "zero string",
			input:     "0",
			want:      0,
			wantIsSet: true,
		},
		{
			name:      "empty string",
			input:     "",
			want:      0,
			wantIsSet: true,
		},
		{
			name:      "days shorthand",
			input:     "7d",
			want:      7 * 24 * time.Hour,
			wantIsSet: true,
		},
		{
			name:      "one day",
			input:     "1d",
			want:      24 * time.Hour,
			wantIsSet: true,
		},
		{
			name:      "zero days",
			input:     "0d",
			want:      0,
			wantIsSet: true,
		},
		{
			name:      "go duration hours",
			input:     "168h",
			want:      168 * time.Hour,
			wantIsSet: true,
		},
		{
			name:      "go duration minutes",
			input:     "30m",
			want:      30 * time.Minute,
			wantIsSet: true,
		},
		{
			name:      "go duration mixed",
			input:     "2h30m",
			want:      2*time.Hour + 30*time.Minute,
			wantIsSet: true,
		},
		{
			name:      "whitespace trimmed",
			input:     "  7d  ",
			want:      7 * 24 * time.Hour,
			wantIsSet: true,
		},
		{
			name:    "negative days",
			input:   "-1d",
			wantErr: require.Error,
		},
		{
			name:    "negative go duration",
			input:   "-1h",
			wantErr: require.Error,
		},
		{
			name:    "invalid format",
			input:   "abc",
			wantErr: require.Error,
		},
		{
			name:    "non-numeric days",
			input:   "abcd",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			var d JSONDuration
			err := d.UnmarshalText([]byte(tt.input))
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, d.Duration)
			require.Equal(t, tt.wantIsSet, d.IsSet, "IsSet should reflect that the value was explicitly configured")
		})
	}
}

func TestJSONDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      time.Duration
		wantIsSet bool
		wantErr   require.ErrorAssertionFunc
	}{
		{
			name:      "days shorthand via YAML",
			input:     "7d",
			want:      7 * 24 * time.Hour,
			wantIsSet: true,
		},
		{
			name:      "go duration via YAML",
			input:     "2h30m",
			want:      2*time.Hour + 30*time.Minute,
			wantIsSet: true,
		},
		{
			name:    "invalid via YAML",
			input:   "bogus",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			var d JSONDuration
			err := d.UnmarshalYAML(func(v interface{}) error {
				ptr, ok := v.(*string)
				if !ok {
					t.Fatal("expected *string")
				}
				*ptr = tt.input
				return nil
			})
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, d.Duration)
			require.Equal(t, tt.wantIsSet, d.IsSet)
		})
	}
}

func TestJSONDuration_MarshalText(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "zero",
			duration: 0,
			want:     "0",
		},
		{
			name:     "exact days",
			duration: 7 * 24 * time.Hour,
			want:     "7d",
		},
		{
			name:     "one day",
			duration: 24 * time.Hour,
			want:     "1d",
		},
		{
			name:     "non-exact days",
			duration: 25 * time.Hour,
			want:     "25h0m0s",
		},
		{
			name:     "hours and minutes",
			duration: 2*time.Hour + 30*time.Minute,
			want:     "2h30m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := JSONDuration{Duration: tt.duration}
			got, err := d.MarshalText()
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}
