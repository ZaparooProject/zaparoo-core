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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func playlistsUpdateEnv(t *testing.T, params string) (requests.RequestEnv, *state.State, *[]*playlists.Playlist) {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)
	var requested []*playlists.Playlist
	st.SetPlaylistRequester(func(_ context.Context, pls *playlists.Playlist) error {
		requested = append(requested, pls)
		return nil
	})
	return requests.RequestEnv{
		Context: context.Background(),
		State:   st,
		Params:  json.RawMessage(params),
	}, st, &requested
}

func TestHandlePlaylistsUpdate_QueuesModeChange(t *testing.T) {
	t.Parallel()
	env, st, requested := playlistsUpdateEnv(t, `{"slot":"bg","shuffle":true,"repeat":"off"}`)
	st.SetBackgroundPlaylist(&playlists.Playlist{ID: "music", Slot: mediaslot.Background})

	result, err := HandlePlaylistsUpdate(env)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Equal(t, "null", string(encoded))

	require.Len(t, *requested, 1)
	update := (*requested)[0]
	assert.True(t, update.ModeChange)
	assert.Equal(t, mediaslot.Background, update.Slot)
	assert.True(t, update.ShuffleSet)
	assert.True(t, update.Shuffle)
	require.NotNil(t, update.Repeat)
	assert.Equal(t, playlists.RepeatNone, *update.Repeat, "off is accepted as none")
	assert.Empty(t, update.Items, "a mode change carries no playlist of its own")
}

func TestHandlePlaylistsUpdate_DefaultsToPrimaryAndOnlyWhatWasAsked(t *testing.T) {
	t.Parallel()
	env, st, requested := playlistsUpdateEnv(t, `{"repeat":"one"}`)
	st.SetActivePlaylist(&playlists.Playlist{ID: "games"})

	_, err := HandlePlaylistsUpdate(env)
	require.NoError(t, err)
	require.Len(t, *requested, 1)
	update := (*requested)[0]
	assert.Equal(t, mediaslot.Primary, update.Slot)
	assert.False(t, update.ShuffleSet, "shuffle is left alone when not asked")
	require.NotNil(t, update.Repeat)
	assert.Equal(t, playlists.RepeatOne, *update.Repeat)
}

func TestHandlePlaylistsUpdate_ClientErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		params string
		open   bool
	}{
		{name: "nothing to change", params: `{"slot":"primary"}`, open: true},
		{name: "bad repeat", params: `{"repeat":"sometimes"}`, open: true},
		{name: "bad slot", params: `{"slot":"side","shuffle":true}`, open: true},
		{name: "no playlist open", params: `{"shuffle":true}`, open: false},
		{name: "no playlist in that slot", params: `{"slot":"background","shuffle":true}`, open: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, st, requested := playlistsUpdateEnv(t, tc.params)
			if tc.open {
				st.SetActivePlaylist(&playlists.Playlist{ID: "games"})
			}
			_, err := HandlePlaylistsUpdate(env)
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr, "%v is a client error", err)
			assert.Empty(t, *requested)
		})
	}
}

func TestHandlePlaylistsUpdate_QueueFailureIsServerError(t *testing.T) {
	t.Parallel()
	env, st, _ := playlistsUpdateEnv(t, `{"shuffle":false}`)
	st.SetActivePlaylist(&playlists.Playlist{ID: "games"})
	st.SetPlaylistRequester(func(context.Context, *playlists.Playlist) error {
		return context.Canceled
	})

	_, err := HandlePlaylistsUpdate(env)
	require.ErrorIs(t, err, context.Canceled)
}
