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

package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// playSyncTestEntry builds one closed, syncable MediaHistory row.
func playSyncTestEntry(dbid int64, id, mediaName string, updatedAt time.Time) database.MediaHistoryEntry {
	endTime := updatedAt
	return database.MediaHistoryEntry{
		DBID:          dbid,
		ID:            id,
		StartTime:     updatedAt.Add(-30 * time.Minute),
		EndTime:       &endTime,
		SystemID:      "snes",
		SystemName:    "Super Nintendo",
		MediaPath:     "/games/" + mediaName + ".sfc",
		MediaName:     mediaName,
		LauncherID:    "test",
		PlayTime:      1800,
		ClockReliable: true,
		ClockSource:   "system",
		Tags:          []string{"region:us", "ext:sfc"},
		UpdatedAt:     updatedAt,
	}
}

// playSyncTestServer serves the watermark and ingestion endpoints, recording
// every uploaded batch.
func playSyncTestServer(t *testing.T, watermark *time.Time) (*httptest.Server, *[][]remotePlaySessionItem) {
	t.Helper()
	var batches [][]remotePlaySessionItem
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/device/play-sessions/watermark":
			assert.NoError(t, json.NewEncoder(w).Encode(remotePlayWatermarkResponse{Watermark: watermark}))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/device/play-sessions":
			var req remotePlaySessionRequest
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			batches = append(batches, req.Sessions)
			newest := req.Sessions[len(req.Sessions)-1].CoreUpdatedAt
			assert.NoError(t, json.NewEncoder(w).Encode(remotePlaySessionResponse{
				Accepted:  int64(len(req.Sessions)),
				Watermark: &newest,
			}))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &batches
}

func TestSyncPlayHistory_BulkImport(t *testing.T) {
	// No t.Parallel(): configureRemoteTestAuth mutates the global auth config.
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(true)
	base := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)

	server, batches := playSyncTestServer(t, nil)
	configureRemoteTestAuth(t, env.Manager, server.URL)

	first := playSyncTestEntry(1, "11111111-1111-4111-8111-111111111111", "Game A", base)
	second := playSyncTestEntry(2, "22222222-2222-4222-8222-222222222222", "Game B", base.Add(time.Hour))

	env.UserDB.On("ResetMediaHistorySyncAfter", (*time.Time)(nil)).Return(nil).Once()
	// Never synced: the cursor starts at zero and the whole history uploads.
	env.UserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), playSyncBatchSize).
		Return([]database.MediaHistoryEntry{first, second}, nil).Once()
	env.UserDB.On("MarkMediaHistorySynced", []database.MediaHistorySyncRef{
		{DBID: first.DBID, UpdatedAt: first.UpdatedAt},
		{DBID: second.DBID, UpdatedAt: second.UpdatedAt},
	}, testifymock.AnythingOfType("time.Time")).Return(nil).Once()

	info, err := env.Manager.SyncPlayHistory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, info.Uploaded)
	assert.Equal(t, 1, info.Batches)

	require.Len(t, *batches, 1)
	uploaded := (*batches)[0]
	require.Len(t, uploaded, 2)
	assert.Equal(t, first.ID, uploaded[0].SessionUUID)
	assert.Equal(t, "Game A", uploaded[0].MediaName)
	assert.Equal(t, 1800, uploaded[0].PlayTimeSecs)
	assert.True(t, first.UpdatedAt.Equal(uploaded[0].CoreUpdatedAt))
	assert.True(t, uploaded[0].ClockReliable)
	assert.Equal(t, []string{"region:us", "ext:sfc"}, uploaded[0].Tags,
		"disambiguating tags travel with the session")
}

func TestSyncPlayHistory_ResumesFromServerWatermark(t *testing.T) {
	// No t.Parallel(): configureRemoteTestAuth mutates the global auth config.
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(true)
	watermark := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)

	server, batches := playSyncTestServer(t, &watermark)
	configureRemoteTestAuth(t, env.Manager, server.URL)

	env.UserDB.On(
		"ResetMediaHistorySyncAfter",
		testifymock.MatchedBy(func(got *time.Time) bool {
			return got != nil && watermark.Equal(*got)
		}),
	).Return(nil).Once()
	// Local acknowledgement state suppresses unchanged rows; selection always
	// starts at zero so old unreliable-clock rows remain eligible.
	env.UserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), playSyncBatchSize).
		Return([]database.MediaHistoryEntry{}, nil).Once()

	info, err := env.Manager.SyncPlayHistory(context.Background())
	require.NoError(t, err)
	assert.Zero(t, info.Uploaded)
	assert.Empty(t, *batches, "nothing new: no upload requests")
}

