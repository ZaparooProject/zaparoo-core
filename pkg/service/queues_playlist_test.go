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
	"path/filepath"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupPlaylistTestEnv(t *testing.T) *ServiceContext {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("ReturnToMenu").Return(nil).Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	cfg := &config.Instance{}

	st, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	return &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan *tokens.Token, 10),
		PlaylistQueue:       make(chan *playlists.Playlist, 10),
	}
}

func makeServicePlaylist() *playlists.Playlist {
	items := []playlists.PlaylistItem{
		{Name: "Item 1", ZapScript: "**test1"},
		{Name: "Item 2", ZapScript: "**test2"},
		{Name: "Item 3", ZapScript: "**test3"},
	}
	return playlists.NewPlaylist("id", "name", items)
}

type servicePlaybackRecorder struct {
	states  map[string]audio.PlaybackState
	played  []string
	stopped []string
	paused  []string
	resumed []string
}

func (r *servicePlaybackRecorder) Play(slot, _ string, _ audio.PlaybackOptions) error {
	r.played = append(r.played, slot)
	return nil
}

func (r *servicePlaybackRecorder) Stop(slot string) error {
	r.stopped = append(r.stopped, slot)
	return nil
}

func (r *servicePlaybackRecorder) Pause(slot string) error {
	r.paused = append(r.paused, slot)
	return nil
}

func (r *servicePlaybackRecorder) Resume(slot string) error {
	r.resumed = append(r.resumed, slot)
	return nil
}

func (*servicePlaybackRecorder) TogglePause(string) error {
	return nil
}

func (*servicePlaybackRecorder) Seek(string, time.Duration) error {
	return nil
}

func (r *servicePlaybackRecorder) State(slot string) audio.PlaybackState {
	if r.states == nil {
		return audio.PlaybackState{}
	}
	return r.states[slot]
}

func TestRunTokenZapScript_ClearsPlaylistOnMediaChange(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**launch.system:menu",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		assert.Nil(t, pls, "playlist queue should receive nil to clear the active playlist")
	case <-time.After(time.Second):
		t.Fatal("expected nil on playlist queue but nothing was sent")
	}
}

func TestRunTokenZapScript_ReturnsWhenPlaylistClearBlockedByShutdown(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	cfg := &config.Instance{}
	st, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	mockPlatform.On("ReturnToMenu").Run(func(mock.Arguments) {
		st.StopService()
	}).Return(nil)

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()
	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan *tokens.Token, 10),
	}

	plq := make(chan *playlists.Playlist)
	plsc := playlists.PlaylistController{Queue: plq}
	token := tokens.Token{
		Text:     "**launch.system:menu",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)

	require.ErrorContains(t, err, "service shutting down")
	select {
	case pls := <-plq:
		t.Fatalf("playlist queue should remain blocked during shutdown, got: %v", pls)
	default:
	}
}

func TestRunTokenZapScript_SkipsPlaylistClearForPlaylistSource(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**launch.system:menu",
		ScanTime: time.Now(),
		Source:   tokens.SourcePlaylist,
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		t.Fatalf("playlist queue should NOT receive anything for playlist-sourced tokens, got: %v", pls)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing sent
	}
}

func TestRunTokenZapScript_NoPlaylistClearForNonMediaCommand(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.Platform.(*mocks.MockPlatform).On("KeyboardPress", "{f2}").Return(nil)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**input.keyboard:{f2}",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		t.Fatalf("playlist queue should NOT receive anything for non-media commands, got: %v", pls)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing sent
	}
}

func TestHandlePlaylist_BackgroundSlotUpdatesBackgroundStateOnly(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	active := makeServicePlaylist()
	svc.State.SetActivePlaylist(active)
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	background.Playing = false

	handlePlaylist(svc, background, nil)

	assert.Same(t, active, svc.State.GetActivePlaylist(), "background update must not replace active playlist")
	assert.Same(t, background, svc.State.GetBackgroundPlaylist())
	assert.Equal(t, mediaslot.Background, background.Slot)
}

