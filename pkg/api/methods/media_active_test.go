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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleActiveMedia_WithZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		activeMedia       *models.ActiveMedia
		setupMock         func(*helpers.MockMediaDBI)
		expectedZapScript string
		expectNil         bool
	}{
		{
			name:              "returns nil when no active media",
			activeMedia:       nil,
			setupMock:         nil,
			expectedZapScript: "",
			expectNil:         true,
		},
		{
			name: "returns zapScript with year when available",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Super Mario World.sfc",
				"Super Mario World",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "snes", "/roms/snes/Super Mario World.sfc").
					Return([]database.TagInfo{{Type: "year", Tag: "1990"}}, nil)
			},
			expectedZapScript: "@snes/Super Mario World (year:1990)",
			expectNil:         false,
		},
		{
			name: "returns zapScript without year when not found",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Unknown Game.sfc",
				"Unknown Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "snes", "/roms/snes/Unknown Game.sfc").
					Return([]database.TagInfo{}, nil)
			},
			expectedZapScript: "@snes/Unknown Game",
			expectNil:         false,
		},
		{
			name: "returns zapScript without year on error",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Error Game.sfc",
				"Error Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "snes", "/roms/snes/Error Game.sfc").
					Return([]database.TagInfo(nil), errors.New("db error"))
			},
			expectedZapScript: "@snes/Error Game",
			expectNil:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockMediaDB := helpers.NewMockMediaDBI()

			if tt.setupMock != nil {
				tt.setupMock(mockMediaDB)
			}
			mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
				Return([]database.MediaPathID{}, nil).Maybe()

			// Create state and set active media
			appState, _ := state.NewState(mockPlatform, "test-boot-uuid")
			if tt.activeMedia != nil {
				appState.SetActiveMedia(tt.activeMedia)
			}

			env := requests.RequestEnv{
				Context: context.Background(),
				Database: &database.Database{
					MediaDB: mockMediaDB,
				},
				Platform: mockPlatform,
				State:    appState,
			}

			result, err := HandleActiveMedia(env)
			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				response, ok := result.(models.ActiveMediaResponse)
				require.True(t, ok, "Should return ActiveMediaResponse")
				assert.Equal(t, tt.expectedZapScript, response.ZapScript)
				assert.Equal(t, tt.activeMedia.SystemID, response.SystemID)
				assert.Equal(t, tt.activeMedia.Name, response.Name)
				assert.Equal(t, tt.activeMedia.Path, response.Path)
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleMedia_WithActiveMediaZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		activeMedia       *models.ActiveMedia
		setupMock         func(*helpers.MockMediaDBI)
		expectedZapScript string
		expectedSystemID  string
		expectActiveMedia bool
	}{
		{
			name:              "returns empty active array when no active media",
			activeMedia:       nil,
			setupMock:         nil,
			expectedZapScript: "",
			expectedSystemID:  "",
			expectActiveMedia: false,
		},
		{
			name: "returns zapScript with year in Active array when available",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Super Mario World.sfc",
				"Super Mario World",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				// HandleMedia uses system.ID from GetSystemMetadata which returns uppercase "SNES"
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", "/roms/snes/Super Mario World.sfc").
					Return([]database.TagInfo{{Type: "year", Tag: "1990"}}, nil)
			},
			expectedZapScript: "@SNES/Super Mario World (year:1990)",
			expectedSystemID:  "SNES",
			expectActiveMedia: true,
		},
		{
			name: "returns zapScript without year in Active when not found",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Unknown Game.sfc",
				"Unknown Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				// HandleMedia uses system.ID from GetSystemMetadata which returns uppercase "SNES"
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", "/roms/snes/Unknown Game.sfc").
					Return([]database.TagInfo{}, nil)
			},
			expectedZapScript: "@SNES/Unknown Game",
			expectedSystemID:  "SNES",
			expectActiveMedia: true,
		},
		{
			name: "returns zapScript without year in Active on db error",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Error Game.sfc",
				"Error Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				// HandleMedia uses system.ID from GetSystemMetadata which returns uppercase "SNES"
				m.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", "/roms/snes/Error Game.sfc").
					Return([]database.TagInfo(nil), errors.New("db error"))
			},
			expectedZapScript: "@SNES/Error Game",
			expectedSystemID:  "SNES",
			expectActiveMedia: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockMediaDB := helpers.NewMockMediaDBI()

			if tt.setupMock != nil {
				tt.setupMock(mockMediaDB)
			}
			mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
				Return([]database.MediaPathID{}, nil).Maybe()

			// Standard mocks needed for HandleMedia to work
			// GetOptimizationStatus is always called
			mockMediaDB.On("GetOptimizationStatus").Return("", nil)
			// These may not be called if another parallel test is running indexing
			// (which sets global statusInstance.indexing = true)
			mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil).Maybe()
			mockMediaDB.On("GetTotalMediaCount").Return(100, nil).Maybe()

			// Clear indexing status
			ClearIndexingStatus()

			// Create state and set active media
			appState, _ := state.NewState(mockPlatform, "test-boot-uuid")
			if tt.activeMedia != nil {
				appState.SetActiveMedia(tt.activeMedia)
			}

			env := requests.RequestEnv{
				Context: context.Background(),
				Database: &database.Database{
					MediaDB: mockMediaDB,
				},
				Platform: mockPlatform,
				State:    appState,
			}

			result, err := HandleMedia(env)
			require.NoError(t, err)

			response, ok := result.(models.MediaResponse)
			require.True(t, ok, "Should return MediaResponse")

			if tt.expectActiveMedia {
				require.Len(t, response.Active, 1, "Should have one active media")
				assert.Equal(t, tt.expectedZapScript, response.Active[0].ZapScript)
				assert.Equal(t, tt.expectedSystemID, response.Active[0].SystemID)
				assert.Equal(t, tt.activeMedia.Name, response.Active[0].Name)
				assert.Equal(t, tt.activeMedia.Path, response.Active[0].Path)
			} else {
				assert.Empty(t, response.Active, "Active array should be empty")
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleActiveMedia_WithMediaIDAndRelativePath(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	rootDir := filepath.Join(string(filepath.Separator), "mock", "roms")
	mediaPath := filepath.Join(rootDir, "NES", "game.nes")
	relPath := filepath.ToSlash(filepath.Join("NES", "game.nes"))

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "nes-launcher", SystemID: "NES", Folders: []string{"NES"}},
	})

	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	defer appState.StopService()
	drainNotifications(t, ns)
	appState.SetActiveMedia(models.NewActiveMedia("NES", "NES", mediaPath, "Game", "test-launcher"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "NES", mediaPath).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: mediaPath, DBID: 42}}, nil)

	env := requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{MediaDB: mockMediaDB},
		Platform:      mockPlatform,
		State:         appState,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Equal(t, int64(42), resp.MediaID)
	require.NotNil(t, resp.RelPath)
	assert.Equal(t, relPath, *resp.RelPath)
	assert.Equal(t, "@NES/Game", resp.ZapScript)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMedia_WithActiveMediaIDAndRelativePath(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	rootDir := filepath.Join(string(filepath.Separator), "mock", "roms")
	mediaPath := filepath.Join(rootDir, "NES", "game.nes")
	relPath := filepath.ToSlash(filepath.Join("NES", "game.nes"))

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "nes-launcher", SystemID: "NES", Folders: []string{"NES"}},
	})

	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	defer appState.StopService()
	drainNotifications(t, ns)
	appState.SetActiveMedia(models.NewActiveMedia("NES", "NES", mediaPath, "Game", "test-launcher"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "NES", mediaPath).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{mediaPath}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: mediaPath, DBID: 42}}, nil)
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
	mockMediaDB.On("GetTotalMediaCount").Return(100, nil)
	ClearIndexingStatus()

	env := requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{MediaDB: mockMediaDB},
		Platform:      mockPlatform,
		State:         appState,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	require.Len(t, resp.Active, 1)
	assert.Equal(t, int64(42), resp.Active[0].MediaID)
	require.NotNil(t, resp.Active[0].RelPath)
	assert.Equal(t, relPath, *resp.Active[0].RelPath)
	assert.Equal(t, "@NES/Game", resp.Active[0].ZapScript)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleActiveMedia_WithLauncherControls(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	activeMedia := models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher")
	activeMedia.LauncherControls = []string{"save_state"}
	st.SetActiveMedia(activeMedia)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindSystemBySystemID", mock.Anything).
		Return(database.System{}, errors.New("not found")).Maybe()
	mockMediaDB.On("FindMediaBySystemAndPaths", mock.Anything, mock.Anything, mock.Anything).
		Return(map[string]database.Media{}, nil).Maybe()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Equal(t, []string{"save_state"}, resp.LauncherControls)
}

