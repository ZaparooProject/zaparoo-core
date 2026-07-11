//go:build linux

package mistermain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFramebufferFormat(t *testing.T) {
	t.Parallel()

	pixelFormat, rb, err := parseFramebufferFormat(VideoModeFormatRGB16)
	require.NoError(t, err)
	assert.Equal(t, "565", pixelFormat)
	assert.Equal(t, 1, rb)
}

func TestParseFramebufferFormatRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	_, _, err := parseFramebufferFormat("rgb16")
	require.Error(t, err)
}