func TestHandlePlaylist_BackgroundClearStopsPlaybackAndClearsBackgroundOnly(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeServicePlaylist()
	svc.State.SetActivePlaylist(active)
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	svc.State.SetBackgroundPlaylist(background)
	svc.State.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))
	require.NotNil(t, svc.State.BackgroundMedia())

	handlePlaylist(svc, &playlists.Playlist{Slot: mediaslot.Background, Clear: true}, nil)

	assert.Same(t, active, svc.State.GetActivePlaylist(), "background clear must not clear active playlist")
	assert.Nil(t, svc.State.GetBackgroundPlaylist())
	assert.Nil(t, svc.State.BackgroundMedia())
	assert.Equal(t, []string{mediaslot.Background}, recorder.stopped)
}

func TestHandlePlaylist_BackgroundPausePausesPlayback(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	background.Playing = true
	svc.State.SetBackgroundPlaylist(background)

	paused := playlists.Pause(*background)
	handlePlaylist(svc, paused, nil)

	assert.Equal(t, []string{mediaslot.Background}, recorder.paused)
	assert.False(t, svc.State.GetBackgroundPlaylist().Playing)
}

func TestHandlePlaylist_BackgroundPlayResumesPausedPlayback(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{states: map[string]audio.PlaybackState{
		mediaslot.Background: {Path: "track.mp3", Paused: true},
	}}
	svc.PlaybackManager = recorder
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	background.Playing = false
	svc.State.SetBackgroundPlaylist(background)

	playing := playlists.Play(*background)
	handlePlaylist(svc, playing, nil)

	assert.Equal(t, []string{mediaslot.Background}, recorder.resumed)
	assert.Empty(t, recorder.played)
	assert.True(t, svc.State.GetBackgroundPlaylist().Playing)
}

func TestHandlePlaylist_BackgroundPlayRelaunchesWhenNoPlaybackSource(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	background.Playing = false
	svc.State.SetBackgroundPlaylist(background)

	playing := playlists.Play(*background)
	handlePlaylist(svc, playing, nil)

	assert.Empty(t, recorder.resumed)
	assert.True(t, svc.State.GetBackgroundPlaylist().Playing)
}

func TestStopNativePlaybackBeforePrimaryCommandStopsAndClearsPrimaryNativeAudio(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	svc.State.SetActiveMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))

	err := stopNativePlaybackBeforePrimaryCommand(svc, gozapscript.Command{
		Name: gozapscript.ZapScriptCmdLaunch,
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{mediaslot.Primary}, recorder.stopped)
	assert.Nil(t, svc.State.ActiveMedia())
}

func TestStopNativePlaybackBeforePrimaryCommandStopsPrimaryPlaylistStop(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	svc.State.SetActiveMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))

	err := stopNativePlaybackBeforePrimaryCommand(svc, gozapscript.Command{
		Name: gozapscript.ZapScriptCmdPlaylistStop,
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{mediaslot.Primary}, recorder.stopped)
	assert.Nil(t, svc.State.ActiveMedia())
}

func TestStopNativePlaybackBeforePrimaryCommandSkipsBackgroundLaunch(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	svc.State.SetActiveMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))

	err := stopNativePlaybackBeforePrimaryCommand(svc, gozapscript.Command{
		Name:    gozapscript.ZapScriptCmdLaunch,
		AdvArgs: gozapscript.NewAdvArgs(map[string]string{string(gozapscript.KeySlot): mediaslot.Background}),
	}, nil)

	require.NoError(t, err)
	assert.Empty(t, recorder.stopped)
	assert.NotNil(t, svc.State.ActiveMedia())
}

func TestRunTokenZapScript_BackgroundLaunchPreservesPrimaryPlaylist(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)

	path := filepath.Join(t.TempDir(), "track.mp3")
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.Slot == mediaslot.Background
		})).Return(nil).Once()

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}
	token := tokens.Token{
		Text:     "**launch:" + path + "?slot=background",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		t.Fatalf("background launch must not clear the primary playlist, got: %v", pls)
	default:
	}
	mockPlatform.AssertNumberOfCalls(t, "LaunchMedia", 1)
}

