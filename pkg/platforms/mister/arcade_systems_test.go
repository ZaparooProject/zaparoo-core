//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArcadeSetSystemsMapsCuratedPlatforms(t *testing.T) {
	t.Parallel()

	setSystems := arcadeSetSystems([]arcadedb.ArcadeDbEntry{
		{Setname: "CPS1GAME", Platform: "Capcom CPS-1"},
		{Setname: "cps15game", Platform: "Capcom CPS-1.5"},
		{Setname: "cps2game", Platform: "Capcom CPS-2"},
		{Setname: "CPS3GAME", Platform: "Capcom CPS-3"},
		{Setname: "m72game", Platform: "Irem M72"},
		{Setname: "m92game", Platform: "Irem M92"},
		{Setname: "jalecogame", Platform: "Jaleco Mega System 1"},
		{Setname: "namcogame", Platform: "Namco System-1"},
		{Setname: "pgmgame", Platform: "IGS PGM"},
		{Setname: "stvgame", Platform: "Sega ST-V"},
		{Setname: "system16game", Platform: "Sega System 16"},
		{Setname: "system18game", Platform: "Sega System 18"},
		{Setname: "taitogame", Platform: "Taito F2 System"},
		{Setname: "unknown", Platform: "Unique hardware"},
	})

	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps1game"])
	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps15game"])
	assert.Equal(t, systemdefs.SystemCPS2, setSystems["cps2game"])
	assert.Equal(t, systemdefs.SystemCPS3, setSystems["cps3game"])
	assert.Equal(t, systemdefs.SystemIremM72, setSystems["m72game"])
	assert.Equal(t, systemdefs.SystemIremM92, setSystems["m92game"])
	assert.Equal(t, systemdefs.SystemJalecoMegaSystem1, setSystems["jalecogame"])
	assert.Equal(t, systemdefs.SystemNamcoSystem1, setSystems["namcogame"])
	assert.Equal(t, systemdefs.SystemPGM, setSystems["pgmgame"])
	assert.Equal(t, systemdefs.SystemSegaSTV, setSystems["stvgame"])
	assert.Equal(t, systemdefs.SystemSegaSystem16, setSystems["system16game"])
	assert.Equal(t, systemdefs.SystemSegaSystem18, setSystems["system18game"])
	assert.Equal(t, systemdefs.SystemTaitoF2, setSystems["taitogame"])
	assert.NotContains(t, setSystems, "unknown")
}

func TestArcadeSystemCacheClassifiesProvidedMRAFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	classifiedPath := filepath.Join(dir, "game.mra")
	require.NoError(t, os.WriteFile(classifiedPath, []byte(
		"<misterromdescription><setname>CPS1GAME</setname></misterromdescription>",
	), 0o600))
	malformedPath := filepath.Join(dir, "malformed.mra")
	require.NoError(t, os.WriteFile(malformedPath, []byte("<invalid>"), 0o600))
	mglPath := filepath.Join(dir, "shortcut.mgl")
	require.NoError(t, os.WriteFile(mglPath, []byte("ignored"), 0o600))

	cache := newArcadeSystemCache(NewPlatform())
	readCalls := 0
	cache.readArcadeDB = func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error) {
		readCalls++
		return []arcadedb.ArcadeDbEntry{{Setname: "cps1game", Platform: "Capcom CPS-1"}}, nil
	}
	input := []platforms.ScanResult{
		{Path: classifiedPath, Name: "Classified"},
		{Path: malformedPath},
		{Path: mglPath},
	}
	inputBefore := append([]platforms.ScanResult(nil), input...)

	unchanged, err := cache.captureScanner(context.Background(), &config.Instance{}, systemdefs.SystemArcade, input)
	require.NoError(t, err)
	assert.Equal(t, inputBefore, unchanged)

	results, err := cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	require.Equal(t, []platforms.ScanResult{{Path: classifiedPath, Name: "Classified"}}, results)
	assert.Equal(t, 1, readCalls)

	results[0].Path = "mutated"
	results, err = cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, classifiedPath, results[0].Path)
}

func TestArcadeSystemCacheRetriesAfterCancelledScan(t *testing.T) {
	t.Parallel()

	cache := newArcadeSystemCache(NewPlatform())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cache.scanFiles(ctx, &config.Instance{})
	require.ErrorIs(t, err, context.Canceled)

	calls := 0
	cache.scanArcadeFiles = func(context.Context, *config.Instance) ([]platforms.ScanResult, error) {
		calls++
		if calls == 1 {
			return []platforms.ScanResult{{Path: "partial.mra"}}, context.Canceled
		}
		return nil, nil
	}

	_, err = cache.captureScanner(context.Background(), &config.Instance{}, systemdefs.SystemArcade, nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, cache.loaded)
	assert.Empty(t, cache.results)

	_, err = cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestArcadeSystemCacheScanFilesFiltersSupportedExtensions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	arcadeDir := filepath.Join(root, "_Arcade", "subdirectory")
	require.NoError(t, os.MkdirAll(arcadeDir, 0o750))
	mraPath := filepath.Join(arcadeDir, "game.mra")
	mglPath := filepath.Join(arcadeDir, "shortcut.MGL")
	txtPath := filepath.Join(arcadeDir, "notes.txt")
	for _, path := range []string{mraPath, mglPath, txtPath} {
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))
	}

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[launchers]\nindex_root = [%q]\n", root)))
	cache := newArcadeSystemCache(NewPlatform())

	results, err := cache.scanFiles(context.Background(), cfg)
	require.NoError(t, err)
	assert.ElementsMatch(t, []platforms.ScanResult{{Path: mraPath}, {Path: mglPath}}, results)
}