func TestHandleActiveMedia_WithoutLauncherControls(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "/game.nes", "Game", "test-launcher"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.TagInfo{}, nil)
	mockMediaDB.On("FindSystemBySystemID", mock.Anything).
		Return(database.System{}, errors.New("not found")).Maybe()
	mockMediaDB.On("FindMediaBySystemAndPaths", mock.Anything, mock.Anything, mock.Anything).
		Return(map[string]database.Media{}, nil).Maybe()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Nil(t, resp.LauncherControls)
}

func TestHandleMedia_WithBackgroundMedia(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	background := models.NewActiveMedia(
		"Audio", "Audio", "song.mp3", "Song", platforms.NativeAudioLauncherID,
	)
	st.SetBackgroundMedia(background)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
	mockMediaDB.On("GetTotalMediaCount").Return(0, nil)
	ClearIndexingStatus()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	require.Len(t, resp.Active, 1, "only background media")
	assert.Equal(t, mediaslot.Background, resp.Active[0].Slot)
	assert.Equal(t, "song.mp3", resp.Active[0].Path)
	assert.Equal(t, "Song", resp.Active[0].Name)
	assert.Equal(t, "@Audio/Song", resp.Active[0].ZapScript)

	mockMediaDB.AssertExpectations(t)
}

// TestHandleActiveMedia_SlotFieldSetOnPrimaryResponse verifies that the slot
// field is always populated on the returned ActiveMediaResponse.
func TestHandleActiveMedia_SlotFieldSetOnPrimaryResponse(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	gamePath := filepath.Join(string(os.PathSeparator), "game.nes")
	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", gamePath, "Game", "test-launcher"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Equal(t, mediaslot.Primary, resp.Slot)
}

