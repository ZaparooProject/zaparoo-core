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
	"github.com/google/uuid"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchablesOtherCoreDefinitions(t *testing.T) {
	items := (&Platform{}).Launchables(&config.Instance{})

	require.Len(t, items, 37)
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
		"MiSTer Quake",
		"Sonic Mania",
		"Slug Cross",
		"Tamagotchi",
		"Tomy Scramble",
	} {
		assert.Equal(t, "Other", seenSystems[name], name)
	}

	for _, name := range []string{
		"AY-3-8500",
		"BBC Bridge Companion",
		"My Vision",
		"Super Vision 8000",
		"Load GB/GBC Cartridge",
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

	paprium, ok := seenMedia["Paprium"]
	require.True(t, ok, "Paprium virtual media missing")
	assert.Equal(t, launchables.MisterGenesisPaprium, paprium.ID)
	assert.Equal(t, systemdefs.SystemGenesis, paprium.SystemID)
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

func TestLaunchMGLFileUsesInjectedLauncher(t *testing.T) {
	mglPath := filepath.Join("_Custom Cores", "PapriumMD.mgl")
	closed := false
	var launched string
	p := &Platform{
		closeConsole: func() error {
			closed = true
			return nil
		},
		launchBasicFile: func(path string) error {
			launched = path
			return nil
		},
	}

	process, err := p.launchMGLFile(mglPath)(&config.Instance{}, "", nil)

	require.NoError(t, err)
	assert.Nil(t, process)
	assert.True(t, closed)
	assert.Equal(t, mglPath, launched)
}

func TestLaunchMGLFileReturnsInjectedLaunchError(t *testing.T) {
	mglPath := filepath.Join("_Custom Cores", "PapriumMD.mgl")
	launchErr := errors.New("launch failed")
	p := &Platform{
		closeConsole: func() error {
			return errors.New("close failed")
		},
		launchBasicFile: func(string) error {
			return launchErr
		},
	}

	process, err := p.launchMGLFile(mglPath)(&config.Instance{}, "", nil)

	require.ErrorIs(t, err, launchErr)
	assert.Nil(t, process)
}

func TestLaunchableFileAvailability(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("media", "fat")
	filePath := filepath.Join(root, "launch.mgl")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	require.NoError(t, afero.WriteFile(fs, filePath, nil, 0o600))
	platform := &Platform{fs: fs}

	assert.True(t, platform.testFile(filePath)(nil))
	assert.False(t, platform.testFile(root)(nil))
	assert.False(t, platform.testFile(filepath.Join(root, "missing.mgl"))(nil))
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
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Sonic_Mania_20260701.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Tamagotchi_20260515.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Quake.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(consoleDir, "AY-3-8500_20250903.rbf"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(computerDir, "VT52_20241120.RBF"), nil, 0o600))

	assert.True(t, otherCoreExists(rootDir, "Chess"))
	assert.True(t, otherCoreExists(rootDir, "3S-ARM"))
	assert.True(t, otherCoreExists(rootDir, "Sonic_Mania"))
	assert.True(t, otherCoreExists(rootDir, "Tamagotchi"))
	assert.True(t, otherCoreExists(rootDir, "Quake"))
	assert.False(t, otherCoreExists(rootDir, "Donut"))
	assert.True(t, coreExists(rootDir, filepath.Join("_Console", "AY-3-8500")))
	assert.True(t, coreExists(rootDir, filepath.Join("_Computer", "VT52")))
	assert.False(t, coreExists(rootDir, filepath.Join("_Computer", "Altair8800")))
}

func TestMergeOtherLaunchableDefinitions_AppendsNewEntry(t *testing.T) {
	merged := mergeOtherLaunchableDefinitions(
		misterOtherLaunchableDefinitions,
		[]config.LaunchersCustom{
			{
				ID: "MisterOtherArduboy", Kind: config.CustomLauncherKindVirtualSystem,
				Backend: config.CustomLauncherBackendMisterCore, Name: "Arduboy",
				Category: "Other", LoadPath: "_Other/Arduboy",
			},
		},
	)

	require.Len(t, merged, len(misterOtherLaunchableDefinitions)+1)
	added := merged[len(merged)-1]
	assert.Equal(t, "Arduboy", added.Name)
	assert.Equal(t, "Other", added.Category)
	assert.Equal(t, filepath.Join("_Other", "Arduboy"), added.LoadPath)
	assert.Equal(t, uuid.NewSHA1(
		launchables.ZaparooLaunchableNamespace,
		[]byte("mister_core:misterotherarduboy"),
	), added.ID)
}

func TestMergeOtherLaunchableDefinitions_OverridesBuiltinKeepsUUID(t *testing.T) {
	merged := mergeOtherLaunchableDefinitions(
		misterOtherLaunchableDefinitions,
		[]config.LaunchersCustom{
			{
				ID: "MisterOtherChess", Kind: config.CustomLauncherKindVirtualSystem,
				Backend: config.CustomLauncherBackendMisterCore, Name: "Chess Renamed",
				Category: "Console", LoadPath: "_Other/ChessAlt",
			},
		},
	)

	require.Len(t, merged, len(misterOtherLaunchableDefinitions))

	var chess *misterOtherLaunchableDefinition
	for i := range merged {
		if merged[i].ConfigID == "MisterOtherChess" {
			chess = &merged[i]
			break
		}
	}
	require.NotNil(t, chess, "chess entry missing from merged list")
	assert.Equal(t, "Chess Renamed", chess.Name)
	assert.Equal(t, "Console", chess.Category)
	assert.Equal(t, filepath.Join("_Other", "ChessAlt"), chess.LoadPath)
	assert.Equal(t, launchables.MisterOtherChess, chess.ID)
}

func TestLaunchables_IncludesUserConfiguredOtherLaunchable(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.custom]]
id = "MisterOtherArduboy"
kind = "virtual_system"
backend = "mister_core"
name = "Arduboy"
load_path = "_Other/Arduboy"
`))

	items := (&Platform{}).Launchables(cfg)

	require.Len(t, items, 38)

	var found *launchables.VirtualSystem
	for i := range items {
		if system, ok := items[i].(launchables.VirtualSystem); ok && system.Name == "Arduboy" {
			found = &system
			break
		}
	}
	require.NotNil(t, found, "Arduboy launchable missing")
	assert.Equal(t, "Other", found.Category)
	assert.Equal(t, uuid.NewSHA1(
		launchables.ZaparooLaunchableNamespace,
		[]byte("mister_core:misterotherarduboy"),
	), found.ID)
	assert.NotNil(t, found.Launch)
	assert.NotNil(t, found.Test)
}
