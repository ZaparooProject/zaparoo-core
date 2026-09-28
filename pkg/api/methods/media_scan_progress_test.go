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

package methods

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func mediaStatusDuringIndex(t *testing.T, vals indexingStatusVals) models.IndexingStatusResponse {
	t.Helper()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Time{}, nil).Maybe()
	testState, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	statusInstance.set(vals)
	t.Cleanup(ClearIndexingStatus)

	result, err := HandleMedia(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB, UserDB: &helpers.MockUserDBI{}},
		State:    testState,
	})
	require.NoError(t, err)
	response, ok := result.(models.MediaResponse)
	require.True(t, ok)
	return response.Database
}

// Issue #1572: a folder scan can run for many minutes, and clients showed
// nothing moving. The media status reports how far the scan has read and the
// folder being read, and only while a scan runs.
func TestHandleMedia_ReportsFolderScanProgress(t *testing.T) {
	// Not parallel: shares the global statusInstance.
	scanning := mediaStatusDuringIndex(t, indexingStatusVals{
		indexing: true, totalSteps: 3, currentStep: 1, currentDesc: "Amiga",
		scannedEntries: 48213, scanPath: "/media/usb0/games/Amiga/TOSEC",
	})
	require.NotNil(t, scanning.Scan)
	assert.Equal(t, models.IndexingScanResponse{Entries: 48213, Path: "/media/usb0/games/Amiga/TOSEC"},
		*scanning.Scan)

	writing := mediaStatusDuringIndex(t, indexingStatusVals{
		indexing: true, totalSteps: 3, currentStep: 1, currentDesc: "Amiga",
	})
	assert.Nil(t, writing.Scan, "absent outside a scan")
}

// A stalled scan tells the user once, naming the system and folder and how to
// exclude it. It is deduplicated by category, so a repeat replaces it.
func TestReportStalledScan_AddsInboxMessage(t *testing.T) {
	// Not parallel: sets the package inbox.
	userDB := helpers.NewMockUserDBI()
	userDB.On("AddInboxMessage", mock.Anything).Return(&database.InboxMessage{}, nil)
	testState, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	SetIndexingInbox(inboxservice.NewService(userDB, testState.Notifications))
	t.Cleanup(func() { SetIndexingInbox(nil) })

	reportStalledScan("Commodore Amiga", "/media/usb0/games/Amiga/TOSEC", 2400)

	require.Len(t, userDB.Calls, 1)
	msg, ok := userDB.Calls[0].Arguments.Get(0).(*database.InboxMessage)
	require.True(t, ok)
	assert.Equal(t, inboxservice.CategoryMediaIndexScanStalled, msg.Category)
	assert.Equal(t, inboxservice.SeverityWarning, msg.Severity)
	assert.Contains(t, msg.Body, "Commodore Amiga")
	assert.Contains(t, msg.Body, "/media/usb0/games/Amiga/TOSEC")
	assert.Contains(t, msg.Body, ".zaparooignore")
}

func TestReportStalledScan_NoInboxIsANoOp(t *testing.T) {
	SetIndexingInbox(nil)
	assert.NotPanics(t, func() { reportStalledScan("NES", "/roms/NES", 1) })
}

// Starting an index covers systems auto-resume skipped: the list is cleared
// before the run reads it. The run is held paused so it cannot finish and
// clear the list itself.
func TestHandleGenerateMedia_ClearsSystemsSkippedByResume(t *testing.T) {
	// Not parallel: shares the global statusInstance.
	ClearIndexingStatus()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{}).Maybe()
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{t.TempDir()}).Maybe()
	db, cleanup := helpers.NewTestDatabase(t)
	defer cleanup()
	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")
	require.NoError(t, db.MediaDB.SetIndexingSkippedSystems([]string{"NES"}))
	pauser := syncutil.NewPauser()
	pauser.Pause()

	_, err := HandleGenerateMedia(requests.RequestEnv{
		Context: context.Background(), Params: []byte(`{"systems":["NES"]}`),
		Database: db, Platform: mockPlatform, State: appState,
		Config: &config.Instance{}, ClientID: "127.0.0.1:12345", IndexPauser: pauser,
	})
	require.NoError(t, err)
	skipped, err := db.MediaDB.GetIndexingSkippedSystems()
	require.NoError(t, err)
	assert.Empty(t, skipped)

	pauser.Resume()
	waitForIndexingFinished(t)
}

// A request refused because indexing is running leaves that run's list alone.
func TestHandleGenerateMedia_KeepsSkipListWhileIndexing(t *testing.T) {
	ClearIndexingStatus()
	SetIndexingForTest()
	t.Cleanup(ClearIndexingStatus)
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SetIndexingSkippedSystems", mock.Anything).Return(nil).Maybe()
	env := newRebuildTestEnv(t, mockMediaDB, "")

	_, err := HandleGenerateMedia(env)
	require.Error(t, err)
	mockMediaDB.AssertNotCalled(t, "SetIndexingSkippedSystems", mock.Anything)
}
