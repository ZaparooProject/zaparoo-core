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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPopulatePersistentScanStateForSystem_LoadsMediaOnce(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	scanState := &database.ScanState{
		TitleIDs:     make(map[string]int),
		MediaIDs:     make(map[string]int),
		MissingMedia: make(map[int]struct{}),
	}

	mockDB.On("GetTitlesBySystemID", "NES").Return([]database.TitleWithSystem{{
		DBID:     11,
		SystemID: "NES",
		Slug:     "super-mario-bros",
	}}, nil).Once()
	mockDB.On("GetMediaBySystemID", "NES").Return([]database.MediaWithFullPath{{
		DBID:      42,
		SystemID:  "NES",
		Path:      "/roms/nes/Super Mario Bros.nes",
		TitleSlug: "super-mario-bros",
	}}, nil).Once()

	err := PopulatePersistentScanStateForSystem(context.Background(), mockDB, scanState, "NES")
	require.NoError(t, err)
	require.Contains(t, scanState.TitleIDs, database.TitleKey("NES", "super-mario-bros"))
	require.Contains(t, scanState.MediaIDs, database.MediaKey("NES", "/roms/nes/Super Mario Bros.nes"))
	require.Contains(t, scanState.MissingMedia, 42)
	mockDB.AssertExpectations(t)
}

func TestNewNamesIndex_ResumeResetMissingFlagsSkipsCompletedSystems(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()

	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil).Once()
	mockMediaDB.On("CreateSecondaryIndexes").Return(nil).Once()
	mockMediaDB.On("SetOptimizationStatus", "pending").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Once()
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("GetLastIndexedSystem").Return("snes", nil).Once()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"genesis", "nes", "snes"}, nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string{"genesis", "nes", "snes"}).Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "snes").Return(nil).Maybe()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()
	mockMediaDB.On("GetMaxSystemID").Return(int64(3), nil).Once()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Once()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Once()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(1), nil).Once()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Once()
	mockMediaDB.On("GetAllSystems").Return([]database.System{
		{DBID: 1, SystemID: "genesis"},
		{DBID: 2, SystemID: "nes"},
		{DBID: 3, SystemID: "snes"},
	}, nil).Once()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{{DBID: 1, Type: "genre"}}, nil).Once()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Once()
	mockMediaDB.On("GetTitlesBySystemID", "snes").Return([]database.TitleWithSystem{}, nil).Once()
	mockMediaDB.On("GetMediaBySystemID", "snes").Return([]database.MediaWithFullPath{}, nil).Once()

	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, []systemdefs.System{
		{ID: "genesis"},
		{ID: "nes"},
		{ID: "snes"},
	}, &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.NoError(t, err)
	mockMediaDB.AssertCalled(t, "ResetMissingFlags", []int{3})
	mockMediaDB.AssertExpectations(t)
}

func TestNewNamesIndex_CancelledSystemDoesNotResetMissingFlags(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	mockUserDB := testhelpers.NewMockUserDBI()
	mockMediaDB := testhelpers.NewMockMediaDBI()

	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Maybe()
	mockMediaDB.On("GetLastIndexedSystem").Return("", nil).Maybe()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"snes"}, nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string{"snes"}).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "cancelled").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "failed").Return(nil).Maybe()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Maybe()
	mockMediaDB.On("GetMaxSystemID").Return(int64(1), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(1), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetAllSystems").Return([]database.System{{DBID: 1, SystemID: "snes"}}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{{DBID: 1, Type: "genre"}}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	_, err := NewNamesIndex(ctx, mockPlatform, cfg, []systemdefs.System{{ID: "snes"}},
		&database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB}, func(IndexStatus) {}, nil)
	require.ErrorIs(t, err, context.Canceled)
	mockMediaDB.AssertNotCalled(t, "ResetMissingFlags", mock.Anything)
	mockMediaDB.AssertNotCalled(t, "BulkSetMediaMissing", mock.Anything)
	mockMediaDB.AssertExpectations(t)
}
