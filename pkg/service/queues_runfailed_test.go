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
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const runFailedTimeout = 5 * time.Second

// runFailedEnv drives handleQueuedToken and launchPlaylistMedia directly with
// the notification channel in hand, so a test can read what clients are told.
type runFailedEnv struct {
	svc    *ServiceContext
	userDB *helpers.MockUserDBI
	player *mocks.MockPlayer
	ns     <-chan models.Notification
}

func setupRunFailedEnv(t *testing.T) *runFailedEnv {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("ScanHook", mock.Anything).Return(nil).Maybe()
	mockPlatform.On("ReturnToMenu").Return(nil).Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	cfg := &config.Instance{}
	cfg.SetAudioFeedback(true)

	st, ns := state.NewState(mockPlatform, "test-boot-uuid")
	st.SetRunZapScript(true)
	t.Cleanup(st.StopService)

	userDB := helpers.NewMockUserDBI()
	userDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	userDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()
	userDB.On("AddHistory", mock.Anything).Return(nil).Maybe()

	player := mocks.NewMockPlayer()

	return &runFailedEnv{
		svc: &ServiceContext{
			Platform:            mockPlatform,
			Config:              cfg,
			State:               st,
			DB:                  &database.Database{UserDB: userDB},
			LaunchSoftwareQueue: make(chan softwareTokenUpdate, 10),
			PlaylistQueue:       make(chan *playlists.Playlist, 10),
			BackgroundWG:        &sync.WaitGroup{},
		},
		userDB: userDB,
		player: player,
		ns:     ns,
	}
}

// runFailed waits for the next run.failed notification and decodes it.
func (env *runFailedEnv) runFailed(t *testing.T) models.RunFailedParams {
	t.Helper()
	deadline := time.After(runFailedTimeout)
	for {
		select {
		case n := <-env.ns:
			if n.Method != models.NotificationRunFailed {
				continue
			}
			var params models.RunFailedParams
			require.NoError(t, json.Unmarshal(n.Params, &params))
			return params
		case <-deadline:
			t.Fatal("no run.failed notification")
			return models.RunFailedParams{}
		}
	}
}

func (env *runFailedEnv) expectNoRunFailed(t *testing.T) {
	t.Helper()
	env.svc.BackgroundWG.Wait()
	for {
		select {
		case n := <-env.ns:
			assert.NotEqual(t, models.NotificationRunFailed, n.Method, "run.failed must not be sent")
		default:
			return
		}
	}
}

func (env *runFailedEnv) queue(t *testing.T, source, text string) *tokens.Completion {
	t.Helper()
	c := tokens.NewCompletion()
	handleQueuedToken(env.svc, tokens.Token{
		Text:       text,
		ScanTime:   time.Now(),
		Source:     source,
		ReaderID:   "reader-1",
		Completion: c,
	}, env.player)
	return c
}

func waitRunCompletion(t *testing.T, c *tokens.Completion) error {
	t.Helper()
	select {
	case err := <-c.Done():
		return err
	case <-time.After(runFailedTimeout):
		t.Fatal("token was never completed")
		return nil
	}
}

func TestHandleQueuedToken_ReaderTokenPlaysSuccessBeforeExecution(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", assets.SuccessSound).Return(nil).Once()

	c := env.queue(t, tokens.SourceReader, "**echo:hi")

	// The worker has returned but the launch goroutine may not have run yet:
	// a physical scan is acknowledged before the script executes.
	env.player.AssertCalled(t, "PlayBytes", assets.SuccessSound)
	require.NoError(t, waitRunCompletion(t, c))
	env.svc.BackgroundWG.Wait()
	env.player.AssertNumberOfCalls(t, "PlayBytes", 1)
}