// TestHandleActiveMedia_BackgroundSlotParam verifies that passing slot=background
// returns background media and sets the correct slot field.
func TestHandleActiveMedia_BackgroundSlotParam(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	bgMedia := models.NewActiveMedia("Audio", "Audio", "song.mp3", "Song", platforms.NativeAudioLauncherID)
	st.SetBackgroundMedia(bgMedia)

	params, _ := json.Marshal(map[string]string{"slot": "background"})
	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: helpers.NewMockMediaDBI()},
		Params:   json.RawMessage(params),
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Equal(t, mediaslot.Background, resp.Slot)
	assert.Equal(t, "song.mp3", resp.Path)
	assert.Equal(t, "Song", resp.Name)
}

// TestHandleActiveMedia_BackgroundSlotReturnsNilWhenNoneActive verifies that
// requesting background slot when there is no background media returns nil.
func TestHandleActiveMedia_BackgroundSlotReturnsNilWhenNoneActive(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	params, _ := json.Marshal(map[string]string{"slot": "bg"})
	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: helpers.NewMockMediaDBI()},
		Params:   json.RawMessage(params),
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestHandleActiveMedia_NativeAudioCarriesPosition verifies that
// positionMs/durationMs are populated for native-audio entries.
func TestHandleActiveMedia_NativeAudioCarriesPosition(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID))

	mgr := newStubPlaybackManager(mediaslot.Primary, audio.PlaybackState{
		Position: 30 * time.Second,
		Duration: 3 * time.Minute,
	})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()

	env := requests.RequestEnv{
		Context:         context.Background(),
		State:           st,
		Database:        &database.Database{MediaDB: mockMediaDB},
		PlaybackManager: mgr,
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	require.NotNil(t, resp.PositionMs)
	require.NotNil(t, resp.DurationMs)
	assert.Equal(t, (30 * time.Second).Milliseconds(), *resp.PositionMs)
	assert.Equal(t, (3 * time.Minute).Milliseconds(), *resp.DurationMs)
}

