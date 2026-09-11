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
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func (env *scanBehaviorEnv) scanWithCompletion(t *testing.T, uid, text string) *tokens.Completion {
	t.Helper()
	completion := tokens.NewCompletion()
	select {
	case env.scanQueue <- readers.Scan{
		ReaderID: testReaderID,
		Token: &tokens.Token{
			UID: uid, Text: text, Source: tokens.SourceReader, ReaderID: testReaderID,
			ScanTime: time.Now(), Completion: completion,
		},
	}:
	case <-time.After(behaviorTimeout):
		t.Fatal("reader manager did not accept scan")
	}
	return completion
}

func TestTapRelaunch_SameTargetPreservesStateAndChainedCommands(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	path := env.gamePath("game.rom")
	first := env.scanWithCompletion(t, "first", path)
	require.NoError(t, waitCompletion(t, first))
	require.Equal(t, path, env.waitForLaunch(t))
	env.waitForSoftwareToken(t)
	active := env.st.ActiveMedia()
	owner := env.st.GetSoftwareToken()
	playlist := &playlists.Playlist{ID: "keep-this-playlist"}
	env.st.SetActivePlaylist(playlist)

	var exits atomic.Int32
	env.st.SetBeforeExitHook(func() { exits.Add(1) })
	require.NoError(t, env.cfg.LoadTOML(`[launchers]
before_media_start = "**input.keyboard:before"`))

	// Different UID and script, identical resolved file. Only the launch is ignored.
	second := env.scanWithCompletion(t, "second", "**launch:"+path+"||**input.keyboard:x")
	require.NoError(t, assertCompletedOnce(t, second))
	assert.Empty(t, env.launchCh)
	assert.Empty(t, env.stopCh)
	assert.Equal(t, "x", env.waitForKeyboard(t))
	assert.Empty(t, env.keyboardCh, "before_media_start must not run for a no-op")
	assert.Zero(t, exits.Load())
	assert.Same(t, active, env.st.ActiveMedia())
	assert.Equal(t, owner, env.st.GetSoftwareToken())
	assert.Same(t, playlist, env.st.GetActivePlaylist())
}

func TestTapRelaunch_MappingAndCurrentState(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	path := env.gamePath("game.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	env.addConfigMapping(t, "mapped-game", path)

	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "mapped", "mapped-game")))
	assert.Empty(t, env.launchCh, "mapped script and direct path resolve to the same game")
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "coin", "**input.keyboard:coin")))
	env.waitForKeyboard(t)
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "mapped", "mapped-game")))
	assert.Empty(t, env.launchCh, "a utility card must not reset suppression")

	env.sendRemoval()
	env.simulateManualExit()
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "mapped", "mapped-game")))
	assert.Equal(t, path, env.waitForLaunch(t), "same card launches after the game exits")

	otherPath := env.gamePath("other.rom")
	env.addConfigMapping(t, "mapped-game", otherPath)
	env.sendRemoval()
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "mapped", "mapped-game")))
	assert.Equal(t, otherPath, env.waitForLaunch(t), "same text must resolve its current mapping")
}