func TestHandleQueuedToken_APITokenPlaysSuccessOnlyAfterCompletion(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	c := tokens.NewCompletion()
	completedAtPlay := false
	env.player.On("PlayBytes", assets.SuccessSound).Run(func(mock.Arguments) {
		// Complete has already been delivered once when the sound plays, so
		// this probe must lose; if it won, the caller would read "probe".
		completedAtPlay = !c.Complete(errors.New("probe"))
	}).Return(nil).Once()

	handleQueuedToken(env.svc, tokens.Token{
		Text: "**echo:hi", ScanTime: time.Now(), Source: tokens.SourceAPI, Completion: c,
	}, env.player)

	require.NoError(t, waitRunCompletion(t, c))
	env.svc.BackgroundWG.Wait()
	env.player.AssertNumberOfCalls(t, "PlayBytes", 1)
	assert.True(t, completedAtPlay, "an API token's success sound follows its completion")
	env.expectNoRunFailed(t)
}

func TestHandleQueuedToken_APITokenFailurePlaysOnlyFailSoundAndNotifies(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", assets.FailSound).Return(nil).Once()

	c := env.queue(t, tokens.SourceAPI, "**nonexistent.cmd")

	require.Error(t, waitRunCompletion(t, c))
	env.svc.BackgroundWG.Wait()
	env.player.AssertNumberOfCalls(t, "PlayBytes", 1)
	env.player.AssertNotCalled(t, "PlayBytes", assets.SuccessSound)

	failed := env.runFailed(t)
	assert.Equal(t, tokens.SourceAPI, failed.Source)
	assert.Equal(t, "reader-1", failed.ReaderID)
	assert.Equal(t, "**nonexistent.cmd", failed.Script)
	assert.Equal(t, "nonexistent.cmd", failed.Command)
	assert.Equal(t, models.ErrorCategoryInvalidScript, failed.Category)
	assert.Equal(t, "ZapScript is invalid", failed.Message)
	assert.Empty(t, failed.PlaylistID)
	assert.Nil(t, failed.PlaylistIndex)
}

func TestHandleQueuedToken_ReaderTokenFailureNotifies(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	c := env.queue(t, tokens.SourceReader, "**nonexistent.cmd")

	require.Error(t, waitRunCompletion(t, c))
	failed := env.runFailed(t)
	assert.Equal(t, tokens.SourceReader, failed.Source)
	assert.Equal(t, models.ErrorCategoryInvalidScript, failed.Category)
}

func TestHandleQueuedToken_DisabledRunDoesNotNotify(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.svc.State.SetRunZapScript(false)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	c := env.queue(t, tokens.SourceAPI, "**echo:hi")

	require.ErrorIs(t, waitRunCompletion(t, c), state.ErrRunZapScriptDisabled)
	env.expectNoRunFailed(t)
	env.player.AssertNotCalled(t, "PlayBytes", assets.SuccessSound)
}

func TestHandleQueuedToken_RedactsScriptInNotification(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	// The profile switch ID is a credential; the run fails on the first
	// command, but the script text still carries the ID and history stores
	// it redacted, so the notification must too.
	script := "**nonexistent.cmd||**profile:sw-7f3a9c21"
	c := env.queue(t, tokens.SourceAPI, script)

	require.Error(t, waitRunCompletion(t, c))
	failed := env.runFailed(t)
	assert.NotContains(t, failed.Script, "sw-7f3a9c21")
	assert.NotContains(t, failed.Message, "sw-7f3a9c21")
}

func failingPlaylist(id string, index int) *playlists.Playlist {
	pls := playlists.NewPlaylist(id, id, []playlists.PlaylistItem{
		{Name: "A", ZapScript: "**nonexistent.a"},
		{Name: "B", ZapScript: "**nonexistent.b"},
		{Name: "C", ZapScript: "**nonexistent.c"},
	})
	pls.Index = index
	pls.Playing = true
	return pls
}

