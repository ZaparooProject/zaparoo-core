//go:build linux

package mister

import (
	"context"
	"errors"
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
		{Setname: "CPS3GAME", Platform: "Capcom CPS-3"},
		{Setname: "pgmgame", Platform: "IGS PGM"},
		{Setname: "unknown", Platform: "Unique hardware"},
	})

	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps1game"])
	assert.Equal(t, systemdefs.SystemCPS1, setSystems["cps15game"])
	assert.Equal(t, systemdefs.SystemCPS3, setSystems["cps3game"])
	assert.Equal(t, systemdefs.SystemPGM, setSystems["pgmgame"])
	assert.NotContains(t, setSystems, "unknown")
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

func TestArcadeSystemLaunchersPreserveArcadeAndAddGranularSystems(t *testing.T) {
	t.Parallel()

	launchers := addArcadeSystemLaunchers(NewPlatform(), CreateLaunchers(NewPlatform()))
	byID := make(map[string]platforms.Launcher, len(launchers))
	for i := range launchers {
		byID[launchers[i].ID] = launchers[i]
	}

	arcade, ok := byID[systemdefs.SystemArcade]
	require.True(t, ok)
	assert.False(t, arcade.SkipFilesystemScan)
	assert.NotNil(t, arcade.Scanner)

	for _, spec := range misterArcadeSystemSpecs {
		launcher, found := byID[spec.systemID]
		require.True(t, found, spec.systemID)
		assert.True(t, launcher.SkipFilesystemScan, spec.systemID)
		assert.Equal(t, []string{"_Arcade"}, launcher.Folders, spec.systemID)
		assert.NotNil(t, launcher.Scanner, spec.systemID)
		assert.NotNil(t, launcher.Launch, spec.systemID)
	}
}