func TestTapRelaunch_Scope(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, source, mode, trait, config string
		wantLaunch                        bool
	}{
		{name: "tap reader", source: tokens.SourceReader, mode: "tap"},
		{
			name: "legacy opt out", source: tokens.SourceReader, mode: "tap",
			config: "[readers.scan]\nallow_relaunch = true", wantLaunch: true,
		},
		{name: "hold reader", source: tokens.SourceReader, mode: "hold", wantLaunch: true},
		{name: "tap trait", source: tokens.SourceReader, mode: "hold", trait: "#tap||"},
		{name: "hold trait", source: tokens.SourceReader, mode: "tap", trait: "#hold||", wantLaunch: true},
		{
			name: "reader override", source: tokens.SourceReader, mode: "hold",
			config: "[readers.drivers.mock-reader]\nscan_mode = 'tap'",
		},
		{name: "API", source: tokens.SourceAPI, mode: "tap", wantLaunch: true},
		{name: "playlist", source: tokens.SourcePlaylist, mode: "tap", wantLaunch: true},
		{name: "hook", source: tokens.SourceHook, mode: "tap", wantLaunch: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupScanBehavior(t, tt.mode, 0)
			path := env.gamePath("game.rom")
			require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
			env.waitForLaunch(t)
			if tt.config != "" {
				require.NoError(t, env.cfg.LoadTOML(tt.config))
			}
			completion := tokens.NewCompletion()
			env.sendAPITokenWith(t, tokens.Token{
				Source: tt.source, Text: tt.trait + path, ReaderID: testReaderID,
				ScanTime: time.Now(), Completion: completion,
			})
			require.NoError(t, waitCompletion(t, completion))
			if tt.wantLaunch {
				assert.Equal(t, path, env.waitForLaunch(t))
			} else {
				assert.Empty(t, env.launchCh)
			}
		})
	}
}

func TestTapRelaunch_ResolvedSearchAndRandom(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	path := env.gamePath("game.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	mediaDB := env.mediaDB
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{{Path: path, SystemID: "mock"}}, nil).Once()
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "search", "**launch.search:game")))
	assert.Empty(t, env.launchCh)

	mediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{Path: path, SystemID: "mock"}, nil).Once()
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "random", "**launch.random:all")))
	assert.Empty(t, env.launchCh, "random selection of the current game is a no-op, not a retry")
	env.sendRemoval()
	otherPath := env.gamePath("other.rom")
	mediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{Path: otherPath, SystemID: "mock"}, nil).Once()
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "random", "**launch.random:all")))
	assert.Equal(t, otherPath, env.waitForLaunch(t), "same random script must be resolved again")
	mediaDB.AssertExpectations(t)
}

func (env *scanBehaviorEnv) waitForGuard(t *testing.T) models.UIEvent {
	t.Helper()
	for {
		select {
		case update := <-env.uiCh:
			if len(update.Events) > 0 {
				return update.Events[0]
			}
		case <-time.After(behaviorTimeout):
			t.Fatal("launch guard did not open")
			return models.UIEvent{}
		}
	}
}

func TestTapRelaunch_GuardResumesResolvedTarget(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	path := env.gamePath("game.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)

	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "same", path)))
	assert.Empty(t, env.launchCh)
	assert.Empty(t, env.svc.UI.State().Events, "same game must not prompt")

	otherPath := env.gamePath("other.rom")
	mediaDB := env.mediaDB
	mediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{Path: otherPath, SystemID: "mock"}, nil).Once()
	completion := env.scanWithCompletion(t, "random", "**launch.random:all")
	env.waitForGuard(t)
	assert.Empty(t, env.launchCh)
	// Re-tap confirms the prepared target, without selecting another random game.
	env.sendCommandScan("random", "**launch.random:all")
	require.NoError(t, waitCompletion(t, completion))
	assert.Equal(t, otherPath, env.waitForLaunch(t))
	mediaDB.AssertExpectations(t)
}

func TestTapRelaunch_GuardCancellation(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	path := env.gamePath("game.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)
	completion := env.scanWithCompletion(t, "other", env.gamePath("other.rom"))
	event := env.waitForGuard(t)
	require.NoError(t, env.svc.UI.Respond(event.ID, models.UIResponseActionDismiss, ""))
	require.NoError(t, waitCompletion(t, completion))
	assert.Empty(t, env.launchCh)
	assert.Equal(t, path, env.st.ActiveMedia().Path)
}

func TestTapRelaunch_GuardRechecksCurrentMedia(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, env.gamePath("first.rom"))))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)
	path := env.gamePath("second.rom")
	completion := env.scanWithCompletion(t, "second", path)
	event := env.waitForGuard(t)

	// Another explicit action starts the target while confirmation is open.
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	active := env.st.ActiveMedia()
	require.NoError(t, env.cfg.LoadTOML(`[launchers]
before_media_start = "**input.keyboard:x"`))
	require.NoError(t, env.svc.UI.Respond(event.ID, models.UIResponseActionConfirm, ""))
	require.NoError(t, waitCompletion(t, completion))
	assert.Empty(t, env.launchCh)
	assert.Empty(t, env.keyboardCh, "no hook after target became active during confirmation")
	assert.Same(t, active, env.st.ActiveMedia())
}