func TestLaunchPlaylistMedia_FailedLaunchPausesPlaylist(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", assets.FailSound).Return(nil).Once()
	var recorded *database.HistoryEntry
	env.userDB.ExpectedCalls = nil
	env.userDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	env.userDB.On("AddHistory", mock.Anything).Run(func(args mock.Arguments) {
		he, ok := args.Get(0).(*database.HistoryEntry)
		require.True(t, ok)
		recorded = he
	}).Return(nil).Once()

	pls := failingPlaylist("deck://abc", 1)
	env.svc.State.SetActivePlaylist(pls)

	launchPlaylistMedia(env.svc, pls, env.player)

	after := env.svc.State.GetActivePlaylist()
	require.NotNil(t, after)
	assert.False(t, after.Playing, "a playlist whose item did not launch is not playing")
	assert.Equal(t, 1, after.Index, "the failed item stays current so play retries it")
	assert.Equal(t, "deck://abc", after.ID)
	require.NotNil(t, recorded)
	assert.False(t, recorded.Success)
	env.player.AssertExpectations(t)

	failed := env.runFailed(t)
	assert.Equal(t, tokens.SourcePlaylist, failed.Source)
	assert.Equal(t, "deck://abc", failed.PlaylistID)
	require.NotNil(t, failed.PlaylistIndex)
	assert.Equal(t, 1, *failed.PlaylistIndex)
	assert.Equal(t, "**nonexistent.b", failed.Script)
	assert.Equal(t, "nonexistent.b", failed.Command)
	assert.Equal(t, models.ErrorCategoryInvalidScript, failed.Category)
}

func TestLaunchPlaylistMedia_FailedLaunchLeavesReplacedPlaylistAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stored *playlists.Playlist
		name   string
	}{
		{name: "another playlist took the slot", stored: failingPlaylist("deck://other", 1)},
		{name: "the playlist moved on", stored: failingPlaylist("deck://abc", 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupRunFailedEnv(t)
			env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()
			env.svc.State.SetActivePlaylist(tt.stored)

			launchPlaylistMedia(env.svc, failingPlaylist("deck://abc", 1), env.player)

			after := env.svc.State.GetActivePlaylist()
			assert.Same(t, tt.stored, after)
			assert.True(t, after.Playing, "a late failure must not pause what replaced it")
		})
	}
}

// TestLaunchPlaylistMedia_BusyRefusalDoesNotPause pins that a launch refused
// because another launch is in flight leaves the playlist playing. Pausing and
// playing again while an item loads starts a second launch of that item; the
// guard refuses it while the first one succeeds, and pausing then would leave
// the playlist paused over the game it launched.
func TestLaunchPlaylistMedia_BusyRefusalDoesNotPause(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()
	require.NoError(t, env.svc.State.LauncherManager().TryStartLaunch(), "the in-flight launch holds the guard")
	t.Cleanup(env.svc.State.LauncherManager().EndLaunch)

	pls := playlists.NewPlaylist("deck://abc", "abc", []playlists.PlaylistItem{{ZapScript: "**launch.system:nes"}})
	pls.Playing = true
	env.svc.State.SetActivePlaylist(pls)

	launchPlaylistMedia(env.svc, pls, env.player)

	failed := env.runFailed(t)
	require.Equal(t, models.ErrorCategoryBusy, failed.Category, "the item must have been refused as busy")
	after := env.svc.State.GetActivePlaylist()
	assert.Same(t, pls, after)
	assert.True(t, after.Playing, "a busy refusal must not pause the playlist")
}

func TestLaunchPlaylistMedia_BackgroundFailurePausesBackgroundOnly(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()
	primary := failingPlaylist("deck://primary", 0)
	background := failingPlaylist("deck://bg", 0)
	background.Slot = mediaslot.Background
	env.svc.State.SetActivePlaylist(primary)
	env.svc.State.SetBackgroundPlaylist(background)

	launchPlaylistMedia(env.svc, background, env.player)

	assert.False(t, env.svc.State.GetBackgroundPlaylist().Playing)
	assert.True(t, env.svc.State.GetActivePlaylist().Playing)
	failed := env.runFailed(t)
	assert.Equal(t, "deck://bg", failed.PlaylistID)
}

