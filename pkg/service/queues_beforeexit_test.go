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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// exitRecorder records the order of before_exit runs against the platform calls
// that actually stop or replace media.
type exitRecorder struct {
	events []string
	mu     syncutil.Mutex
}

func (r *exitRecorder) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *exitRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *exitRecorder) count(event string) int {
	n := 0
	for _, e := range r.snapshot() {
		if e == event {
			n++
		}
	}
	return n
}

type exitTestEnv struct {
	svc      *ServiceContext
	platform *mocks.MockPlatform
	rec      *exitRecorder
	gamePath string
	// publishOnLaunch makes a mocked LaunchMedia publish active media the way a
	// real launch does, so a launch from inside before_exit moves the active
	// media generation the stop paths check.
	publishOnLaunch bool
}

// publishActiveMediaOnLaunch makes every subsequent mocked launch publish new
// active media, which is what a real launch does and what the generation guard
// on the stop paths keys off.
func (e *exitTestEnv) publishActiveMediaOnLaunch() {
	e.publishOnLaunch = true
}

// setupExitTestEnv wires a service whose NES before_exit hook presses a key,
// recording "before_exit" alongside "launch" and "stop" platform calls.
func setupExitTestEnv(t *testing.T, beforeExit string) *exitTestEnv {
	t.Helper()

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[zapscript.input]
mode = "unrestricted"`))
	cfg.SetSystemDefaults([]config.SystemsDefault{{System: "NES", BeforeExit: beforeExit}})

	rec := &exitRecorder{}
	env := &exitTestEnv{rec: rec}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()
	mockPlatform.On("KeyboardPress", mock.Anything).Run(func(_ mock.Arguments) {
		rec.record("before_exit")
	}).Return(nil).Maybe()
	mockPlatform.On("ReturnToMenu").Run(func(_ mock.Arguments) {
		rec.record("stop")
	}).Return(nil).Maybe()
	mockPlatform.On("StopActiveLauncher", mock.Anything).Run(func(_ mock.Arguments) {
		rec.record("stop")
	}).Return(nil).Maybe()
	launched := 0
	mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Run(func(args mock.Arguments) {
		rec.record("launch")
		if !env.publishOnLaunch || env.svc == nil {
			return
		}
		launched++
		media := models.NewActiveMedia(
			"NES", "NES", fmt.Sprintf("hook-%d.nes", launched),
			fmt.Sprintf("Hook %d", launched), "test-launcher")
		// Publish through the launch's own publisher, as a real launcher does.
		// State.SetActiveMedia would take restore access a second time on this
		// goroutine, which already holds it via AcquireMediaLaunch.
		if opts, ok := args.Get(4).(*platforms.LaunchOptions); ok && opts != nil &&
			opts.ActiveMediaPublisher != nil {
			opts.ActiveMediaPublisher(media)
			return
		}
		env.svc.State.SetActiveMedia(media)
	}).Return(nil).Maybe()

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

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

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan *tokens.Token, 10),
		PlaylistQueue:       make(chan *playlists.Playlist, 10),
	}
	st.SetBeforeExitHook(func() { runBeforeExitHook(svc) })

	gamePath := filepath.Join(t.TempDir(), "next.nes")
	require.NoError(t, os.WriteFile(gamePath, []byte("rom"), 0o600))

	env.svc = svc
	env.platform = mockPlatform
	env.gamePath = gamePath
	return env
}

// setNESMediaRunning marks an NES game as the active primary media, which is
// what before_exit keys off.
func (e *exitTestEnv) setNESMediaRunning() {
	e.svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "test-launcher"))
}

func (e *exitTestEnv) run(t *testing.T, script string) error {
	t.Helper()
	return runTokenZapScript(e.svc, tokens.Token{
		UID:      "card",
		Text:     script,
		ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 10)}, nil, false)
}

const (
	keypressHook = "**input.keyboard:{f2}"
	// An unknown command, so the script errors without any platform side effect.
	failingScript = "**definitely.not.a.command:x"
)

// Tapping a second card replaces the running game. This is the case the docs
// promised and the code never did.
func TestRunTokenZapScript_ReplaceLaunchRunsBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.setNESMediaRunning()

	require.NoError(t, env.run(t, "**launch:"+env.gamePath))

	assert.Equal(t, []string{"before_exit", "launch"}, env.rec.snapshot(),
		"before_exit must run before the replacement launch")
}

func TestRunTokenZapScript_StopCommandRunsBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.setNESMediaRunning()

	require.NoError(t, env.run(t, "**stop"))

	assert.Equal(t, []string{"before_exit", "stop"}, env.rec.snapshot(),
		"before_exit must run before returning to the menu")
}

func TestRunTokenZapScript_BackgroundSlotLaunchDoesNotRunBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.setNESMediaRunning()

	// A background launch does not touch primary media, so nothing is exiting.
	_ = env.run(t, "**launch:"+env.gamePath+"?slot=background")

	assert.Zero(t, env.rec.count("before_exit"),
		"background slot media must not trigger the primary before_exit hook")
}

func TestRunTokenZapScript_NoActiveMediaDoesNotRunBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)

	require.NoError(t, env.run(t, "**launch:"+env.gamePath))

	assert.Zero(t, env.rec.count("before_exit"),
		"nothing is exiting when no media is running")
}

// A before_exit script that itself stops media re-enters the same path. It must
// run once and terminate rather than recursing.
func TestRunTokenZapScript_BeforeExitScriptDoesNotRecurse(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, "**stop")
	env.setNESMediaRunning()

	done := make(chan error, 1)
	go func() { done <- env.run(t, "**launch:"+env.gamePath) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("before_exit recursion hung the launch")
	}

	assert.Equal(t, 1, env.rec.count("launch"), "the outer launch should still happen exactly once")
}

// A broken hook must not strand the user with media that will not change.
func TestRunTokenZapScript_BeforeExitFailureDoesNotAbortLaunch(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, failingScript)
	env.setNESMediaRunning()

	require.NoError(t, env.run(t, "**launch:"+env.gamePath),
		"a failing before_exit hook must not block the launch")
	assert.Equal(t, 1, env.rec.count("launch"))
}

// before_media_start is the veto gate. A launch it blocks never happens, so
// nothing exits and before_exit must not fire.
func TestRunTokenZapScript_BlockedBeforeMediaStartSuppressesBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	require.NoError(t, env.svc.Config.LoadTOML(
		`[launchers]
before_media_start = "`+failingScript+`"`))
	env.setNESMediaRunning()

	err := env.run(t, "**launch:"+env.gamePath)

	require.Error(t, err, "a failing before_media_start hook must block the launch")
	assert.Zero(t, env.rec.count("before_exit"),
		"before_exit must not fire for a launch that was vetoed")
	assert.Zero(t, env.rec.count("launch"))
}

func TestRunTokenZapScript_HookContextDoesNotRunBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.setNESMediaRunning()

	err := runTokenZapScript(env.svc, tokens.Token{
		UID:      "card",
		Text:     "**launch:" + env.gamePath,
		ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 10)}, nil, true)
	require.NoError(t, err)

	assert.Zero(t, env.rec.count("before_exit"),
		"a script already running inside a hook must not re-trigger before_exit")
}

// A launch that is rejected never disturbs the running game, so the exit it
// was going to cause never happens. Firing before_exit anyway runs the user's
// save-and-clean-up script against media that is still playing.
func TestRunTokenZapScript_RejectedLaunchDoesNotRunBeforeExit(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.setNESMediaRunning()

	err := env.run(t, "**launch:"+env.gamePath+"?launcher=NoSuchLauncher")

	require.Error(t, err, "an unknown launcher must fail the launch")
	assert.Zero(t, env.rec.count("launch"), "nothing should have been launched")
	assert.Zero(t, env.rec.count("before_exit"),
		"before_exit must not fire for a launch that was rejected")
}

// A before_exit script may launch media of its own. HandleStop skips its stop
// when that happens; the ZapScript stop commands run through a different path
// and must skip it too, or they kill what the hook just started instead of the
// media the token asked to stop.
func TestRunTokenZapScript_StopSkippedWhenBeforeExitLaunchedMedia(t *testing.T) {
	t.Parallel()

	hookGame := filepath.Join(t.TempDir(), "hook.nes")
	require.NoError(t, os.WriteFile(hookGame, []byte("rom"), 0o600))

	env := setupExitTestEnv(t, "**launch:"+hookGame)
	env.publishActiveMediaOnLaunch()
	env.setNESMediaRunning()

	require.NoError(t, env.run(t, "**stop"))

	assert.Equal(t, 1, env.rec.count("launch"), "the hook's own launch should happen")
	assert.Zero(t, env.rec.count("stop"),
		"the stop must be skipped once before_exit replaced the outgoing media")
}

// The same guard, for the playlist stop command.
func TestRunTokenZapScript_PlaylistStopSkippedWhenBeforeExitLaunchedMedia(t *testing.T) {
	t.Parallel()

	hookGame := filepath.Join(t.TempDir(), "hook.nes")
	require.NoError(t, os.WriteFile(hookGame, []byte("rom"), 0o600))

	env := setupExitTestEnv(t, "**launch:"+hookGame)
	env.publishActiveMediaOnLaunch()
	env.setNESMediaRunning()

	_ = env.run(t, "**playlist.stop")

	assert.Equal(t, 1, env.rec.count("launch"),
		"the hook's own launch should happen, or the stop assertion below proves nothing")
	assert.Zero(t, env.rec.count("stop"),
		"the playlist stop must be skipped once before_exit replaced the outgoing media")
}

// Without a launching hook the stop still happens; the guard must not swallow
// ordinary stops.
func TestRunTokenZapScript_StopStillHappensWithHarmlessHook(t *testing.T) {
	t.Parallel()

	env := setupExitTestEnv(t, keypressHook)
	env.publishActiveMediaOnLaunch()
	env.setNESMediaRunning()

	require.NoError(t, env.run(t, "**stop"))

	assert.Equal(t, []string{"before_exit", "stop"}, env.rec.snapshot(),
		"a hook that launches nothing must not suppress the stop")
}

// stubPlayback is the PlaybackManager the native-audio launcher needs for the
// control tests below: it records calls and never fails.
type stubPlayback struct {
	calls []string
	mu    syncutil.Mutex
}

func (p *stubPlayback) record(call string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
}

func (p *stubPlayback) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func (p *stubPlayback) Play(string, string, audio.PlaybackOptions) error {
	p.record("play")
	return nil
}

func (p *stubPlayback) Stop(string) error {
	p.record("stop")
	return nil
}

func (p *stubPlayback) Pause(string) error {
	p.record("pause")
	return nil
}

func (p *stubPlayback) Resume(string) error {
	p.record("resume")
	return nil
}

func (p *stubPlayback) TogglePause(string) error {
	p.record("toggle_pause")
	return nil
}

func (p *stubPlayback) Seek(string, time.Duration) error {
	p.record("seek")
	return nil
}

func (*stubPlayback) State(string) audio.PlaybackState {
	return audio.PlaybackState{}
}

// TestRunTokenZapScript_BeforeExitControlStopOnNativeAudio drives **control:stop
// from a before_exit script while native audio is the outgoing media. The stop
// control takes the media stop gate exclusively; before_exit runs with no gate
// held, so the token must complete and leave nothing playing.
func TestRunTokenZapScript_BeforeExitControlStopOnNativeAudio(t *testing.T) {
	// Not parallel: cmdControl resolves launchers through the global launcher
	// cache, which this test replaces for its duration.
	env := setupExitTestEnv(t, keypressHook)
	env.svc.Config.SetSystemDefaults([]config.SystemsDefault{
		{System: systemdefs.SystemAudio, BeforeExit: "**control:stop"},
	})
	pm := &stubPlayback{}
	env.svc.PlaybackManager = pm
	previous := helpers.GlobalLauncherCache.GetAllLaunchers()
	helpers.GlobalLauncherCache.InitializeFromSlice([]platforms.Launcher{
		platforms.NativeAudioLauncher(pm, env.svc.State.SetBackgroundMedia,
			func(ctx context.Context, stop func() error) error {
				return stopNativeAudioPrimaryMedia(ctx, env.svc, stop)
			}),
	})
	t.Cleanup(func() { helpers.GlobalLauncherCache.InitializeFromSlice(previous) })

	env.svc.State.SetActiveMedia(models.NewActiveMedia(
		systemdefs.SystemAudio, "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID))
	gen, active := env.svc.State.ActiveMediaReadyGeneration()
	require.True(t, active)
	env.svc.State.MarkActiveMediaReady(gen)

	done := make(chan error, 1)
	go func() { done <- env.run(t, "**stop") }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("gated **control:stop from before_exit hung the stop")
	}

	assert.Equal(t, []string{"stop"}, pm.snapshot(), "before_exit must stop native audio exactly once")
	assert.Nil(t, env.svc.State.ActiveMedia(), "the stop control clears the media it stopped")
	assert.Zero(t, env.rec.count("stop"), "nothing is left for the outer stop to exit")
}