func TestTapRelaunch_GuardRejectsLateResolution(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, env.gamePath("first.rom"))))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)

	resolving := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	env.mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{{Path: env.gamePath("slow.rom"), SystemID: "mock"}}, nil).
		Run(func(_ mock.Arguments) {
			close(resolving)
			<-release
		}).Once()
	slow := env.scanWithCompletion(t, "slow", "**launch.search:slow")
	select {
	case <-resolving:
	case <-time.After(behaviorTimeout):
		t.Fatal("search did not start")
	}
	path := env.gamePath("newer.rom")
	newer := env.scanWithCompletion(t, "newer", path)
	event := env.waitForGuard(t)
	// The old resolution must not replace the newer card's prompt.
	release <- struct{}{}
	require.NoError(t, waitCompletion(t, slow))
	require.Len(t, env.svc.UI.State().Events, 1)
	assert.Equal(t, event.ID, env.svc.UI.State().Events[0].ID)
	require.NoError(t, env.svc.UI.Respond(event.ID, models.UIResponseActionConfirm, ""))
	require.NoError(t, waitCompletion(t, newer))
	assert.Equal(t, path, env.waitForLaunch(t))
	assert.Empty(t, env.launchCh)
}

func TestTapRelaunch_GuardStopDuringResolutionCancelsLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, env.gamePath("first.rom"))))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)
	resolving := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	env.mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{{Path: env.gamePath("slow.rom"), SystemID: "mock"}}, nil).
		Run(func(_ mock.Arguments) {
			close(resolving)
			<-release
		}).Once()
	completion := env.scanWithCompletion(t, "slow", "**launch.search:slow")
	select {
	case <-resolving:
	case <-time.After(behaviorTimeout):
		t.Fatal("search did not start")
	}
	env.simulateManualExit()
	release <- struct{}{}
	require.NoError(t, waitCompletion(t, completion))
	assert.Empty(t, env.launchCh)
	assert.Empty(t, env.svc.UI.State().Events)
	assert.Nil(t, env.st.ActiveMedia())
}

func TestTapRelaunch_GuardRechecksAdmission(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		want   error
		change func(*testing.T, *scanBehaviorEnv)
		name   string
	}{
		{
			name: "execution disabled", want: state.ErrRunZapScriptDisabled,
			change: func(_ *testing.T, env *scanBehaviorEnv) { env.st.SetRunZapScript(false) },
		},
		{
			name: "profile required", want: state.ErrLaunchRequiresProfile,
			change: func(_ *testing.T, env *scanBehaviorEnv) { env.cfg.SetProfilesRequireForLaunch(true) },
		},
		{
			name: "command blocked", want: zapscript.ErrCommandBlocked,
			change: func(t *testing.T, env *scanBehaviorEnv) {
				t.Helper()
				require.NoError(t, env.cfg.LoadTOML("[zapscript]\nblock_commands = ['launch']"))
			},
		},
		{
			name: "playtime exhausted", want: playtime.ErrLimitReached,
			change: func(t *testing.T, env *scanBehaviorEnv) {
				t.Helper()
				env.cfg.SetPlaytimeLimitsEnabled(true)
				require.NoError(t, env.cfg.SetDailyLimit("1h"))
				env.userDB.On("SumMediaPlayTimeForDay", mock.Anything).Return(int64(7200), nil)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupScanBehavior(t, "tap", 0)
			require.NoError(t, waitCompletion(t, env.sendAPIToken(t, env.gamePath("first.rom"))))
			env.waitForLaunch(t)
			env.cfg.SetLaunchGuard(true)
			completion := env.scanWithCompletion(t, "second", env.gamePath("second.rom"))
			event := env.waitForGuard(t)
			tt.change(t, env)
			require.NoError(t, env.svc.UI.Respond(event.ID, models.UIResponseActionConfirm, ""))
			require.ErrorIs(t, waitCompletion(t, completion), tt.want)
			assert.Empty(t, env.launchCh)
		})
	}
}