func TestAddNeoGeoMVSLauncherSharesScannerCache(t *testing.T) {
	t.Parallel()

	t.Run("successful scan", func(t *testing.T) {
		t.Parallel()

		expected := []platforms.ScanResult{{Path: "mslug.neo", Name: "Metal Slug"}}
		calls := 0
		neoGeo := platforms.Launcher{Scanner: func(
			context.Context, *config.Instance, string, []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			calls++
			return expected, nil
		}}

		updated, mvs := addNeoGeoMVSLauncher(NewPlatform(), &neoGeo)
		results, err := updated.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeo, nil)
		require.NoError(t, err)
		assert.Equal(t, expected, results)

		results, err = mvs.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeoMVS, nil)
		require.NoError(t, err)
		assert.Equal(t, expected, results)
		assert.Equal(t, 1, calls)
	})

	t.Run("MVS scan populates shared cache", func(t *testing.T) {
		t.Parallel()

		expected := []platforms.ScanResult{{Path: "kof98.neo", Name: "The King of Fighters '98"}}
		calls := 0
		neoGeo := platforms.Launcher{Scanner: func(
			context.Context, *config.Instance, string, []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			calls++
			return expected, nil
		}}

		updated, mvs := addNeoGeoMVSLauncher(NewPlatform(), &neoGeo)
		results, err := mvs.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeoMVS, nil)
		require.NoError(t, err)
		assert.Equal(t, expected, results)
		assert.Equal(t, 1, calls)

		results, err = updated.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeo, nil)
		require.NoError(t, err)
		assert.Equal(t, expected, results)
		assert.Equal(t, 2, calls)

		results, err = mvs.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeoMVS, nil)
		require.NoError(t, err)
		assert.Equal(t, expected, results)
		assert.Equal(t, 2, calls)
	})

	t.Run("scanner error is not cached", func(t *testing.T) {
		t.Parallel()

		scanErr := errors.New("scan failed")
		calls := 0
		neoGeo := platforms.Launcher{Scanner: func(
			context.Context, *config.Instance, string, []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			calls++
			return nil, scanErr
		}}

		updated, mvs := addNeoGeoMVSLauncher(NewPlatform(), &neoGeo)
		_, err := updated.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeo, nil)
		require.ErrorIs(t, err, scanErr)
		_, err = mvs.Scanner(context.Background(), &config.Instance{}, systemdefs.SystemNeoGeoMVS, nil)
		require.ErrorIs(t, err, scanErr)
		assert.Equal(t, 2, calls)
	})
}

func TestNeoGeoMVSLaunchOptions(t *testing.T) {
	t.Parallel()

	defaults := neoGeoMVSLaunchOptions(nil)
	assert.Equal(t, systemdefs.SystemNeoGeoMVS, defaults.SetName)
	assert.Equal(t, "true", defaults.SetNameSameDir)

	explicit := neoGeoMVSLaunchOptions(&platforms.LaunchOptions{
		SetName:        "CustomMVS",
		SetNameSameDir: "false",
		Action:         "details",
	})
	assert.Equal(t, "CustomMVS", explicit.SetName)
	assert.Equal(t, "false", explicit.SetNameSameDir)
	assert.Equal(t, "details", explicit.Action)
}

func TestArcadeSystemLaunchersPreserveArcadeAndAddGranularSystems(t *testing.T) {
	t.Parallel()

	platform := NewPlatform()
	launchers := addArcadeSystemLaunchers(platform, CreateLaunchers(platform))
	byID := make(map[string]platforms.Launcher, len(launchers))
	for i := range launchers {
		byID[launchers[i].ID] = launchers[i]
	}

	arcade, ok := byID[systemdefs.SystemArcade]
	require.True(t, ok)
	assert.False(t, arcade.SkipFilesystemScan)
	require.NotNil(t, arcade.Scanner)

	dir := t.TempDir()
	classifiedPath := filepath.Join(dir, "classified.mra")
	unclassifiedPath := filepath.Join(dir, "unclassified.mra")
	require.NoError(t, os.WriteFile(classifiedPath, []byte(
		"<misterromdescription><setname>1941</setname></misterromdescription>",
	), 0o600))
	require.NoError(t, os.WriteFile(unclassifiedPath, []byte(
		"<misterromdescription><setname>unknown</setname></misterromdescription>",
	), 0o600))
	arcadeInput := []platforms.ScanResult{{Path: classifiedPath}, {Path: unclassifiedPath}}
	arcadeResults, err := arcade.Scanner(
		context.Background(), &config.Instance{}, systemdefs.SystemArcade, arcadeInput,
	)
	require.NoError(t, err)
	assert.Equal(t, arcadeInput, arcadeResults)

	for _, spec := range misterArcadeSystemSpecs {
		launcher, found := byID[spec.systemID]
		require.True(t, found, spec.systemID)
		assert.True(t, launcher.SkipFilesystemScan, spec.systemID)
		assert.Equal(t, []string{"_Arcade"}, launcher.Folders, spec.systemID)
		assert.NotNil(t, launcher.Scanner, spec.systemID)
		assert.NotNil(t, launcher.Launch, spec.systemID)
	}
}
