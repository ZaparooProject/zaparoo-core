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
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubPlaybackManager is a minimal PlaybackManager that returns fixed state per slot.
type stubPlaybackManager struct {
	states map[string]audio.PlaybackState
}

func newStubPlaybackManager(slot string, state audio.PlaybackState) *stubPlaybackManager {
	return &stubPlaybackManager{states: map[string]audio.PlaybackState{slot: state}}
}

func (*stubPlaybackManager) Play(_, _ string, _ audio.PlaybackOptions) error { return nil }
func (*stubPlaybackManager) Stop(_ string) error                             { return nil }
func (*stubPlaybackManager) Pause(_ string) error                            { return nil }
func (*stubPlaybackManager) Resume(_ string) error                           { return nil }
func (*stubPlaybackManager) TogglePause(_ string) error                      { return nil }
func (*stubPlaybackManager) Seek(_ string, _ time.Duration) error            { return nil }
func (s *stubPlaybackManager) State(slot string) audio.PlaybackState {
	return s.states[slot]
}

func TestMediaIDsByPath_DeduplicatesRefsAndSkipsInvalidRefs(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pathOne := filepath.Join("games", "one.rom")
	pathTwo := filepath.Join("games", "two.rom")
	refs := []mediaPathRef{
		{SystemID: "NES", Path: pathOne},
		{SystemID: "NES", Path: pathOne},
		{SystemID: "NES", Path: pathTwo},
		{SystemID: "", Path: pathTwo},
		{SystemID: "NES", Path: ""},
	}

	mockDB.On("FindMediaIDsByPaths", mock.Anything, []string{pathOne, pathTwo}).Return(
		[]database.MediaPathID{
			{SystemID: "NES", Path: pathOne, DBID: 10},
			{SystemID: "NES", Path: pathTwo, DBID: 11},
		}, nil,
	)

	ids := mediaIDsByPath(context.Background(), mockDB, refs)

	assert.Equal(t, map[mediaPathRef]int64{
		{SystemID: "NES", Path: pathOne}: 10,
		{SystemID: "NES", Path: pathTwo}: 11,
	}, ids)
	mockDB.AssertExpectations(t)
}

// ----- toPlaylistState tests -----

func TestToPlaylistState_RepeatNone(t *testing.T) {
	t.Parallel()
	p := &playlists.Playlist{
		ID:   "pl-1",
		Name: "Rock Classics",
		Slot: mediaslot.Background,
		Items: []playlists.PlaylistItem{
			{Name: "Track A", ZapScript: "**launch:a.mp3"},
			{Name: "Track B", ZapScript: "**launch:b.mp3"},
		},
		Index: 1,
	}
	got := toPlaylistState(p)
	assert.Equal(t, "pl-1", got.ID)
	assert.Equal(t, "Rock Classics", got.Name)
	assert.Equal(t, mediaslot.Background, got.Slot)
	assert.Equal(t, 1, got.Index)
	assert.Equal(t, 2, got.Total)
	assert.Equal(t, "none", got.Repeat)
	assert.False(t, got.Playing)
	assert.Len(t, got.Items, 2)
	assert.Equal(t, "Track A", got.Items[0].Name)
}

func TestToPlaylistState_RepeatAll(t *testing.T) {
	t.Parallel()
	p := &playlists.Playlist{Slot: mediaslot.Primary, Loop: true}
	assert.Equal(t, "all", toPlaylistState(p).Repeat)
}

func TestToPlaylistState_RepeatOne(t *testing.T) {
	t.Parallel()
	// LoopOne takes priority over Loop if both are set (defensive)
	p := &playlists.Playlist{Slot: mediaslot.Primary, Loop: true, LoopOne: true}
	assert.Equal(t, "one", toPlaylistState(p).Repeat)
}

func TestToPlaylistState_Playing(t *testing.T) {
	t.Parallel()
	p := &playlists.Playlist{Slot: mediaslot.Primary, Playing: true}
	assert.True(t, toPlaylistState(p).Playing)
}