func TestRunTokenZapScript_BackgroundPlaylistCommandDoesNotBecomeCurrent(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	primary := makeServicePlaylist()
	primary.Slot = mediaslot.Primary
	background := makeServicePlaylist()
	background.Slot = mediaslot.Background
	plq := make(chan *playlists.Playlist, 2)
	plsc := playlists.PlaylistController{Active: primary, Background: background, Current: primary, Queue: plq}
	token := tokens.Token{
		Text:     "**playlist.next?slot=background||**playlist.next",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	first := <-plq
	second := <-plq
	assert.Equal(t, mediaslot.Background, first.Slot)
	assert.Equal(t, 1, first.Index)
	assert.Equal(t, mediaslot.Primary, second.Slot)
	assert.Equal(t, 1, second.Index)
}

func TestRunTokenZapScript_BackgroundLaunchSkipsSoftwareToken(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)

	const readerID = "mock-removable-reader"
	readerPath := filepath.Join(string(filepath.Separator), "dev", "mock-device")
	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
	mockReader.On("Path").Return(readerPath).Maybe()
	mockReader.On("Capabilities").Return([]readers.Capability{
		readers.CapabilityRemovable,
	}).Maybe()
	mockReader.On("ReaderID").Return(readerID).Maybe()
	svc.State.SetReader(mockReader)

	path := filepath.Join(t.TempDir(), "track.mp3")
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB,
		mock.Anything).Return(nil).Once()

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 10)}
	token := tokens.Token{
		Text:     "**launch:" + path + "?slot=background",
		ScanTime: time.Now(),
		ReaderID: readerID,
	}

	err := runTokenZapScript(svc, token, plsc, nil, false)
	require.NoError(t, err)

	select {
	case st := <-svc.LaunchSoftwareQueue:
		t.Fatalf("background launch must not update the software token, got: %v", st)
	default:
	}
	mockPlatform.AssertNumberOfCalls(t, "LaunchMedia", 1)
}

// statefulPlaybackStub tracks per-slot playing state so tests can observe the
// pause/resume ordering of the launch path.
type statefulPlaybackStub struct {
	playing map[string]bool
}

func (s *statefulPlaybackStub) Play(slot, _ string, _ audio.PlaybackOptions) error {
	s.playing[slot] = true
	return nil
}

func (s *statefulPlaybackStub) Stop(slot string) error {
	delete(s.playing, slot)
	return nil
}

func (s *statefulPlaybackStub) Pause(slot string) error {
	s.playing[slot] = false
	return nil
}

func (s *statefulPlaybackStub) Resume(slot string) error {
	s.playing[slot] = true
	return nil
}

func (s *statefulPlaybackStub) TogglePause(slot string) error {
	s.playing[slot] = !s.playing[slot]
	return nil
}

func (*statefulPlaybackStub) Seek(string, time.Duration) error {
	return nil
}

func (s *statefulPlaybackStub) State(slot string) audio.PlaybackState {
	return audio.PlaybackState{Playing: s.playing[slot]}
}

func TestGameReplacingPrimaryAudioKeepsBackgroundPaused(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	pm := &statefulPlaybackStub{playing: map[string]bool{
		mediaslot.Background: false, // auto-paused by an earlier primary launch
	}}
	svc.PlaybackManager = pm
	svc.State.SetBackgroundAutoPaused(true)
	svc.State.SetOnMediaStopHook(func() { resumeBackgroundAfterMediaStop(svc) })
	svc.State.SetActiveMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))

	// A game token replaces the playing primary audio: the native audio stop fires
	// the media-stop hook (resuming auto-paused background), and the pause step
	// must then run after that resume so the background ends up paused again.
	launchCmd := gozapscript.Command{
		Name:    gozapscript.ZapScriptCmdLaunch,
		AdvArgs: gozapscript.NewAdvArgs(map[string]string{}),
	}
	require.NoError(t, stopNativePlaybackBeforePrimaryCommand(svc, launchCmd, nil))
	require.NoError(t, pauseBackgroundForPrimaryLaunch(svc, launchCmd, nil))

	assert.False(t, pm.playing[mediaslot.Background], "background must be paused while the game runs")
	assert.True(t, svc.State.BackgroundAutoPaused(), "auto-pause flag must be re-armed for resume on game exit")
	assert.Nil(t, svc.State.ActiveMedia())
}

func TestHandlePlaylist_InvalidSlotIgnored(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	active := makeServicePlaylist()
	background := makeServicePlaylist()
	svc.State.SetActivePlaylist(active)
	svc.State.SetBackgroundPlaylist(background)

	handlePlaylist(svc, &playlists.Playlist{Slot: "badslot", Clear: true}, nil)

	assert.Same(t, active, svc.State.GetActivePlaylist())
	assert.Same(t, background, svc.State.GetBackgroundPlaylist())
}
