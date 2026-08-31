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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// syncLogBuffer is a goroutine-safe buffer for capturing zerolog output, since
// background optimization may keep logging after the indexing loop returns.
type syncLogBuffer struct {
	buf bytes.Buffer
	mu  syncutil.Mutex
}

func (s *syncLogBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write to log buffer: %w", err)
	}
	return n, nil
}

func (s *syncLogBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// setupCustomLauncherSystems writes the given files into a temp folder per
// system and wires up a mock platform with one custom launcher per system. It
// returns the platform and config ready to pass to NewNamesIndex. The caller is
// responsible for restoring GlobalLauncherCache (handled here via t.Cleanup).
func setupCustomLauncherSystems(
	t *testing.T, systemFiles map[string][]string,
) (platforms.Platform, *config.Instance, []systemdefs.System) {
	t.Helper()

	fsHelper := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fsHelper, t.TempDir())
	require.NoError(t, err)

	launchers := make([]platforms.Launcher, 0, len(systemFiles))
	systems := make([]systemdefs.System, 0, len(systemFiles))
	for systemID, files := range systemFiles {
		dir := t.TempDir()
		for _, name := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
		}
		launchers = append(launchers, platforms.Launcher{
			ID:         "custom-" + systemID,
			SystemID:   systemID,
			Folders:    []string{dir},
			Extensions: []string{".bin"},
		})
		systems = append(systems, systemdefs.System{ID: systemID})
	}

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	})

	return platform, cfg, systems
}

func genFileNames(prefix string, n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("%s_%05d.bin", prefix, i)
	}
	return names
}

// TestNewNamesIndex_BatchesCommitsAcrossSmallSystems verifies that a run of many
// small systems shares a single transaction commit instead of committing once
// (or twice) per system. The fixed per-commit fsync+checkpoint cost dominates
// indexing on slow storage, so collapsing N systems into one commit is the win.
func TestNewNamesIndex_BatchesCommitsAcrossSmallSystems(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systemFiles := map[string][]string{
		systemdefs.SystemNES:          {"a.bin", "b.bin"},
		systemdefs.SystemSNES:         {"c.bin", "d.bin"},
		systemdefs.SystemGenesis:      {"e.bin", "f.bin"},
		systemdefs.SystemGameboy:      {"g.bin", "h.bin"},
		systemdefs.SystemGameboyColor: {"i.bin", "j.bin"},
		systemdefs.SystemGBA:          {"k.bin", "l.bin"},
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	buf := &syncLogBuffer{}
	prevLogger := log.Logger
	prevGlobal := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = prevLogger; zerolog.SetGlobalLevel(prevGlobal) }()

	filesIndexed, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	// Restore the logger before reading so background optimization can't race the read.
	log.Logger = prevLogger
	zerolog.SetGlobalLevel(prevGlobal)
	output := buf.String()

	assert.Equal(t, 12, filesIndexed, "all files across all systems should be indexed")

	for systemID, files := range systemFiles {
		media, mErr := db.MediaDB.GetMediaBySystemID(systemID)
		require.NoError(t, mErr)
		assert.Lenf(t, media, len(files), "system %s should have all its media", systemID)
	}

	// Each small system should commit at its own boundary. Cross-system batching
	// kept transactions open into the next system and matched MiSTer crash logs.
	boundaryCommits := strings.Count(output, `"message":"committing media indexing batch"`)
	fileLimitCommits := strings.Count(output, "committed batch (file limit)")
	assert.Equal(t, len(systemFiles), boundaryCommits,
		"small systems should commit at each system boundary (got log:\n%s)", output)
	assert.Equal(t, 0, fileLimitCommits, "no mid-system commits expected for small systems")
}