func TestHandlePlaylist_PlayAfterFailedLaunchRelaunches(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	// The state a failed item leaves behind: same playlist, same index, paused.
	stored := playlists.Pause(*failingPlaylist("deck://abc", 1))
	env.svc.State.SetActivePlaylist(stored)

	// What the picker's play, or the app's play button, sends next.
	handlePlaylist(env.svc, playlists.Play(*stored), env.player)
	env.svc.BackgroundWG.Wait()

	env.userDB.AssertCalled(t, "AddHistory", mock.Anything)
	failed := env.runFailed(t)
	require.NotNil(t, failed.PlaylistIndex)
	assert.Equal(t, 1, *failed.PlaylistIndex, "play retries the item that failed")
	assert.False(t, env.svc.State.GetActivePlaylist().Playing, "and pauses again when it fails again")
}

// TestPlaylistNext_AfterFailedLaunchLaunchesNextItem pins that next, after an
// item failed, launches the item it moves to. A playlist someone paused only
// moves its cursor, so a failure must not leave the playlist looking paused
// that way.
func TestPlaylistNext_AfterFailedLaunchLaunchesNextItem(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	pls := failingPlaylist("deck://abc", 0)
	env.svc.State.SetActivePlaylist(pls)
	launchPlaylistMedia(env.svc, pls, env.player)
	require.True(t, env.svc.State.GetActivePlaylist().PausedByFailure)
	first := env.runFailed(t)
	require.NotNil(t, first.PlaylistIndex)
	require.Equal(t, 0, *first.PlaylistIndex)

	handlePlaylist(env.svc, playlists.Next(*env.svc.State.GetActivePlaylist()), env.player)
	env.svc.BackgroundWG.Wait()

	second := env.runFailed(t)
	require.NotNil(t, second.PlaylistIndex)
	assert.Equal(t, 1, *second.PlaylistIndex, "next launched the item it moved to")
}

// TestPlaylistNext_WhenPausedOnPurposeOnlyMovesCursor pins the behaviour a
// failure must not change: next on a playlist someone paused launches nothing.
func TestPlaylistNext_WhenPausedOnPurposeOnlyMovesCursor(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	paused := playlists.Pause(*failingPlaylist("deck://abc", 0))
	env.svc.State.SetActivePlaylist(paused)

	handlePlaylist(env.svc, playlists.Next(*paused), env.player)
	env.svc.BackgroundWG.Wait()

	after := env.svc.State.GetActivePlaylist()
	assert.Equal(t, 1, after.Index)
	assert.False(t, after.Playing)
	env.expectNoRunFailed(t)
	env.userDB.AssertNotCalled(t, "AddHistory", mock.Anything)
}

// An API token that arms a next action finishes in the preflight and never
// reaches the launch, where a run request's success sound is played. It
// succeeded, so it must still get that sound.
func TestHandleQueuedToken_APITokenArmingNextActionPlaysSuccess(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", assets.SuccessSound).Return(nil).Once()

	c := env.queue(t, tokens.SourceAPI, "**write:payload")

	require.NoError(t, waitRunCompletion(t, c))
	require.NotNil(t, env.svc.State.GetPendingWrite(), "the write must have been armed")
	env.svc.BackgroundWG.Wait()
	env.player.AssertNumberOfCalls(t, "PlayBytes", 1)
	env.expectNoRunFailed(t)
}

