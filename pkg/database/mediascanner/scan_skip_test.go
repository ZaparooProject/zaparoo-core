// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package mediascanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type scanSkipFixture struct {
	platform platforms.Platform
	cfg      *config.Instance
	db       *database.Database
	dirs     map[string]string
	systems  []systemdefs.System
}

func (f *scanSkipFixture) index(t *testing.T) {
	t.Helper()
	_, err := NewNamesIndex(context.Background(), f.platform, f.cfg, f.systems, f.db, func(IndexStatus) {}, nil)
	require.NoError(t, err)
}

func (f *scanSkipFixture) titleNames(t *testing.T, systemID string) []string {
	t.Helper()
	rows, err := f.db.MediaDB.UnsafeGetSQLDb().QueryContext(context.Background(), `
		SELECT mt.Name FROM MediaTitles mt JOIN Systems s ON s.DBID = mt.SystemDBID
		WHERE s.SystemID = ? ORDER BY mt.Name`, systemID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func (f *scanSkipFixture) breakTitles(t *testing.T, systemID string) {
	t.Helper()
	_, err := f.db.MediaDB.UnsafeGetSQLDb().ExecContext(context.Background(), `
		UPDATE MediaTitles SET Name = 'Edited Behind The Scanner'
		WHERE SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)`, systemID)
	require.NoError(t, err)
}

func newScanSkipFixture(t *testing.T) *scanSkipFixture {
	t.Helper()
	return newScanSkipFixtureFor(t, ".bin", systemdefs.SystemNES, systemdefs.SystemSNES)
}

// newScanSkipFixtureFor indexes one "Alpha Game" file with the given
// extension in each of the given systems.
func newScanSkipFixtureFor(t *testing.T, ext string, systemIDs ...string) *scanSkipFixture {
	t.Helper()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)

	fixture := &scanSkipFixture{db: db, dirs: map[string]string{}}
	launchers := make([]platforms.Launcher, 0, len(systemIDs))
	for _, systemID := range systemIDs {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Alpha Game"+ext), []byte("x"), 0o600))
		fixture.dirs[systemID] = dir
		launchers = append(launchers, platforms.Launcher{
			ID: "custom-" + systemID, SystemID: systemID, Folders: []string{dir}, Extensions: []string{ext},
		})
		fixture.systems = append(fixture.systems, systemdefs.System{ID: systemID})
	}
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fixture.cfg = cfg

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)
	fixture.platform = platform

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	})
	return fixture
}

// A system whose files are as its last index found them is left alone, which
// is observable as an edit behind the scanner's back surviving the reindex.
// A system that did change is reconciled as before, in the same run.
func TestScanSkip_UnchangedSystemIsLeftAloneAndChangedOneIsReconciled(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	fixture := newScanSkipFixture(t)
	fixture.index(t)
	require.Equal(t, []string{"Alpha Game"}, fixture.titleNames(t, systemdefs.SystemNES))

	fixture.breakTitles(t, systemdefs.SystemNES)
	fixture.breakTitles(t, systemdefs.SystemSNES)
	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.dirs[systemdefs.SystemSNES], "Beta Game.bin"), []byte("x"), 0o600))
	fixture.index(t)

	assert.Equal(t, []string{"Edited Behind The Scanner"}, fixture.titleNames(t, systemdefs.SystemNES),
		"an unchanged system must not be reconciled")
	assert.Equal(t, []string{"Alpha Game", "Beta Game"}, fixture.titleNames(t, systemdefs.SystemSNES),
		"a system with a new file must be reconciled in full")

	// Forgetting the record is what an outside writer does to ask for a
	// reconcile; the next index then repairs the system.
	require.NoError(t, fixture.db.MediaDB.SetScanMarker(context.Background(), systemdefs.SystemNES, ""))
	fixture.index(t)
	assert.Equal(t, []string{"Alpha Game"}, fixture.titleNames(t, systemdefs.SystemNES))
}

// A change to the parsing rules reconciles systems whose files did not
// change, and a Core upgrade that leaves the rules alone does not.
func TestScanSkip_RulesVersionChangeReconcilesUnchangedSystems(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache, config.AppVersion and scanRulesVersion.
	fixture := newScanSkipFixture(t)
	fixture.index(t)
	fixture.breakTitles(t, systemdefs.SystemNES)

	originalApp := config.AppVersion
	config.AppVersion = originalApp + "-next"
	t.Cleanup(func() { config.AppVersion = originalApp })
	fixture.index(t)
	assert.Equal(t, []string{"Edited Behind The Scanner"}, fixture.titleNames(t, systemdefs.SystemNES),
		"a Core upgrade with the same parsing rules must not reconcile an unchanged system")

	originalRules := scanRulesVersion
	scanRulesVersion = originalRules + 1
	t.Cleanup(func() { scanRulesVersion = originalRules })
	fixture.index(t)
	assert.Equal(t, []string{"Alpha Game"}, fixture.titleNames(t, systemdefs.SystemNES))
}