// TestNewNamesIndex_BatchedReindexIsIdempotent verifies that re-indexing the same
// media a second time (which exercises the persistent-state reload plus the
// missing-state/disambiguation work now merged into the shared transaction)
// produces identical results and does not duplicate or drop rows.
func TestNewNamesIndex_BatchedReindexIsIdempotent(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systemFiles := map[string][]string{
		systemdefs.SystemNES:     {"a.bin", "b.bin", "c.bin"},
		systemdefs.SystemSNES:    {"d.bin", "e.bin"},
		systemdefs.SystemGenesis: {"f.bin"},
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	first, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	assert.Equal(t, 6, first)

	second, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	assert.Equal(t, first, second, "re-indexing identical media must produce an identical count")

	for systemID, files := range systemFiles {
		media, mErr := db.MediaDB.GetMediaBySystemID(systemID)
		require.NoError(t, mErr)
		assert.Lenf(t, media, len(files), "system %s should still have exactly its media after reindex", systemID)
	}
}

// TestNewNamesIndex_BatchedMissingMediaMarkedOnReindex verifies that media which
// disappears between indexes is still flagged missing now that the missing-state
// reconciliation runs inside the shared batch transaction rather than its own.
func TestNewNamesIndex_BatchedMissingMediaMarkedOnReindex(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	gone := filepath.Join(dir, "gone.bin")
	require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(gone, []byte("x"), 0o600))

	launcher := platforms.Launcher{
		ID:         "custom-nes",
		SystemID:   systemdefs.SystemNES,
		Folders:    []string{dir},
		Extensions: []string{".bin"},
	}
	fsHelper := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fsHelper, t.TempDir())
	require.NoError(t, err)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{launcher})

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	systems := []systemdefs.System{{ID: systemdefs.SystemNES}}

	first, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, first)

	// The file vanishes before the second index, so its row must be flagged missing.
	require.NoError(t, os.Remove(gone))

	_, err = NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	// GetAllMedia does not select IsMissing, so read the flag directly.
	missingByBase := readMissingFlags(t, db)
	assert.True(t, missingByBase["gone.bin"], "the removed file's media row should be flagged missing")
	assert.False(t, missingByBase["keep.bin"], "the surviving file's media row should not be missing")
}

// TestNewNamesIndex_CommitsOnReconcileVolume verifies that a run of systems with no
// files on disk but missing-media reconcile writes commits before completion rather
// than carrying one unbounded transaction until the last system. This is the
// stress-DB workload: a library that has shrunk drastically, flipping many rows
// to missing per system.
func TestNewNamesIndex_CommitsOnReconcileVolume(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Build several systems each with a handful of files in their own dir so we can
	// remove the files before the second index.
	systemIDs := []string{
		systemdefs.SystemNES, systemdefs.SystemSNES, systemdefs.SystemGenesis,
		systemdefs.SystemGameboy, systemdefs.SystemGameboyColor, systemdefs.SystemGBA,
	}
	const filesPerSystem = 4
	dirs := make(map[string]string, len(systemIDs))
	launchers := make([]platforms.Launcher, 0, len(systemIDs))
	systems := make([]systemdefs.System, 0, len(systemIDs))
	for _, systemID := range systemIDs {
		dir := t.TempDir()
		dirs[systemID] = dir
		for _, name := range genFileNames("game", filesPerSystem) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
		}
		launchers = append(launchers, platforms.Launcher{
			ID:         "custom-" + systemID,
			SystemID:   systemID,
			Folders:    []string{dir},
			Extensions: []string{".bin"},
		})
		systems = append(systems, systemdefs.System{ID: systemID})
	}

	fsHelper := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fsHelper, t.TempDir())
	require.NoError(t, err)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	// First index creates all the rows.
	first, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	assert.Equal(t, len(systemIDs)*filesPerSystem, first)

	// Remove every file so the reindex flips all rows to missing with zero files on
	// disk — filesInBatch never moves, so only the reconcile-volume trigger (or the
	// final system) can force a commit.
	for _, dir := range dirs {
		for _, name := range genFileNames("game", filesPerSystem) {
			require.NoError(t, os.Remove(filepath.Join(dir, name)))
		}
	}

	// Lower the historical volume threshold; current production commits every
	// system boundary, but this keeps the regression setup near the old stress path.
	origThreshold := maxReconcileRowsPerTransaction
	maxReconcileRowsPerTransaction = filesPerSystem + 1
	defer func() { maxReconcileRowsPerTransaction = origThreshold }()

	buf := &syncLogBuffer{}
	prevLogger := log.Logger
	prevGlobal := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = prevLogger; zerolog.SetGlobalLevel(prevGlobal) }()

	_, err = NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	log.Logger = prevLogger
	zerolog.SetGlobalLevel(prevGlobal)
	output := buf.String()

	// With no files on disk, filesInBatch stays 0 the whole run. System-boundary
	// commits must still produce more than one batch-boundary commit.
	boundaryCommits := strings.Count(output, `"message":"committing media indexing batch"`)
	fileLimitCommits := strings.Count(output, "committed batch (file limit)")
	assert.Equal(t, 0, fileLimitCommits, "no file-limit commits expected when there are no files on disk")
	assert.Greaterf(t, boundaryCommits, 1,
		"system-boundary commits should prevent a single final commit (got log:\n%s)", output)

	// Every row must still be present and flagged missing after the reindex. Query
	// counts directly rather than via basename (filenames collide across systems).
	sqlDB := db.MediaDB.UnsafeGetSQLDb()
	require.NotNil(t, sqlDB)
	var total, missing int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*), COALESCE(SUM(IsMissing), 0) FROM Media").Scan(&total, &missing))
	assert.Equal(t, len(systemIDs)*filesPerSystem, total, "all media rows should still be present")
	assert.Equal(t, total, missing, "all media should be flagged missing after files removed")
}

