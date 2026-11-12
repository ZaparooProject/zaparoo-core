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

package playlistmemory_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlistmemory"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openUserDBFile(ctx context.Context, t *testing.T, path string) *userdb.UserDB {
	t.Helper()
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	sqlDB, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	db := &userdb.UserDB{}
	require.NoError(t, db.SetSQLForTesting(ctx, sqlDB, platform))
	return db
}

// The service cancels the context the UserDB was opened with before it stops
// the tracker and the writer, so the last checkpoint is written after that
// context is gone and must still land.
func TestShutdownWritesSurviveACanceledServiceContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "user.db")
	ctx, cancel := context.WithCancel(context.Background())
	db := openUserDBFile(ctx, t, path)
	mem := playlistmemory.New(db, nil)
	mem.Start()
	rig := &trackerRig{mem: mem, store: db, profile: &profileSwitch{}, playback: &fakePlayback{}}
	tracker := playlistmemory.NewPositionTracker(mem, rig.playback, rig.activeMedia, nil)
	tracker.Start()
	rig.play(bookPath, 33*time.Minute, time.Hour, false)

	cancel()
	pls := playlists.NewPlaylist("deck", "deck", scripts("a", "b"))
	mem.PlaylistChanged(mediaslot.Primary, playlists.Next(*pls))
	tracker.Stop()
	mem.Stop()
	require.NoError(t, db.Close())

	reopened := openUserDBFile(context.Background(), t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	pos, found, err := reopened.GetMediaPosition("", systemdefs.SystemAudio, bookPath)
	require.NoError(t, err)
	require.True(t, found, "the position saved at shutdown is kept")
	assert.Equal(t, (33 * time.Minute).Milliseconds(), pos.PositionMs)
	rows, err := reopened.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1, "the playlist recorded at shutdown is kept")
	mem2, found, err := reopened.GetPlaylistMemory("", "id:deck")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, mem2.ItemIndex)
}
