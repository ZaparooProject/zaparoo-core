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

	// All files fit well under maxFilesPerTransaction, so there must be exactly one
	// batch-boundary commit and no mid-system (file-limit) commits — proving the six
	// systems shared a single transaction rather than committing per system.
	boundaryCommits := strings.Count(output, `"message":"committing media indexing batch"`)
	fileLimitCommits := strings.Count(output, "committed batch (file limit)")
	assert.Equal(t, 1, boundaryCommits,
		"six small systems should share exactly one batch commit (got log:\n%s)", output)
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

// TestNewNamesIndex_LargeSystemCommitsMidBatch verifies that a single system
// exceeding maxFilesPerTransaction still triggers an intermediate (memory-safety)
// commit and indexes every file correctly.
func TestNewNamesIndex_LargeSystemCommitsMidBatch(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	fileCount := maxFilesPerTransaction + 5
	systemFiles := map[string][]string{
		systemdefs.SystemNES: genFileNames("game", fileCount),
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

	assert.Equal(t, fileCount, filesIndexed, "every file in the oversized system should be indexed")

	media, mErr := db.MediaDB.GetMediaBySystemID(systemdefs.SystemNES)
	require.NoError(t, mErr)
	assert.Len(t, media, fileCount, "all media rows should be present")

	fileLimitCommits := strings.Count(output, "committed batch (file limit)")
	assert.GreaterOrEqual(t, fileLimitCommits, 1,
		"a system larger than maxFilesPerTransaction must commit mid-system at least once")
}
