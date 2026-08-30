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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
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
	mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Run(func(_ mock.Arguments) {
		rec.record("launch")
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

	return &exitTestEnv{svc: svc, platform: mockPlatform, rec: rec, gamePath: gamePath}
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