func TestScanWalkMarker_CoversEverythingAnEntryIsStagedFrom(t *testing.T) {
	t.Parallel()
	base := []platforms.ScanResult{
		{Path: "/games/nes/a.nes", Name: "A"},
		{Path: "/games/nes/b.nes"},
	}
	marker, ok := scanWalkMarker(nil, base)
	require.True(t, ok)

	reordered, ok := scanWalkMarker(nil, []platforms.ScanResult{base[1], base[0]})
	require.True(t, ok)
	assert.Equal(t, marker, reordered, "the order scanners report entries in does not matter")

	changes := map[string][]platforms.ScanResult{
		"file added":     {base[0], base[1], {Path: "/games/nes/c.nes"}},
		"file removed":   base[:1],
		"name changed":   {{Path: "/games/nes/a.nes", Name: "Renamed"}, base[1]},
		"no-ext changed": {{Path: "/games/nes/a.nes", Name: "A", NoExt: true}, base[1]},
		"source added": {
			{Path: "/games/nes/a.nes", Name: "A", Source: &platforms.MediaSource{}}, base[1],
		},
	}
	for name, files := range changes {
		changed, changedOK := scanWalkMarker(nil, files)
		require.True(t, changedOK, name)
		assert.NotEqual(t, marker, changed, name)
	}

	_, ok = scanWalkMarker(nil, []platforms.ScanResult{
		{Path: "/games/arcade/alias.mra", LinkTarget: "/games/arcade/real.mra"},
	})
	assert.False(t, ok, "a system with linked media is always reconciled")
}

// scannerOwnedWrite matches SQL that writes a table whose rows a reconcile
// owns outright.
var scannerOwnedWrite = regexp.MustCompile(
	`(?i:UPDATE\s+(OR\s+\w+\s+)?|INSERT\s+(OR\s+\w+\s+)?INTO\s+|DELETE\s+FROM\s+)` +
		`(Media|MediaTitles|MediaSources|MediaLinks)\b`)

// The scanner leaves a system alone when its files have not changed, which is
// only sound while nothing else rewrites the rows a reconcile owns. This test
// lists every file that writes those tables. A new entry means a new writer:
// it must either go through the scanner, or forget the scan record of the
// systems it touches (mediadb.SetScanMarker with an empty marker), before it
// is added here.
func TestScanSkip_ScannerOwnedTablesHaveKnownWriters(t *testing.T) {
	t.Parallel()
	known := []string{
		// Generic multi-row inserter; the scanner is its only caller for these tables.
		filepath.Join("database", "mediadb", "batch_inserter.go"),
		// Single-row insert kept for tests and tools; nothing in the service calls it.
		filepath.Join("database", "mediadb", "sql_media.go"),
		// Truncate and orphan cleanup: truncate forgets the scan records, and
		// orphan cleanup removes only rows already flagged missing.
		filepath.Join("database", "mediadb", "sql_maintenance.go"),
		// Disambiguation recompute writes a column derived from the others.
		filepath.Join("database", "mediadb", "sql_media_titles.go"),
		// One-off repair that fills an empty ParentDir with what a reconcile would write.
		filepath.Join("database", "mediadb", "sql_parent_dir_repair.go"),
		// The reconcile itself.
		filepath.Join("database", "mediadb", "sql_scan_reconcile.go"),
	}
	sort.Strings(known)

	root := filepath.Join("..", "..")
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // walks this repository's own sources
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		if scannerOwnedWrite.Match(content) {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return fmt.Errorf("relative path of %s: %w", path, relErr)
			}
			found = append(found, relative)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(found)
	assert.Equal(t, known, found)
}

// An index that changes nothing, on a database whose caches are all in place,
// leaves them alone: the index generation, which every persisted cache and
// the library sync inventory are keyed to, does not move. An index that does
// change something moves it as before.
func TestScanSkip_UnchangedIndexKeepsCachesAndGeneration(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	fixture := newScanSkipFixture(t)
	ctx := context.Background()
	fixture.index(t)

	// Background optimization is what completes after a real index.
	require.NoError(t, fixture.db.MediaDB.PopulateBrowseCache(ctx))
	require.NoError(t, fixture.db.MediaDB.RebuildSlugSearchCache())
	require.NoError(t, fixture.db.MediaDB.RebuildTagCache())
	require.NoError(t, fixture.db.MediaDB.SetOptimizationStatus(mediadb.IndexingStatusCompleted))
	concrete, ok := fixture.db.MediaDB.(*mediadb.MediaDB)
	require.True(t, ok)
	require.Empty(t, concrete.IndexCachesMissing(ctx), "fixture must start with intact caches")
	before, err := fixture.db.MediaDB.IndexGeneration()
	require.NoError(t, err)

	fixture.index(t)
	after, err := fixture.db.MediaDB.IndexGeneration()
	require.NoError(t, err)
	assert.Equal(t, before, after, "an unchanged index must not invalidate what was built from it")
	status, err := fixture.db.MediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusCompleted, status)

	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.dirs[systemdefs.SystemSNES], "Gamma Game.bin"), []byte("x"), 0o600))
	fixture.index(t)
	changed, err := fixture.db.MediaDB.IndexGeneration()
	require.NoError(t, err)
	assert.NotEqual(t, before, changed, "an index that added media must rebuild")
}
