//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
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

// newTestArcadeSystemCache builds a cache with persistence pointed at a
// temp file so tests never touch the real data directory.
func newTestArcadeSystemCache(t *testing.T) *arcadeSystemCache {
	t.Helper()
	cache := newArcadeSystemCache(NewPlatform())
	cache.persistPath = filepath.Join(t.TempDir(), arcadeClassCacheFileName)
	return cache
}

func TestLoadArcadeClassCacheRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), arcadeClassCacheFileName)
	require.NoError(t, saveArcadeClassCache(path, map[string]arcadeClassCacheEntry{
		"oversized.mra": {SetName: "oversized", Size: 1, MtimeNs: 1},
	}))
	encodedInfo, err := os.Stat(path)
	require.NoError(t, err)

	previousMax := arcadeClassCacheMaxBytes
	arcadeClassCacheMaxBytes = encodedInfo.Size()
	t.Cleanup(func() { arcadeClassCacheMaxBytes = previousMax })

	paddingFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)
	t.Cleanup(func() { _ = paddingFile.Close() })
	_, err = paddingFile.Write(make([]byte, 64))
	require.NoError(t, err)
	require.NoError(t, paddingFile.Close())

	oversizedInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.Greater(t, oversizedInfo.Size(), arcadeClassCacheMaxBytes)
	assert.Empty(t, loadArcadeClassCache(path))
}

func TestArcadeSystemCacheEmptyScanPreservesPersistedCache(t *testing.T) {
	t.Parallel()

	cache := newTestArcadeSystemCache(t)
	persisted := map[string]arcadeClassCacheEntry{
		"existing.mra": {SetName: "existing", Size: 10, MtimeNs: 20},
	}
	require.NoError(t, saveArcadeClassCache(cache.persistPath, persisted))
	cache.readArcadeDB = func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error) {
		return nil, nil
	}

	_, err := cache.captureScanner(
		context.Background(), &config.Instance{}, systemdefs.SystemArcade, nil,
	)
	require.NoError(t, err)
	require.NoError(t, cache.classify(context.Background(), &config.Instance{}))
	assert.Equal(t, persisted, loadArcadeClassCache(cache.persistPath))
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

	cache := newTestArcadeSystemCache(t)
	readCalls := 0
	cache.readArcadeDB = func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error) {
		readCalls++
		return []arcadedb.ArcadeDbEntry{{Setname: "cps1game", Platform: "Capcom CPS-1"}}, nil
	}
	mraReads := 0
	baseReadMRA := cache.readMRA
	cache.readMRA = func(path string) (mgls.MRA, error) {
		mraReads++
		return baseReadMRA(path)
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
	assert.Zero(t, mraReads, "capture must not read MRA contents")
	assert.Zero(t, readCalls, "capture must not read the arcade DB")

	results, err := cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	require.Equal(t, []platforms.ScanResult{{Path: classifiedPath, Name: "Classified"}}, results)
	assert.Equal(t, 1, readCalls)
	assert.Equal(t, 2, mraReads, "both .mra files parsed once, .mgl skipped")

	results[0].Path = "mutated"
	results, err = cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, classifiedPath, results[0].Path)
	assert.Equal(t, 2, mraReads, "second demand serves from memory")
}

