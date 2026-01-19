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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