// Runs refused before they start are runs that failed: each preflight
// rejection must be reported.
func TestHandleQueuedToken_PreflightRejectionsNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup    func(env *runFailedEnv)
		name     string
		text     string
		category string
		script   string
	}{
		{
			name: "invalid next action", text: "**write:", script: "**write:",
			category: models.ErrorCategoryInvalidScript,
		},
		{
			name: "blocked next action", text: "**write:payload", script: "**write:payload",
			category: models.ErrorCategoryBlocked,
			setup: func(env *runFailedEnv) {
				require.NoError(t, env.svc.Config.LoadTOML("[zapscript]\nblock_commands = [\"write\"]\n"))
			},
		},
		{
			name: "profile required", text: "**launch.system:nes", script: "**launch.system:nes",
			category: models.ErrorCategoryBlocked,
			setup:    func(env *runFailedEnv) { env.svc.Config.SetProfilesRequireForLaunch(true) },
		},
		{
			// The script is left out: it was never run, and redacting it
			// would mean parsing it.
			name: "oversized", text: "**echo:" + strings.Repeat("a", zapscript.MaxScriptLength),
			category: models.ErrorCategoryInvalidScript,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupRunFailedEnv(t)
			env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()
			if tt.setup != nil {
				tt.setup(env)
			}

			c := env.queue(t, tokens.SourceAPI, tt.text)

			require.Error(t, waitRunCompletion(t, c))
			failed := env.runFailed(t)
			assert.Equal(t, tt.category, failed.Category)
			assert.Equal(t, tt.script, failed.Script)
			assert.Equal(t, tokens.SourceAPI, failed.Source)
		})
	}
}

// A run stopped by shutdown failed only because the service is going away.
func TestReportRunFailure_SilentDuringShutdown(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.svc.State.StopService()
	for {
		select {
		case <-env.ns:
			continue
		default:
		}
		break
	}

	reportRunFailure(env.svc, &tokens.Token{Text: "**x", Source: tokens.SourceAPI},
		errors.New("context canceled by shutdown"), nil)

	env.expectNoRunFailed(t)
}

// A hook is a run of its own: its failure is reported with the hook source.
// One its caller cancelled, like an on_remove abandoned when the tag returns,
// did not fail.
func TestRunHookWithContext_ReportsFailureButNotCancellation(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)

	err := runHookWithContext(context.Background(), env.svc, "on_scan", "**nonexistent.hook", nil, nil)
	require.Error(t, err)
	failed := env.runFailed(t)
	assert.Equal(t, tokens.SourceHook, failed.Source)
	assert.Equal(t, "nonexistent.hook", failed.Command)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, runHookWithContext(ctx, env.svc, "on_remove", "**nonexistent.hook", nil, nil))
	env.expectNoRunFailed(t)
}

// A script run inside another run, such as before_media_start, is not a run
// of its own: its failure fails the run around it, which is reported once.
func TestRunTokenZapScript_NestedHookContextIsNotReported(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)

	err := runTokenZapScript(env.svc, tokens.Token{
		Text: "**nonexistent.nested", ScanTime: time.Now(),
		Source: tokens.SourceHook,
	}, playlists.PlaylistController{Queue: env.svc.PlaylistQueue}, nil, true)

	require.Error(t, err)
	env.expectNoRunFailed(t)
}

