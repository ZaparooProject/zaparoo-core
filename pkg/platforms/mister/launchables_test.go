//go:build linux

package mister

import (
	"errors"
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

func TestLaunchOtherCoreUsesInjectedLauncher(t *testing.T) {
	corePath := filepath.Join("_Other", "Chess")
	closed := false
	var launched string
	p := &Platform{
		closeConsole: func() error {
			closed = true
			return nil
		},
		launchShortCore: func(path string) error {
			launched = path
			return nil
		},
	}

	process, err := p.launchOtherCore(corePath)(&config.Instance{}, "", nil)

	require.NoError(t, err)
	assert.Nil(t, process)
	assert.True(t, closed)
	assert.Equal(t, corePath, launched)
}

func TestLaunchOtherCoreReturnsInjectedLaunchError(t *testing.T) {
	corePath := filepath.Join("_Other", "Chess")
	launchErr := errors.New("launch failed")
	closed := false
	p := &Platform{
		closeConsole: func() error {
			closed = true
			return errors.New("close failed")
		},
		launchShortCore: func(string) error {
			return launchErr
		},
	}

	process, err := p.launchOtherCore(corePath)(&config.Instance{}, "", nil)

	require.ErrorIs(t, err, launchErr)
	assert.Nil(t, process)
	assert.True(t, closed)
}

func TestOtherCoreExists(t *testing.T) {
	rootDir := t.TempDir()
	otherDir := filepath.Join(rootDir, "_Other")
	require.NoError(t, os.MkdirAll(otherDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Chess_20240410.rbf"), nil, 0o600))

	assert.True(t, otherCoreExists(rootDir, "Chess"))
	assert.False(t, otherCoreExists(rootDir, "Donut"))
}
