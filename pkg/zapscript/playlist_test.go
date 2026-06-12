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

package zapscript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newPlaylistTestPlatform creates a mock platform with Launchers configured for playlist tests.
func newPlaylistTestPlatform() *mocks.MockPlatform {
	mp := mocks.NewMockPlatform()
	mp.On("Launchers", mock.Anything).Return([]platforms.Launcher{}).Maybe()
	return mp
}

func TestQueuePlaylistUpdateReturnsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env := platforms.CmdEnv{
		LauncherCtx: ctx,
		Playlist: playlists.PlaylistController{
			Queue: make(chan *playlists.Playlist),
		},
	}

	err := queuePlaylistUpdate(&env, &playlists.Playlist{})

	require.ErrorIs(t, err, context.Canceled)
}

func TestQueuePlaylistUpdateReturnsWhenServiceContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env := platforms.CmdEnv{
		ServiceCtx: ctx,
		Playlist: playlists.PlaylistController{
			Queue: make(chan *playlists.Playlist),
		},
	}

	err := queuePlaylistUpdate(&env, &playlists.Playlist{})

	require.ErrorIs(t, err, context.Canceled)
}

func TestReadPlsFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		plsContent     string
		expectedErrMsg string
		expectedMedia  []playlists.PlaylistItem
	}{
		{
			name: "valid_pls_with_multiple_entries",
			plsContent: `[playlist]
File1=/path/to/song1.mp3
Title1=Song 1
File2=/path/to/song2.mp3
Title2=Song 2`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "Song 1", ZapScript: "/path/to/song1.mp3"},
				{Name: "Song 2", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "valid_pls_with_missing_titles",
			plsContent: `[playlist]
File1=/path/to/song1.mp3
File2=/path/to/song2.mp3`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "", ZapScript: "/path/to/song1.mp3"},
				{Name: "", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "valid_pls_with_missing_files",
			plsContent: `[playlist]
Title1=Song 1
File2=/path/to/song2.mp3`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "missing_header",
			plsContent: `File1=/path/to/song1.mp3
Title1=Song 1
File2=/path/to/song2.mp3
Title2=Song 2`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
		{
			name: "empty_pls_file",
			plsContent: `
			`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
		{
			name: "invalid_entry_ids",
			plsContent: `[playlist]
FileA=/path/to/song1.mp3
TitleB=Song 1`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plsFile := filepath.Join(t.TempDir(), "test.pls")
			err := os.WriteFile(plsFile, []byte(tt.plsContent), 0o600)
			require.NoError(t, err)

			media, err := readPlsFile(plsFile)
			if tt.expectedErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedMedia, media)
			}
		})
	}
}

// TestCmdPlaylistOpen_NoArgs tests playlist.open command behavior with no arguments
func TestCmdPlaylistOpen_NoArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		activePlaylist   *playlists.Playlist
		expectedError    string
		expectPickerCall bool
	}{
		{
			name: "no args with active playlist shows picker",
			activePlaylist: &playlists.Playlist{
				ID:   "test-playlist",
				Name: "Test Playlist",
				Items: []playlists.PlaylistItem{
					{Name: "Item 1", ZapScript: "**test1"},
					{Name: "Item 2", ZapScript: "**test2"},
					{Name: "Item 3", ZapScript: "**test3"},
				},
				Index:   1, // Currently at second item
				Playing: true,
			},
			expectPickerCall: true,
		},
		{
			name:           "no args with no active playlist returns error",
			activePlaylist: nil,
			expectedError:  "no active playlist to open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := newPlaylistTestPlatform()
			cfg := &config.Instance{}

			// Mock ShowPicker if we expect it to be called
			if tt.expectPickerCall {
				mockPlatform.On("ShowPicker", cfg, mock.MatchedBy(func(args models.PickerArgs) bool {
					// Verify picker shows the active playlist
					return args.Title == tt.activePlaylist.Name &&
						len(args.Items) == len(tt.activePlaylist.Items) &&
						args.Selected == tt.activePlaylist.Index
				})).Return(nil)
			}

			// Create playlist queue channel
			playlistQueue := make(chan *playlists.Playlist, 1)

			env := platforms.CmdEnv{
				Cmd: zapscript.Command{
					Name: "playlist.open",
					Args: []string{}, // No arguments!
				},
				Cfg: cfg,
				Playlist: playlists.PlaylistController{
					Active: tt.activePlaylist,
					Queue:  playlistQueue,
				},
			}

			result, err := cmdPlaylistOpen(mockPlatform, env)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.True(t, result.PlaylistChanged)
				assert.Equal(t, tt.activePlaylist, result.Playlist)

				// Verify playlist was sent to queue
				select {
				case queuedPlaylist := <-playlistQueue:
					assert.Equal(t, tt.activePlaylist, queuedPlaylist)
				default:
					t.Fatal("expected playlist to be queued")
				}

				mockPlatform.AssertExpectations(t)
			}
		})
	}
}

