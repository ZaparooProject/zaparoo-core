//go:build linux

package mister

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchablesOtherCoreDefinitions(t *testing.T) {
	items := (&Platform{}).Launchables(&config.Instance{})

	require.Len(t, items, 9)
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		system, ok := item.(launchables.VirtualSystem)
		require.True(t, ok)
		assert.Equal(t, "Other", system.Category)
		assert.NotNil(t, system.Launch)
		assert.NotNil(t, system.Test)
		assert.NotEmpty(t, system.ZapScript())
		seen[system.Name] = true
	}

	for _, name := range []string{
		"Chess",
		"Donut",
		"Epoch Galaxy II",
		"Flappy Bird",
		"Game of Life",
		"GBMidi",
		"GenMidi",
		"Slug Cross",
		"Tomy Scramble",
	} {
		assert.True(t, seen[name], name)
	}
}

func TestOtherCoreExists(t *testing.T) {
	rootDir := t.TempDir()
	otherDir := filepath.Join(rootDir, "_Other")
	require.NoError(t, os.MkdirAll(otherDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Chess_20240410.rbf"), nil, 0o600))

	assert.True(t, otherCoreExists(rootDir, "Chess"))
	assert.False(t, otherCoreExists(rootDir, "Donut"))
}
