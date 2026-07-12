//go:build linux

package mistermain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFramebufferFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantFormat  string
		wantRB      int
		expectError bool
	}{
		{name: "rb disabled", input: "08888", wantFormat: "8888", wantRB: 0},
		{name: "rb enabled", input: VideoModeFormatRGB16, wantFormat: "565", wantRB: 1},
		{name: "too short", input: "1", expectError: true},
		{name: "invalid rb", input: "28888", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pixelFormat, rb, err := parseFramebufferFormat(tt.input)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFormat, pixelFormat)
			assert.Equal(t, tt.wantRB, rb)
		})
	}
}
