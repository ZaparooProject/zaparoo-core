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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func nilScanner(
	context.Context, *config.Instance, string, []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	return nil, nil
}

// TestNewNamesIndex_ResumeReconcilesOnlyRemainingSystems verifies that on
// resume, systems sorted before the resume cursor are skipped entirely: only
// the interrupted system (and any after it) are staged and reconciled.
func TestNewNamesIndex_ResumeReconcilesOnlyRemainingSystems(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	launchers := []platforms.Launcher{
		{ID: "genesis-launcher", SystemID: "genesis", Scanner: nilScanner},
		{ID: "nes-launcher", SystemID: "nes", Scanner: nilScanner},
		{ID: "snes-launcher", SystemID: "snes", Scanner: nilScanner},
	}
	mockPlatform.On("Launchers", mock.Anything).Return(launchers)
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice(launchers)
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()

	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil).Once()
	mockMediaDB.On("CreateSecondaryIndexes").Return(nil).Once()
	mockMediaDB.On("PopulateSystemTagsCacheForSystems", mock.Anything, mock.Anything).Return(nil).Once()
	mockMediaDB.On("RefreshSlugSearchCacheForSystems", mock.Anything, mock.Anything).Return(nil).Once()
	mockMediaDB.On("SetOptimizationStatus", "pending").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Once()
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("GetLastIndexedSystem").Return("snes", nil).Once()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"genesis", "nes", "snes"}, nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string{"genesis", "nes", "snes"}).Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "snes").Return(nil).Maybe()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()
	mockMediaDB.On("GetAllSystems").Return([]database.System{
		{DBID: 1, SystemID: "genesis"},
		{DBID: 2, SystemID: "nes"},
		{DBID: 3, SystemID: "snes"},
	}, nil).Once()
	// Only the resume system is cleared and reconciled.
	mockMediaDB.On("ClearScanStage").Return(nil).Once()
	mockMediaDB.On("ReconcileStagedSystem", mock.Anything, "snes", mock.Anything).
		Return(database.ScanReconcileStats{SystemKnown: true}, nil).Once()

	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, []systemdefs.System{
		{ID: "genesis"},
		{ID: "nes"},
		{ID: "snes"},
	}, &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	mockMediaDB.AssertNotCalled(t, "ReconcileStagedSystem", mock.Anything, "genesis", mock.Anything)
	mockMediaDB.AssertNotCalled(t, "ReconcileStagedSystem", mock.Anything, "nes", mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_CancelledRunDoesNotReconcile verifies a cancelled context
// aborts before any system's staged rows are folded into the media tables.
func TestNewNamesIndex_CancelledRunDoesNotReconcile(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()

	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Maybe()
	mockMediaDB.On("GetLastIndexedSystem").Return("", nil).Maybe()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"snes"}, nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string{"snes"}).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "cancelled").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "failed").Return(nil).Maybe()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Maybe()
	mockMediaDB.On("GetAllSystems").Return([]database.System{{DBID: 1, SystemID: "snes"}}, nil).Maybe()

	_, err := NewNamesIndex(ctx, mockPlatform, cfg, []systemdefs.System{{ID: "snes"}},
		&database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.ErrorIs(t, err, context.Canceled)
	mockMediaDB.AssertNotCalled(t, "ReconcileStagedSystem", mock.Anything, mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_ResumeKeepsRequestedSystemsWhenSomeAreNotRunnable verifies
// that launch-only systems (no scanner, no paths, no DB rows) are filtered out
// without invalidating the resume state of the remaining systems.
func TestNewNamesIndex_ResumeKeepsRequestedSystemsWhenSomeAreNotRunnable(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launchers := []platforms.Launcher{
		{ID: "nes-scanner", SystemID: "nes", Scanner: nilScanner},
		{ID: "ps2-launch-only", SystemID: "ps2"},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return(launchers)
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice(launchers)
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()

	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil).Once()
	mockMediaDB.On("CreateSecondaryIndexes").Return(nil).Once()
	mockMediaDB.On("PopulateSystemTagsCacheForSystems", mock.Anything, mock.Anything).Return(nil).Once()
	mockMediaDB.On("RefreshSlugSearchCacheForSystems", mock.Anything, mock.Anything).Return(nil).Once()
	mockMediaDB.On("SetOptimizationStatus", "pending").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("GetLastIndexedSystem").Return("nes", nil).Once()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"nes", "ps2"}, nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string{"nes", "ps2"}).Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "nes").Return(nil).Maybe()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()
	mockMediaDB.On("GetAllSystems").Return([]database.System{{DBID: 1, SystemID: "nes"}}, nil).Once()
	mockMediaDB.On("ClearScanStage").Return(nil).Once()
	mockMediaDB.On("ReconcileStagedSystem", mock.Anything, "nes", mock.Anything).
		Return(database.ScanReconcileStats{SystemKnown: true}, nil).Once()

	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, []systemdefs.System{
		{ID: "nes"},
		{ID: "ps2"},
	}, &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	mockMediaDB.AssertNotCalled(t, "SetIndexingStatus", "")
	mockMediaDB.AssertNotCalled(t, "ReconcileStagedSystem", mock.Anything, "ps2", mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_StagingErrorAbortsIndexing verifies a hard staging failure
// (e.g. a batch flush hitting a constraint) aborts the run before completion
// steps rather than being swallowed.
func TestNewNamesIndex_StagingErrorAbortsIndexing(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{
		ID:                 "nes-scanner",
		SystemID:           "nes",
		SkipFilesystemScan: true,
		Scanner: func(
			context.Context, *config.Instance, string, []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return []platforms.ScanResult{{
				Path: filepath.Join(string(filepath.Separator), "roms", "nes", "Super Mario Bros.nes"),
			}}, nil
		},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{launcher})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{launcher})
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	depFlushErr := fmt.Errorf("failed to flush dependency for ScanStageTags: %w: %w",
		mediadb.ErrDependencyFlush,
		sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique},
	)

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("StageScannedMedia", mock.Anything).Return(depFlushErr).Once()
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "failed").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Once()
	mockMediaDB.On("ClearScanStage").Return(nil).Once()

	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, []systemdefs.System{{ID: "nes"}},
		&database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, mediadb.ErrDependencyFlush)
	assert.Contains(t, err.Error(), "unrecoverable error staging media path")
	mockMediaDB.AssertNotCalled(t, "UpdateLastGenerated")
	mockMediaDB.AssertNotCalled(t, "CreateSecondaryIndexes")
	mockMediaDB.AssertNotCalled(t, "ReconcileStagedSystem", mock.Anything, mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}