// readMissingFlags returns IsMissing for every Media row keyed by path basename.
func readMissingFlags(t *testing.T, db *database.Database) map[string]bool {
	t.Helper()
	sqlDB := db.MediaDB.UnsafeGetSQLDb()
	require.NotNil(t, sqlDB)
	rows, err := sqlDB.QueryContext(context.Background(), "SELECT Path, IsMissing FROM Media")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	flags := make(map[string]bool)
	for rows.Next() {
		var path string
		var missing bool
		require.NoError(t, rows.Scan(&path, &missing))
		flags[filepath.Base(path)] = missing
	}
	require.NoError(t, rows.Err())
	return flags
}

// TestNewNamesIndex_LargeSystemCommitsMidBatch verifies that a single system
// exceeding maxFilesPerTransaction still triggers an intermediate (memory-safety)
// commit and indexes every file correctly. A small system is batched ahead of the
// large one (it sorts first: "Gameboy" < "NES"), so the mid-system commit fires
// while the small system is already finalized — exercising the path that marks
// previously-batched systems complete at a mid-system commit boundary.
func TestNewNamesIndex_LargeSystemCommitsMidBatch(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	fileCount := maxFilesPerTransaction + 5
	systemFiles := map[string][]string{
		systemdefs.SystemGameboy: {"small1.bin", "small2.bin"},
		systemdefs.SystemNES:     genFileNames("game", fileCount),
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	buf := &syncLogBuffer{}
	prevLogger := log.Logger
	prevGlobal := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = prevLogger; zerolog.SetGlobalLevel(prevGlobal) }()

	filesIndexed, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	log.Logger = prevLogger
	zerolog.SetGlobalLevel(prevGlobal)
	output := buf.String()

	assert.Equal(t, fileCount+2, filesIndexed, "every file in both systems should be indexed")

	media, mErr := db.MediaDB.GetMediaBySystemID(systemdefs.SystemNES)
	require.NoError(t, mErr)
	assert.Len(t, media, fileCount, "all media rows for the oversized system should be present")

	// The small system was batched ahead of the large one; it must be committed
	// (and queryable) even though the large system forced a mid-system commit.
	smallMedia, sErr := db.MediaDB.GetMediaBySystemID(systemdefs.SystemGameboy)
	require.NoError(t, sErr)
	assert.Len(t, smallMedia, 2, "the small system batched ahead should be fully indexed")

	fileLimitCommits := strings.Count(output, "committed batch (file limit)")
	assert.GreaterOrEqual(t, fileLimitCommits, 1,
		"a system larger than maxFilesPerTransaction must commit mid-system at least once")

	// The mid-batch (file limit) commit path must refresh mid-scan caches for the
	// systems it just finalized, same as a natural system-boundary commit. Without
	// that, IndexedSystems() (backing the `systems` API) misses systems that are
	// fully durable but happened to land on a file-limit flush.
	indexed, iErr := db.MediaDB.IndexedSystems()
	require.NoError(t, iErr)
	assert.Contains(t, indexed, systemdefs.SystemGameboy,
		"a system committed via the file-limit path must be visible via IndexedSystems() "+
			"without waiting for the end-of-run browse cache rebuild")
}