// The pause decision: whether the failed item left nothing playing.
func TestPausesAfterFailedItem(t *testing.T) {
	t.Parallel()

	primary := playlists.NewPlaylist("p", "p", []playlists.PlaylistItem{{ZapScript: "**a"}})
	background := playlists.NewPlaylist("b", "b", []playlists.PlaylistItem{{ZapScript: "**a"}})
	background.Slot = mediaslot.Background
	game := func(name string) *models.ActiveMedia {
		return models.NewActiveMedia("NES", "NES", "/games/"+name+".nes", name, "nes")
	}
	notFound := zapscript.ErrFileNotFound

	tests := []struct {
		before func(*state.State)
		during func(*state.State)
		pls    *playlists.Playlist
		err    error
		name   string
		want   bool
	}{
		{name: "nothing ran before or after", pls: primary, err: notFound, want: true},
		{
			name: "earlier game still running", pls: primary, err: notFound, want: true,
			before: func(s *state.State) { s.SetActiveMedia(game("old")) },
		},
		{
			name: "item started its game, a later command failed", pls: primary, err: notFound, want: false,
			during: func(s *state.State) { s.SetActiveMedia(game("new")) },
		},
		{
			name: "item replaced the running game, a later command failed", pls: primary, err: notFound,
			want:   false,
			before: func(s *state.State) { s.SetActiveMedia(game("old")) },
			during: func(s *state.State) { s.SetActiveMedia(game("new")) },
		},
		{
			name: "item stopped the running game, then failed", pls: primary, err: notFound, want: true,
			before: func(s *state.State) { s.SetActiveMedia(game("old")) },
			during: func(s *state.State) { s.SetActiveMedia(nil) },
		},
		{name: "busy refusal", pls: primary, err: state.ErrLaunchInProgress, want: false},
		{name: "background item", pls: background, err: notFound, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupRunFailedEnv(t)
			if tt.before != nil {
				tt.before(env.svc.State)
			}
			gen, had := env.svc.State.ActiveMediaReadyGeneration()
			if tt.during != nil {
				tt.during(env.svc.State)
			}
			assert.Equal(t, tt.want, pausesAfterFailedItem(env.svc, tt.pls, tt.err, gen, had))
		})
	}
}

// Goto on a playlist paused by a failure plays the item it moves to.
func TestHandlePlaylist_GotoResumesPlaylistPausedByFailure(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	stored := failingPlaylist("deck://abc", 0)
	env.svc.State.SetActivePlaylist(stored)
	require.True(t, env.svc.State.PausePlaylistIfCurrent(mediaslot.Primary, stored))

	handlePlaylist(env.svc, playlists.Goto(*env.svc.State.GetActivePlaylist(), 2), env.player)
	env.svc.BackgroundWG.Wait()

	failed := env.runFailed(t)
	require.NotNil(t, failed.PlaylistIndex)
	assert.Equal(t, 2, *failed.PlaylistIndex, "goto alone launched the item it moved to")
}

// The picker sends goto then play in one script. On a playlist paused by a
// failure goto already plays the item, so the play that follows it must not
// launch it a second time.
func TestHandlePlaylist_PickerGotoThenPlayLaunchesOnce(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	stored := failingPlaylist("deck://abc", 0)
	env.svc.State.SetActivePlaylist(stored)
	require.True(t, env.svc.State.PausePlaylistIfCurrent(mediaslot.Primary, stored))

	moved := playlists.Goto(*env.svc.State.GetActivePlaylist(), 2)
	handlePlaylist(env.svc, moved, env.player)
	handlePlaylist(env.svc, playlists.Play(*moved), env.player)
	env.svc.BackgroundWG.Wait()

	failed := env.runFailed(t)
	require.NotNil(t, failed.PlaylistIndex)
	assert.Equal(t, 2, *failed.PlaylistIndex)
	env.expectNoRunFailed(t)
}

// A playlist paused by a failure never loaded its current item, so playing it
// again retries that item rather than resuming whatever earlier track the
// playback manager still holds.
func TestHandlePlaylist_PlayAfterFailureRetriesInsteadOfResumingOldTrack(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()
	recorder := &servicePlaybackRecorder{states: map[string]audio.PlaybackState{
		mediaslot.Primary: {Path: "earlier-track.mp3"},
	}}
	env.svc.PlaybackManager = recorder

	stored := failingPlaylist("deck://abc", 1)
	env.svc.State.SetActivePlaylist(stored)
	require.True(t, env.svc.State.PausePlaylistIfCurrent(mediaslot.Primary, stored))

	handlePlaylist(env.svc, playlists.Play(*env.svc.State.GetActivePlaylist()), env.player)
	env.svc.BackgroundWG.Wait()

	assert.Empty(t, recorder.resumed, "the earlier track must not be resumed")
	failed := env.runFailed(t)
	require.NotNil(t, failed.PlaylistIndex)
	assert.Equal(t, 1, *failed.PlaylistIndex, "the failed item was retried")
}