// TestCmdPlaylistOpen_PreservesPosition tests that position is preserved when reopening active playlist
func TestCmdPlaylistOpen_PreservesPosition(t *testing.T) {
	t.Parallel()

	// Create a test playlist file
	plsContent := `[playlist]
File1=**test1
Title1=Item 1
File2=**test2
Title2=Item 2
File3=**test3
Title3=Item 3`

	plsFile := filepath.Join(t.TempDir(), "test.pls")
	err := os.WriteFile(plsFile, []byte(plsContent), 0o600)
	require.NoError(t, err)

	mockPlatform := newPlaylistTestPlatform()
	cfg := &config.Instance{}

	// Active playlist at index 2 (third item)
	activePlaylist := &playlists.Playlist{
		ID:   plsFile, // ID is the file path
		Name: "test.pls",
		Items: []playlists.PlaylistItem{
			{Name: "Item 1", ZapScript: "**test1"},
			{Name: "Item 2", ZapScript: "**test2"},
			{Name: "Item 3", ZapScript: "**test3"},
		},
		Index:   2, // At third item
		Playing: true,
	}

	// Mock ShowPicker - verify it's called with preserved index
	mockPlatform.On("ShowPicker", cfg, mock.MatchedBy(func(args models.PickerArgs) bool {
		// Should preserve the Index from active playlist
		return args.Selected == 2 && len(args.Items) == 3
	})).Return(nil)

	playlistQueue := make(chan *playlists.Playlist, 1)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "playlist.open",
			Args: []string{plsFile}, // Argument matches active playlist
		},
		Cfg: cfg,
		Playlist: playlists.PlaylistController{
			Active: activePlaylist,
			Queue:  playlistQueue,
		},
	}

	result, err := cmdPlaylistOpen(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	assert.Equal(t, 2, result.Playlist.Index, "should preserve current position")

	mockPlatform.AssertExpectations(t)
}

// makePlaylistEnv returns a 3-item playlist and a buffered queue channel for use in tests.
func makePlaylistEnv() (pls *playlists.Playlist, queue chan *playlists.Playlist) {
	items := []playlists.PlaylistItem{
		{Name: "Item 1", ZapScript: "**test1"},
		{Name: "Item 2", ZapScript: "**test2"},
		{Name: "Item 3", ZapScript: "**test3"},
	}
	pls = playlists.NewPlaylist("id", "name", items)
	queue = make(chan *playlists.Playlist, 1)
	return pls, queue
}

// bgAdvArgs returns an AdvArgs with slot=background set.
func bgAdvArgs() zapscript.AdvArgs {
	var aa zapscript.AdvArgs
	return aa.With(zapscript.KeySlot, "background")
}