func TestToPlaylistState_EmptyItems(t *testing.T) {
	t.Parallel()
	p := &playlists.Playlist{Slot: mediaslot.Primary}
	state := toPlaylistState(p)
	assert.Empty(t, state.Items)
	assert.Equal(t, 0, state.Total)
}

// ----- enrichPlaybackPosition tests -----

func TestEnrichPlaybackPosition_NilManagerLeavesFieldsNil(t *testing.T) {
	t.Parallel()
	env := &requests.RequestEnv{PlaybackManager: nil}
	entry := &models.ActiveMedia{LauncherID: platforms.NativeAudioLauncherID}
	enrichPlaybackPosition(env, entry, mediaslot.Primary)
	assert.Nil(t, entry.PositionMs)
	assert.Nil(t, entry.DurationMs)
}

func TestEnrichPlaybackPosition_NonNativeLauncherLeavesFieldsNil(t *testing.T) {
	t.Parallel()
	mgr := newStubPlaybackManager(mediaslot.Primary, audio.PlaybackState{
		Position: 30 * time.Second,
		Duration: 3 * time.Minute,
	})
	env := &requests.RequestEnv{PlaybackManager: mgr}
	entry := &models.ActiveMedia{LauncherID: "mister"}
	enrichPlaybackPosition(env, entry, mediaslot.Primary)
	assert.Nil(t, entry.PositionMs)
	assert.Nil(t, entry.DurationMs)
}

func TestEnrichPlaybackPosition_NativeAudioFillsPositionAndDuration(t *testing.T) {
	t.Parallel()
	pos := 45 * time.Second
	dur := 3 * time.Minute
	mgr := newStubPlaybackManager(mediaslot.Primary, audio.PlaybackState{
		Position: pos,
		Duration: dur,
	})
	env := &requests.RequestEnv{PlaybackManager: mgr}
	entry := &models.ActiveMedia{LauncherID: platforms.NativeAudioLauncherID}
	enrichPlaybackPosition(env, entry, mediaslot.Primary)
	require.NotNil(t, entry.PositionMs)
	require.NotNil(t, entry.DurationMs)
	assert.Equal(t, pos.Milliseconds(), *entry.PositionMs)
	assert.Equal(t, dur.Milliseconds(), *entry.DurationMs)
}

func TestEnrichPlaybackPosition_BackgroundSlotUsesBackgroundState(t *testing.T) {
	t.Parallel()
	bgPos := 10 * time.Second
	bgDur := 2 * time.Minute
	mgr := &stubPlaybackManager{states: map[string]audio.PlaybackState{
		mediaslot.Primary:    {Position: 999 * time.Second, Duration: 999 * time.Second},
		mediaslot.Background: {Position: bgPos, Duration: bgDur},
	}}
	env := &requests.RequestEnv{PlaybackManager: mgr}
	entry := &models.ActiveMedia{LauncherID: platforms.NativeAudioLauncherID}
	enrichPlaybackPosition(env, entry, mediaslot.Background)
	require.NotNil(t, entry.PositionMs)
	assert.Equal(t, bgPos.Milliseconds(), *entry.PositionMs)
}

func TestMediaIDsByPath_IgnoresRowsForUnrequestedSystems(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	path := filepath.Join("games", "shared.rom")

	// The same path can exist under multiple systems; only the requested
	// (system, path) pair should be resolved.
	mockDB.On("FindMediaIDsByPaths", mock.Anything, []string{path}).Return(
		[]database.MediaPathID{
			{SystemID: "NES", Path: path, DBID: 10},
			{SystemID: "FDS", Path: path, DBID: 22},
		}, nil,
	)

	ids := mediaIDsByPath(context.Background(), mockDB, []mediaPathRef{{SystemID: "NES", Path: path}})

	assert.Equal(t, map[mediaPathRef]int64{
		{SystemID: "NES", Path: path}: 10,
	}, ids)
	mockDB.AssertExpectations(t)
}