func TestTapRelaunch_GuardShutdownReleasesWorker(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, env.gamePath("first.rom"))))
	env.waitForLaunch(t)
	env.cfg.SetLaunchGuard(true)
	completion := env.scanWithCompletion(t, "second", env.gamePath("second.rom"))
	env.waitForGuard(t)
	env.st.StopService()
	// Cancellation can win in either the manager or worker; both must finish.
	_ = waitCompletion(t, completion)
	assert.Empty(t, env.launchCh)
}

func TestTapRelaunch_ExplicitStopThenLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	path := env.gamePath("game.rom")
	require.NoError(t, waitCompletion(t, env.sendAPIToken(t, path)))
	env.waitForLaunch(t)
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "restart", "**stop||"+path)))
	assert.Equal(t, path, env.waitForLaunch(t))
	env.waitForStop(t)
}

func TestTapRelaunch_PreLaunchHookCanLaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, "tap", 0)
	path := env.gamePath("game.rom")
	hookPath := env.gamePath("hook.rom")
	require.NoError(t, env.cfg.LoadTOML("[launchers]\nbefore_media_start = "+strconv.Quote(hookPath)))
	require.NoError(t, waitCompletion(t, env.scanWithCompletion(t, "game", path)))
	assert.Equal(t, hookPath, env.waitForLaunch(t))
	assert.Equal(t, path, env.waitForLaunch(t))
}

func TestLaunchMatchesActive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "game.rom")
	active := models.NewActiveMedia("NES", "NES", path, "Same Name", "nes")
	for _, tt := range []struct {
		name   string
		target platforms.ResolvedLaunch
		want   bool
	}{
		{name: "path", target: platforms.ResolvedLaunch{Path: path}, want: true},
		{name: "system", target: platforms.ResolvedLaunch{Path: path, SystemID: "NES"}, want: true},
		{name: "other file", target: platforms.ResolvedLaunch{Path: filepath.Join(t.TempDir(), "game.rom")}},
		{name: "other system", target: platforms.ResolvedLaunch{Path: path, SystemID: "SNES"}},
		{
			name:   "other launcher",
			target: platforms.ResolvedLaunch{Path: path, Launcher: &platforms.Launcher{ID: "other", SystemID: "NES"}},
		},
		{
			name:   "details",
			target: platforms.ResolvedLaunch{Path: path, Options: &platforms.LaunchOptions{Action: "details"}},
		},
		{
			name:   "background",
			target: platforms.ResolvedLaunch{Path: path, Options: &platforms.LaunchOptions{Slot: "background"}},
		},
		{
			name:   "core override",
			target: platforms.ResolvedLaunch{Path: path, Options: &platforms.LaunchOptions{SetName: "other"}},
		},
		{name: "empty", target: platforms.ResolvedLaunch{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, launchMatchesActive(active, tt.target))
		})
	}
	assert.False(t, launchMatchesActive(nil, platforms.ResolvedLaunch{Path: path}))
	assert.False(t, launchMatchesActive(
		models.NewActiveMedia("Audio", "Audio", path, "Track", "audio"),
		platforms.ResolvedLaunch{Path: path},
	), "audio repeat workflows are unaffected")
}