// TestCommandSlot_InheritFromPlaylistSlot verifies that commandSlot inherits the
// slot from the active playlist when no AdvArgs slot is set.
func TestCommandSlot_InheritFromPlaylistSlot(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Slot = "background"

	// No AdvArgs slot → inherit from Active.Slot ("background").
	// Background must also be set so activePlaylistForSlot("background") is non-nil.
	result, err := cmdPlaylistNext(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Active: pls, Background: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	<-queue
}

// TestCmdPlaylistPlay_ResumesActivePausedPlaylist covers the path where an active
// paused playlist is resumed with no args (slot=primary, active != nil, no arg).
func TestCmdPlaylistPlay_ResumesActivePausedPlaylist(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Playing = false // paused

	result, err := cmdPlaylistPlay(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{}},
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	require.NotNil(t, result.Playlist)
	assert.True(t, result.Playlist.Playing, "play must set Playing=true")
	queued := <-queue
	assert.True(t, queued.Playing)
}

func TestCmdPlaylistPlay_BackgroundSlotResumesBackgroundPlaylist(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Slot = mediaslot.Background
	pls.Playing = false

	result, err := cmdPlaylistPlay(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{AdvArgs: bgAdvArgs(), Args: []string{}},
		Playlist: playlists.PlaylistController{Background: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	require.NotNil(t, result.Playlist)
	assert.True(t, result.Playlist.Playing)
	assert.Equal(t, mediaslot.Background, result.Playlist.Slot)
	queued := <-queue
	assert.Equal(t, mediaslot.Background, queued.Slot)
}

func TestQueuePlaylistUpdate_SetsSlotFromCommandArgs(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Slot = ""
	err := queuePlaylistUpdate(&platforms.CmdEnv{
		Cmd:      zapscript.Command{AdvArgs: bgAdvArgs()},
		Playlist: playlists.PlaylistController{Queue: queue},
	}, pls)
	require.NoError(t, err)
	queued := <-queue
	assert.Same(t, pls, queued)
	assert.Equal(t, mediaslot.Background, queued.Slot)
}

func TestCmdPlaylistNext_AdvancesIndex(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()

	result, err := cmdPlaylistNext(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	assert.Equal(t, 1, result.Playlist.Index)
	assert.Equal(t, 1, (<-queue).Index)
}

func TestCmdPlaylistNext_WrapsAround(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Index = 2

	result, err := cmdPlaylistNext(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Playlist.Index, "next from last item wraps to first")
	<-queue
}

func TestCmdPlaylistNext_NoActivePlaylist(t *testing.T) {
	t.Parallel()

	_, err := cmdPlaylistNext(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)},
	})
	require.Error(t, err)
}

func TestCmdPlaylistPrevious_DecrementsIndex(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Index = 2

	result, err := cmdPlaylistPrevious(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Playlist.Index)
	<-queue
}

func TestCmdPlaylistPrevious_WrapsAround(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Index = 0

	result, err := cmdPlaylistPrevious(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Playlist.Index, "previous from first item wraps to last")
	<-queue
}

func TestCmdPlaylistPrevious_NoActivePlaylist(t *testing.T) {
	t.Parallel()

	_, err := cmdPlaylistPrevious(nil, platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)},
	})
	require.Error(t, err)
}

func TestCmdPlaylistGoto_JumpsToIndex(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()

	// Goto is 1-based: "3" → index 2.
	result, err := cmdPlaylistGoto(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{"3"}},
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	assert.Equal(t, 2, result.Playlist.Index)
	<-queue
}

func TestCmdPlaylistGoto_SameIndexNoOp(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	pls.Index = 1

	result, err := cmdPlaylistGoto(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{"2"}},
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.False(t, result.PlaylistChanged, "already at target index — no-op")
}

func TestCmdPlaylistGoto_InvalidArg(t *testing.T) {
	t.Parallel()

	pls, queue := makePlaylistEnv()
	_, err := cmdPlaylistGoto(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{"notanumber"}},
		Playlist: playlists.PlaylistController{Active: pls, Queue: queue},
	})
	require.Error(t, err)
}

func TestCmdPlaylistGoto_NoActivePlaylist(t *testing.T) {
	t.Parallel()

	_, err := cmdPlaylistGoto(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{"1"}},
		Playlist: playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)},
	})
	require.Error(t, err)
}

func TestCmdPlaylistStop_BackgroundSlot(t *testing.T) {
	t.Parallel()

	// StopActiveLauncher is NOT mocked — a call to it would panic the test.
	mp := newPlaylistTestPlatform()

	pls, queue := makePlaylistEnv()
	pls.Slot = "background"

	result, err := cmdPlaylistStop(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{AdvArgs: bgAdvArgs()},
		Playlist: playlists.PlaylistController{Background: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	assert.Nil(t, result.Playlist)

	queued := <-queue
	require.NotNil(t, queued)
	assert.True(t, queued.Clear, "clear sentinel must be set for background stop")
}

func TestCmdPlaylistPause_BackgroundSlot(t *testing.T) {
	t.Parallel()

	// StopActiveLauncher is NOT mocked — a call to it would panic the test.
	mp := newPlaylistTestPlatform()

	pls, queue := makePlaylistEnv()
	pls.Slot = "background"
	pls.Playing = true

	result, err := cmdPlaylistPause(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{AdvArgs: bgAdvArgs()},
		Playlist: playlists.PlaylistController{Background: pls, Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	require.NotNil(t, result.Playlist)
	assert.False(t, result.Playlist.Playing, "paused playlist must have Playing=false")
	<-queue
}

func TestReadPlaylistFolder_ReturnsItems(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"alpha.nes", "beta.sfc", "gamma.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte{}, 0o600))
	}

	items, err := readPlaylistFolder(dir)
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "alpha", items[0].Name)
	assert.Equal(t, filepath.Join(dir, "alpha.nes"), items[0].ZapScript)
}

func TestReadPlaylistFolder_HiddenFilesSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte{}, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.rom"), []byte{}, 0o600))

	items, err := readPlaylistFolder(dir)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "visible", items[0].Name)
}

