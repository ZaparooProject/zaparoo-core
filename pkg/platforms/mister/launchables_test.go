//go:build linux

package mister

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchablesOtherCoreDefinitions(t *testing.T) {
	items := (&Platform{}).Launchables(&config.Instance{})

	require.Len(t, items, 32)
	seenSystems := make(map[string]string, len(items))
	seenMedia := make(map[string]launchables.VirtualMedia, len(items))
	for _, item := range items {
		switch entry := item.(type) {
		case launchables.VirtualSystem:
			assert.NotEmpty(t, entry.Category)
			assert.NotNil(t, entry.Launch)
			assert.NotNil(t, entry.Test)
			assert.NotEmpty(t, entry.ZapScript())
			seenSystems[entry.Name] = entry.Category
		case launchables.VirtualMedia:
			assert.NotNil(t, entry.Launch)
			assert.NotNil(t, entry.Test)
			assert.NotEmpty(t, entry.ZapScript())
			seenMedia[entry.Name] = entry
		default:
			t.Fatalf("unexpected launchable type %T", item)
		}
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
		assert.Equal(t, "Other", seenSystems[name], name)
	}

	for _, name := range []string{
		"AY-3-8500",
		"BBC Bridge Companion",
		"My Vision",
		"Super Vision 8000",
	} {
		assert.Equal(t, "Console", seenSystems[name], name)
	}

	for _, name := range []string{
		"Altair 8800",
		"Archie",
		"Atari ST",
		"Commodore 128",
		"CoCo 3",
		"Coleco Adam",
		"EG2000 Colour Genie",
		"Enterprise",
		"Homelab",
		"IQ-151",
		"Mac LC",
		"Ondra SPO186",
		"PC-88",
		"PCjr",
		"Sharp MZ",
		"TK2000",
		"Tandy 1000",
		"VT52",
	} {
		assert.Equal(t, "Computer", seenSystems[name], name)
	}

	thirdStrike, ok := seenMedia["Street Fighter III: 3rd Strike (3S-ARM)"]
	require.True(t, ok, "3S-ARM virtual media missing")
	assert.Equal(t, systemdefs.SystemArcade, thirdStrike.SystemID)
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

func TestCoreExists(t *testing.T) {
	rootDir := t.TempDir()
	otherDir := filepath.Join(rootDir, "_Other")
	consoleDir := filepath.Join(rootDir, "_Console")
	computerDir := filepath.Join(rootDir, "_Computer")
	require.NoError(t, os.MkdirAll(otherDir, 0o750))
	require.NoError(t, os.MkdirAll(consoleDir, 0o750))
	require.NoError(t, os.MkdirAll(computerDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Chess_20240410.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "3S-ARM.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(consoleDir, "AY-3-8500_20250903.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(computerDir, "VT52_20241120.RBF"), nil, 0o600))

	assert.True(t, otherCoreExists(rootDir, "Chess"))
	assert.True(t, otherCoreExists(rootDir, "3S-ARM"))
	assert.False(t, otherCoreExists(rootDir, "Donut"))
	assert.True(t, coreExists(rootDir, filepath.Join("_Console", "AY-3-8500")))
	assert.True(t, coreExists(rootDir, filepath.Join("_Computer", "VT52")))
	assert.False(t, coreExists(rootDir, filepath.Join("_Computer", "Altair8800")))
}