func TestSyncPlayHistory_PaginatesFullBatches(t *testing.T) {
	// No t.Parallel(): configureRemoteTestAuth mutates the global auth config.
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(true)
	base := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)

	server, batches := playSyncTestServer(t, nil)
	configureRemoteTestAuth(t, env.Manager, server.URL)

	// A full first batch forces a second query cursored after its last row.
	full := make([]database.MediaHistoryEntry, 0, playSyncBatchSize)
	refs := make([]database.MediaHistorySyncRef, 0, playSyncBatchSize)
	for i := range playSyncBatchSize {
		entry := playSyncTestEntry(
			int64(i+1), "11111111-1111-4111-8111-111111111111", "Game", base.Add(time.Duration(i)*time.Second),
		)
		full = append(full, entry)
		refs = append(refs, database.MediaHistorySyncRef{DBID: entry.DBID, UpdatedAt: entry.UpdatedAt})
	}
	last := full[len(full)-1]

	env.UserDB.On("ResetMediaHistorySyncAfter", (*time.Time)(nil)).Return(nil).Once()
	env.UserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), playSyncBatchSize).
		Return(full, nil).Once()
	env.UserDB.On(
		"GetMediaHistorySyncBatch",
		testifymock.MatchedBy(func(after time.Time) bool { return last.UpdatedAt.Equal(after) }),
		last.DBID, playSyncBatchSize,
	).Return([]database.MediaHistoryEntry{}, nil).Once()
	env.UserDB.On("MarkMediaHistorySynced", refs, testifymock.AnythingOfType("time.Time")).
		Return(nil).Once()

	info, err := env.Manager.SyncPlayHistory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, playSyncBatchSize, info.Uploaded)
	assert.Equal(t, 1, info.Batches)
	require.Len(t, *batches, 1)
}

func TestSyncPlayHistory_StopsWhenConsentRevokedDuringPass(t *testing.T) {
	// No t.Parallel(): configureRemoteTestAuth mutates the global auth config.
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(true)
	base := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	server, batches := playSyncTestServer(t, nil)
	configureRemoteTestAuth(t, env.Manager, server.URL)
	entry := playSyncTestEntry(1, "11111111-1111-4111-8111-111111111111", "Game A", base)

	env.UserDB.On("ResetMediaHistorySyncAfter", (*time.Time)(nil)).Return(nil).Once()
	env.UserDB.On("GetMediaHistorySyncBatch", time.Time{}, int64(0), playSyncBatchSize).
		Run(func(_ testifymock.Arguments) { env.Manager.cfg.SetPlaytimeSync(false) }).
		Return([]database.MediaHistoryEntry{entry}, nil).Once()

	info, err := env.Manager.SyncPlayHistory(context.Background())
	require.ErrorIs(t, err, errPlaySyncDisabled)
	assert.Zero(t, info.Uploaded)
	assert.Empty(t, *batches, "revoked consent must stop before the next upload")
	env.UserDB.AssertNotCalled(t, "MarkMediaHistorySynced", testifymock.Anything, testifymock.Anything)
}

func TestSyncPlayHistory_UnsetConsentIsDisabled(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, "mister")

	assert.False(t, env.Manager.cfg.PlaytimeSyncEnabled())
	_, err := env.Manager.SyncPlayHistory(context.Background())
	require.ErrorIs(t, err, errPlaySyncDisabled)
}

func TestSyncPlayHistory_DisabledByConfig(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(false)

	_, err := env.Manager.SyncPlayHistory(context.Background())
	require.ErrorIs(t, err, errPlaySyncDisabled)
}

func TestSyncPlayHistory_Unlinked(t *testing.T) {
	// No t.Parallel(): configureRemoteTestAuth mutates the global auth config.
	env := newBackupTestEnv(t, "mister")
	env.Manager.cfg.SetPlaytimeSync(true)
	// No credential configured: sync must report unlinked, not upload.

	_, err := env.Manager.SyncPlayHistory(context.Background())
	require.ErrorIs(t, err, errRemoteUnlinked)
}