// TestHandleActiveMedia_NonNativeLauncherOmitsPosition verifies that
// positionMs/durationMs are absent for non-native-audio launchers.
func TestHandleActiveMedia_NonNativeLauncherOmitsPosition(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	gamePath := filepath.Join(string(os.PathSeparator), "game.nes")
	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", gamePath, "Game", "mister"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()

	mgr := newStubPlaybackManager(mediaslot.Primary, audio.PlaybackState{
		Position: 30 * time.Second,
		Duration: 3 * time.Minute,
	})

	env := requests.RequestEnv{
		Context:         context.Background(),
		State:           st,
		Database:        &database.Database{MediaDB: mockMediaDB},
		PlaybackManager: mgr,
	}

	result, err := HandleActiveMedia(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	resp, ok := result.(models.ActiveMediaResponse)
	require.True(t, ok)
	assert.Nil(t, resp.PositionMs, "non-native-audio launcher must not expose position")
	assert.Nil(t, resp.DurationMs, "non-native-audio launcher must not expose duration")
}

// TestHandleMedia_PlaylistsIncludedInResponse verifies that active playlists for
// both slots are included in the media response.
func TestHandleMedia_PlaylistsIncludedInResponse(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	activePL := &playlists.Playlist{
		ID:   "primary-pl",
		Name: "Game Playlist",
		Slot: mediaslot.Primary,
		Items: []playlists.PlaylistItem{
			{Name: "Game A", ZapScript: "**launch:a.nes"},
			{Name: "Game B", ZapScript: "**launch:b.nes"},
		},
		Index: 0,
	}
	bgPL := &playlists.Playlist{
		ID:      "bg-pl",
		Name:    "Background Music",
		Slot:    mediaslot.Background,
		Items:   []playlists.PlaylistItem{{Name: "Song 1", ZapScript: "**launch:1.mp3"}},
		Index:   0,
		Playing: true,
		Loop:    true,
	}
	st.SetActivePlaylist(activePL)
	st.SetBackgroundPlaylist(bgPL)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
	mockMediaDB.On("GetTotalMediaCount").Return(0, nil)
	ClearIndexingStatus()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	require.Len(t, resp.Playlists, 2)

	var primary, background models.PlaylistState
	for _, p := range resp.Playlists {
		switch p.Slot {
		case mediaslot.Primary:
			primary = p
		case mediaslot.Background:
			background = p
		}
	}

	assert.Equal(t, "primary-pl", primary.ID)
	assert.Equal(t, 2, primary.Total)
	assert.Equal(t, "none", primary.Repeat)

	assert.Equal(t, "bg-pl", background.ID)
	assert.Equal(t, 1, background.Total)
	assert.Equal(t, "all", background.Repeat)
	assert.True(t, background.Playing)
}

// TestHandleMedia_EmptyPlaylistsOmittedFromResponse verifies that the playlists
// field is absent when no playlists are active.
func TestHandleMedia_EmptyPlaylistsOmittedFromResponse(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
	mockMediaDB.On("GetTotalMediaCount").Return(0, nil)
	ClearIndexingStatus()

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Playlists)
}

// TestHandleMedia_NativeAudioActiveEntryCarriesPosition verifies that the active[]
// entry for native-audio primary media includes positionMs/durationMs.
func TestHandleMedia_NativeAudioActiveEntryCarriesPosition(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia("Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID))

	mgr := newStubPlaybackManager(mediaslot.Primary, audio.PlaybackState{
		Position: 15 * time.Second,
		Duration: 4 * time.Minute,
	})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()
	mockMediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
	mockMediaDB.On("GetTotalMediaCount").Return(0, nil)
	ClearIndexingStatus()

	env := requests.RequestEnv{
		Context:         context.Background(),
		State:           st,
		Database:        &database.Database{MediaDB: mockMediaDB},
		PlaybackManager: mgr,
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaResponse)
	require.True(t, ok)
	require.Len(t, resp.Active, 1)
	require.NotNil(t, resp.Active[0].PositionMs)
	require.NotNil(t, resp.Active[0].DurationMs)
	assert.Equal(t, (15 * time.Second).Milliseconds(), *resp.Active[0].PositionMs)
	assert.Equal(t, (4 * time.Minute).Milliseconds(), *resp.Active[0].DurationMs)
}

func TestHandleUpdateActiveMedia_ClearMedia(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	st.SetActiveMedia(models.NewActiveMedia(
		"NES", "NES", filepath.Join(string(os.PathSeparator), "game.nes"), "Game", "launcher",
	))

	env := requests.RequestEnv{
		Context: context.Background(),
		State:   st,
		Params:  json.RawMessage{},
	}

	result, err := HandleUpdateActiveMedia(env)
	require.NoError(t, err)
	_, ok := result.(NoContent)
	require.True(t, ok)
	assert.Nil(t, st.ActiveMedia(), "active media must be cleared")
}

func TestHandleUpdateActiveMedia_SetMedia(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mediaPath := filepath.Join(string(os.PathSeparator), "game.nes")
	params, _ := json.Marshal(map[string]string{
		"systemId":  "NES",
		"mediaPath": mediaPath,
		"mediaName": "My Game",
	})
	env := requests.RequestEnv{
		Context: context.Background(),
		State:   st,
		Params:  json.RawMessage(params),
	}

	result, err := HandleUpdateActiveMedia(env)
	require.NoError(t, err)
	_, ok := result.(NoContent)
	require.True(t, ok)

	active := st.ActiveMedia()
	require.NotNil(t, active)
	assert.Equal(t, "NES", active.SystemID)
	assert.Equal(t, mediaPath, active.Path)
	assert.Equal(t, "My Game", active.Name)
}

func TestHandleUpdateActiveMedia_InvalidSystem(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mediaPath := filepath.Join(".", "game")
	params, _ := json.Marshal(map[string]string{
		"systemId":  "UNKNOWN_SYSTEM_XYZ_12345",
		"mediaPath": mediaPath,
		"mediaName": "Game",
	})
	env := requests.RequestEnv{
		Context: context.Background(),
		State:   st,
		Params:  json.RawMessage(params),
	}

	_, err := HandleUpdateActiveMedia(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "looking up system")
}