func TestReadPlaylistFolder_EmptyDir(t *testing.T) {
	t.Parallel()

	_, err := readPlaylistFolder(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid files found")
}

func TestCommandSlot_InvalidSlotReturnsError(t *testing.T) {
	t.Parallel()

	var aa zapscript.AdvArgs
	aa = aa.With(zapscript.KeySlot, "badvalue")
	_, err := cmdPlaylistNext(nil, platforms.CmdEnv{
		Cmd:      zapscript.Command{AdvArgs: aa},
		Playlist: playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "normalize media slot")
}

func TestLoadPlaylist_JSONArg(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)

	jsonArg := `{"id":"list-1","name":"My List",` +
		`"items":[{"name":"Track A","zapscript":"**test.a"},{"name":"Track B","zapscript":"**test.b"}]}`
	result, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonArg}},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	require.NotNil(t, result.Playlist)
	assert.Equal(t, "list-1", result.Playlist.ID)
	assert.Equal(t, "My List", result.Playlist.Name)
	require.Len(t, result.Playlist.Items, 2)
	assert.Equal(t, "Track A", result.Playlist.Items[0].Name)
	<-queue
}

// repeatAdvArgs returns AdvArgs with the repeat key set.
func repeatAdvArgs(repeat string) zapscript.AdvArgs {
	var aa zapscript.AdvArgs
	return aa.With(zapscript.KeyRepeat, repeat)
}

// jsonPlaylistArg is a minimal playlist JSON arg for repeat tests.
const jsonPlaylistArg = `{"id":"rpt","name":"Repeat Test",` +
	`"items":[{"name":"T1","zapscript":"**t1"},{"name":"T2","zapscript":"**t2"}]}`

func TestLoadPlaylist_RepeatOff_DefaultBehaviourNoLoop(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)
	result, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonPlaylistArg}},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Playlist)
	assert.False(t, result.Playlist.Loop, "absent repeat means no loop")
	assert.False(t, result.Playlist.LoopOne)
	<-queue
}

func TestLoadPlaylist_RepeatAll_SetsLoop(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)
	result, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonPlaylistArg}, AdvArgs: repeatAdvArgs("all")},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Playlist)
	assert.True(t, result.Playlist.Loop)
	assert.False(t, result.Playlist.LoopOne)
	<-queue
}

func TestLoadPlaylist_RepeatOne_SetsLoopOne(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)
	result, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonPlaylistArg}, AdvArgs: repeatAdvArgs("one")},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Playlist)
	assert.False(t, result.Playlist.Loop)
	assert.True(t, result.Playlist.LoopOne)
	<-queue
}

func TestLoadPlaylist_RepeatAllWithShuffle_BothApply(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)
	var aa zapscript.AdvArgs
	aa = aa.With(zapscript.KeyRepeat, "all")
	aa = aa.With(zapscript.KeyMode, "shuffle")
	result, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonPlaylistArg}, AdvArgs: aa},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Playlist)
	assert.True(t, result.Playlist.Loop, "repeat=all should set Loop")
	assert.False(t, result.Playlist.LoopOne)
	<-queue
}

func TestLoadPlaylist_InvalidRepeat_ReturnsError(t *testing.T) {
	t.Parallel()

	mp := newPlaylistTestPlatform()
	queue := make(chan *playlists.Playlist, 1)
	_, err := cmdPlaylistLoad(mp, platforms.CmdEnv{
		Cmd:      zapscript.Command{Args: []string{jsonPlaylistArg}, AdvArgs: repeatAdvArgs("forever")},
		Cfg:      &config.Instance{},
		Playlist: playlists.PlaylistController{Queue: queue},
	})
	require.Error(t, err, "invalid repeat value must be rejected")
}

func TestReadPlaylistFolder_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := readPlaylistFolder("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no playlist path specified")
}

func TestReadPlaylistFolder_NonexistentPath(t *testing.T) {
	t.Parallel()

	_, err := readPlaylistFolder(filepath.Join("nonexistent", "path", "12345"))
	require.Error(t, err)
}
