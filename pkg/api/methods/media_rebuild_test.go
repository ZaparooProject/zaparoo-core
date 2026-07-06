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
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newRebuildTestEnv(t *testing.T, mockMediaDB *helpers.MockMediaDBI, params string) requests.RequestEnv {
	t.Helper()

	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Maybe()
	mockMediaDB.On("TrackBackgroundOperation").Return().Maybe()
	mockMediaDB.On("BackgroundOperationDone").Return().Maybe()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{}).Maybe()
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{t.TempDir()}).Maybe()

	appState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	return requests.RequestEnv{
		Context: context.Background(),
		Params:  []byte(params),
		Database: &database.Database{
			MediaDB: mockMediaDB,
			UserDB:  helpers.NewMockUserDBI(),
		},
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}
}

// waitForIndexingFinished polls until the background indexing goroutine started
// by HandleGenerateMedia has cleared the in-process indexing status.
func waitForIndexingFinished(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for background indexing goroutine to finish")
		case <-ticker.C:
			if !IsIndexing() {
				return
			}
		}
	}
}

func TestHandleGenerateMedia_RejectsRebuildWithSystemsFilter(t *testing.T) {
	// Not parallel: shares the global statusInstance.
	ClearIndexingStatus()

	mockMediaDB := helpers.NewMockMediaDBI()
	env := newRebuildTestEnv(t, mockMediaDB, `{"rebuild":true,"systems":["SNES"]}`)

	_, err := HandleGenerateMedia(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rebuild cannot be combined with a systems filter")
	mockMediaDB.AssertNotCalled(t, "Recreate", mock.Anything)
	assert.False(t, IsIndexing(), "a rejected request must not claim the indexing slot")
}

func TestHandleGenerateMedia_RebuildRecreatesDatabaseBeforeIndexing(t *testing.T) {
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

	// Plant a media row the rebuild must discard. A plain (non-rebuild) reindex
	// over the empty roots would only flag it missing — the row itself survives —
	// so a zero row count afterwards proves the database file was recreated.
	sys, err := db.MediaDB.FindOrInsertSystem(database.System{SystemID: "SNES", Name: "SNES"})
	require.NoError(t, err)
	title, err := db.MediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       "marker",
		Name:       "Marker",
	})
	require.NoError(t, err)
	markerPath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "snes", "marker.sfc"))
	_, err = db.MediaDB.InsertMedia(database.Media{
		SystemDBID:     sys.DBID,
		MediaTitleDBID: title.DBID,
		Path:           markerPath,
		ParentDir:      mediadb.ParentDirForMediaPath(markerPath),
	})
	require.NoError(t, err)
	count, err := db.MediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	require.Equal(t, 1, count)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Params:   []byte(`{"rebuild":true}`),
		Database: db,
		Platform: mockPlatform,
		State:    appState,
		Config:   &config.Instance{},
		ClientID: "127.0.0.1:12345",
	}
	_, err = HandleGenerateMedia(env)
	require.NoError(t, err)

	waitForIndexingFinished(t)

	count, err = db.MediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count, "rebuild must discard the existing media database, not upsert into it")
}

func TestHandleGenerateMedia_RebuildRecreateFailureAbortsIndexing(t *testing.T) {
	// Not parallel: shares the global statusInstance.
	ClearIndexingStatus()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("Recreate", false).Return(errors.New("disk on fire")).Once()
	mockMediaDB.On("GetLastGenerated").Return(time.Unix(0, 0), nil).Maybe()

	env := newRebuildTestEnv(t, mockMediaDB, `{"rebuild":true}`)
	_, err := HandleGenerateMedia(env)
	require.NoError(t, err, "the request is accepted; the recreate failure surfaces from the background goroutine")

	waitForIndexingFinished(t)
	// The scanner never ran: a failed recreate must abort before NewNamesIndex
	// touches the (possibly still closed) database.
	mockMediaDB.AssertNotCalled(t, "SetIndexingStatus", mock.Anything)
	mockMediaDB.AssertExpectations(t)
}