func TestArcadeSystemCachePersistedCacheSkipsUnchangedMRAReads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cps1Path := filepath.Join(dir, "game.mra")
	require.NoError(t, os.WriteFile(cps1Path, []byte(
		"<misterromdescription><setname>CPS1GAME</setname></misterromdescription>",
	), 0o600))
	malformedPath := filepath.Join(dir, "malformed.mra")
	require.NoError(t, os.WriteFile(malformedPath, []byte("<invalid>"), 0o600))
	persistPath := filepath.Join(t.TempDir(), arcadeClassCacheFileName)
	input := []platforms.ScanResult{{Path: cps1Path}, {Path: malformedPath}}
	arcadeEntries := []arcadedb.ArcadeDbEntry{{Setname: "cps1game", Platform: "Capcom CPS-1"}}

	makeCache := func(mraReads *int) *arcadeSystemCache {
		cache := newArcadeSystemCache(NewPlatform())
		cache.persistPath = persistPath
		cache.readArcadeDB = func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error) {
			return arcadeEntries, nil
		}
		baseReadMRA := cache.readMRA
		cache.readMRA = func(path string) (mgls.MRA, error) {
			*mraReads++
			return baseReadMRA(path)
		}
		return cache
	}
	classify := func(cache *arcadeSystemCache) []platforms.ScanResult {
		_, err := cache.captureScanner(context.Background(), &config.Instance{}, systemdefs.SystemArcade, input)
		require.NoError(t, err)
		results, err := cache.scanner(systemdefs.SystemCPS1)(
			context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
		)
		require.NoError(t, err)
		return results
	}

	firstReads := 0
	first := classify(makeCache(&firstReads))
	assert.Equal(t, []platforms.ScanResult{{Path: cps1Path}}, first)
	assert.Equal(t, 2, firstReads, "cold cache parses every MRA, including the malformed one")

	// A fresh cache instance (new process) with the persisted file must not
	// re-read unchanged MRAs — including the one that failed to parse.
	secondReads := 0
	second := classify(makeCache(&secondReads))
	assert.Equal(t, first, second)
	assert.Zero(t, secondReads)

	// Rewriting a file with new content (and mtime) invalidates its entry.
	require.NoError(t, os.WriteFile(cps1Path, []byte(
		"<misterromdescription><setname>other</setname></misterromdescription>",
	), 0o600))
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(cps1Path, future, future))
	thirdReads := 0
	third := classify(makeCache(&thirdReads))
	assert.Empty(t, third, "setname no longer maps to CPS1")
	assert.Equal(t, 1, thirdReads, "only the changed file is re-read")

	// A corrupt persisted cache falls back to a full parse.
	require.NoError(t, os.WriteFile(persistPath, []byte("not a gob"), 0o600))
	fourthReads := 0
	classify(makeCache(&fourthReads))
	assert.Equal(t, 2, fourthReads)
}

func TestArcadeSystemCacheRetriesAfterCancelledScan(t *testing.T) {
	t.Parallel()

	cache := newTestArcadeSystemCache(t)
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

	// No capture happened (Arcade wasn't scanned), so classification walks the
	// filesystem itself; a cancelled walk must not be cached as a result.
	_, err = cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, cache.loaded)
	assert.Empty(t, cache.results)

	_, err = cache.scanner(systemdefs.SystemCPS1)(
		context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestArcadeSystemCacheRecaptureInvalidatesClassification(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.mra")
	require.NoError(t, os.WriteFile(firstPath, []byte(
		"<misterromdescription><setname>CPS1GAME</setname></misterromdescription>",
	), 0o600))
	secondPath := filepath.Join(dir, "second.mra")
	require.NoError(t, os.WriteFile(secondPath, []byte(
		"<misterromdescription><setname>CPS1GAME</setname></misterromdescription>",
	), 0o600))

	cache := newTestArcadeSystemCache(t)
	cache.readArcadeDB = func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error) {
		return []arcadedb.ArcadeDbEntry{{Setname: "cps1game", Platform: "Capcom CPS-1"}}, nil
	}

	scan := cache.scanner(systemdefs.SystemCPS1)
	_, err := cache.captureScanner(context.Background(), &config.Instance{}, systemdefs.SystemArcade,
		[]platforms.ScanResult{{Path: firstPath}})
	require.NoError(t, err)
	results, err := scan(context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil)
	require.NoError(t, err)
	assert.Equal(t, []platforms.ScanResult{{Path: firstPath}}, results)

	// A new Arcade walk replaces the captured list; classification must follow.
	_, err = cache.captureScanner(context.Background(), &config.Instance{}, systemdefs.SystemArcade,
		[]platforms.ScanResult{{Path: secondPath}})
	require.NoError(t, err)
	results, err = scan(context.Background(), &config.Instance{}, systemdefs.SystemCPS1, nil)
	require.NoError(t, err)
	assert.Equal(t, []platforms.ScanResult{{Path: secondPath}}, results)
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
