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

package service

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
)

// The recovery happy path runs a real reindex via methods.GenerateMediaDB and is covered
// by the mediadb RecreateAfterCorruption tests plus manual verification. These tests cover
// the guard/early-return branches, where recovery must NOT touch the database.

func TestCheckAndRecoverCorruptMediaDB_NoMarkerIsNoOp(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(false)
	mockDB.On("GetIndexingStatus").Return("", nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertNotCalled(t, "RecreateAfterCorruption", true)
	mockDB.AssertNotCalled(t, "RecreateAfterCorruption", false)
}

func TestCheckAndRecoverCorruptMediaDB_DefersWhenIndexingInFlight(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IsMarkedCorrupt").Return(true)
	mockDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{MediaDB: mockDB}, nil, nil)

	mockDB.AssertNotCalled(t, "RecreateAfterCorruption", true)
	mockDB.AssertNotCalled(t, "RecreateAfterCorruption", false)
}

func TestCheckAndRecoverCorruptMediaDB_NilDatabaseIsNoOp(_ *testing.T) {
	// Must not panic with a nil database or nil MediaDB.
	checkAndRecoverCorruptMediaDB(nil, nil, nil, nil, nil)
	checkAndRecoverCorruptMediaDB(nil, nil, &database.Database{}, nil, nil)
}

// TestCheckAndResumeOptimization_FailedCorruptMarksAndSkips verifies the quick_check gate:
// a failed optimization on a corrupt database is flagged corrupt and NOT resumed (which
// would just fail again on every boot).
func TestCheckAndResumeOptimization_FailedCorruptMarksAndSkips(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusFailed, nil)
	mockDB.On("QuickCheck").Return(false, nil)
	mockDB.On("IntegrityReport").Return([]string{"*** in database main ***", "Page 42: btree corrupt"})
	mockDB.On("MarkCorrupt", "quick_check failed before optimization resume").Return()
	mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCorrupt).Return(nil)

	ns := make(chan models.Notification, 10)
	checkAndResumeOptimization(&database.Database{MediaDB: mockDB}, ns, syncutil.NewPauser())

	mockDB.AssertCalled(t, "MarkCorrupt", "quick_check failed before optimization resume")
	mockDB.AssertCalled(t, "SetIndexingStatus", mediadb.IndexingStatusCorrupt)
	mockDB.AssertNotCalled(t, "RunBackgroundOptimization")
}
