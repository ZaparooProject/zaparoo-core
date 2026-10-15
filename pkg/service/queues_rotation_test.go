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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeIdlePlaylist() *playlists.Playlist {
	pls := makeServicePlaylist()
	pls.Idle = 5 * time.Minute
	pls.Index = 1
	pls.Playing = true
	return pls
}

func TestHandlePlaylist_OutsideLaunchRearmsAnIdlePlaylist(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	svc.State.SetActivePlaylist(makeIdlePlaylist())

	handlePlaylist(svc, nil, nil)

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got, "an idle playlist survives a launch from outside it")
	assert.True(t, got.Armed)
	assert.False(t, got.Playing)
	assert.Equal(t, 1, got.Index, "its position is kept")
	assert.Len(t, got.Items, 3)
	assert.Equal(t, 5*time.Minute, got.Idle)
}

func TestHandlePlaylist_OutsideLaunchStillClearsAnOrdinaryPlaylist(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	pls := makeServicePlaylist()
	pls.Advance = time.Minute
	pls.Playing = true
	svc.State.SetActivePlaylist(pls)

	handlePlaylist(svc, nil, nil)

	assert.Nil(t, svc.State.GetActivePlaylist())
}

func TestHandlePlaylist_ExplicitClearAndReplacementEndAnIdlePlaylist(t *testing.T) {
	t.Parallel()

	t.Run("clear", func(t *testing.T) {
		t.Parallel()
		svc := setupPlaylistTestEnv(t)
		svc.State.SetActivePlaylist(makeIdlePlaylist())
		handlePlaylist(svc, &playlists.Playlist{Slot: mediaslot.Primary, Clear: true}, nil)
		assert.Nil(t, svc.State.GetActivePlaylist())
	})

	t.Run("another playlist", func(t *testing.T) {
		t.Parallel()
		svc := setupPlaylistTestEnv(t)
		svc.PlaybackManager = &servicePlaybackRecorder{}
		svc.State.SetActivePlaylist(makeIdlePlaylist())
		other := playlists.NewPlaylist("other", "other", []playlists.PlaylistItem{{Name: "x", ZapScript: "**x"}})
		handlePlaylist(svc, other, nil)
		got := svc.State.GetActivePlaylist()
		require.NotNil(t, got)
		assert.Equal(t, "other", got.ID)
		assert.False(t, got.Armed)
	})
}

func TestHandlePlaylist_OutsideLaunchDoesNotRearmTheBackgroundSlot(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.State.SetActivePlaylist(makeIdlePlaylist())
	svc.State.SetBackgroundPlaylist(makeServicePlaylist())

	handlePlaylist(svc, &playlists.Playlist{Slot: mediaslot.Background, Clear: true}, nil)

	assert.Nil(t, svc.State.GetBackgroundPlaylist())
	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got)
	assert.False(t, got.Armed, "a background clear leaves the primary playlist as it was")
}

func TestHandlePlaylist_ReconfigureChangesOnlyRotationSettings(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeServicePlaylist()
	active.Index = 2
	active.Playing = true
	active.Advance = time.Minute
	svc.State.SetActivePlaylist(active)

	handlePlaylist(svc, playlists.StopRotating(*active), nil)

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got)
	assert.Zero(t, got.Advance)
	assert.Equal(t, 2, got.Index)
	assert.True(t, got.Playing)
	assert.Empty(t, recorder.played, "nothing is launched")
	assert.Empty(t, svc.PlaylistQueue)

	other := *active
	other.ID = "other"
	other.Advance = time.Hour
	handlePlaylist(svc, playlists.StopRotating(other), nil)
	assert.Equal(t, got, svc.State.GetActivePlaylist(), "another playlist's reconfiguration is ignored")
}

func TestPlaylistNeedsUpdate_RotationSettingsCount(t *testing.T) {
	t.Parallel()

	base := func() *playlists.Playlist {
		pls := makeServicePlaylist()
		pls.Playing = true
		return pls
	}
	tests := []struct {
		change func(*playlists.Playlist)
		name   string
		want   bool
	}{
		{name: "nothing changed", change: func(*playlists.Playlist) {}, want: false},
		{name: "advance", change: func(p *playlists.Playlist) { p.Advance = time.Minute }, want: true},
		{name: "idle", change: func(p *playlists.Playlist) { p.Idle = time.Minute }, want: true},
		{name: "countdown", change: func(p *playlists.Playlist) { p.Countdown = time.Second }, want: true},
		{name: "countdown set", change: func(p *playlists.Playlist) { p.CountdownSet = true }, want: true},
		{name: "armed", change: func(p *playlists.Playlist) { p.Armed = true }, want: true},
		{name: "force relaunch", change: func(p *playlists.Playlist) { p.ForceRelaunch = true }, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			incoming := base()
			tt.change(incoming)
			assert.Equal(t, tt.want, playlistNeedsUpdate(incoming, base()))
		})
	}
}

// A finished item is the playlist's to advance from only because the launch
// that started it told the engine so; this runs the whole path, from the
// worker's launch to the next item reaching the queue.
func TestLaunchPlaylistMedia_BindsPublishedMediaAndAdvancesWhenItFinishes(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.AutoAdvance = startAutoAdvance(svc, false)
	t.Cleanup(svc.AutoAdvance.Stop)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)

	var finish func()
	dir := t.TempDir()
	first := filepath.Join(dir, "first.rom")
	second := filepath.Join(dir, "second.rom")
	require.NoError(t, os.WriteFile(first, []byte("rom"), 0o600))
	require.NoError(t, os.WriteFile(second, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", svc.Config, first, (*platforms.Launcher)(nil), svc.DB, mock.Anything).
		Run(func(args mock.Arguments) {
			opts, isOpts := args.Get(4).(*platforms.LaunchOptions)
			require.True(t, isOpts)
			opts.ActiveMediaPublisher(models.NewActiveMedia("snes", "SNES", first, "first", "launcher"))
			finish = opts.MediaFinished
		}).Return(nil).Once()

	pls := playlists.NewPlaylist("p", "p", []playlists.PlaylistItem{
		{Name: "first", ZapScript: "**launch:" + first},
		{Name: "second", ZapScript: "**launch:" + second},
	})
	pls.Playing = true
	svc.State.SetActivePlaylist(pls)

	mockUserDB, ok := svc.DB.UserDB.(*testhelpers.MockUserDBI)
	require.True(t, ok)
	mockUserDB.On("AddHistory", mock.Anything).Return(nil).Once()

	launchPlaylistMedia(svc, pls, mocks.NewMockPlayer())
	require.NotNil(t, svc.State.ActiveMedia())
	svc.AutoAdvance.Sync()

	require.NotNil(t, finish, "the launcher is given a way to report the end")
	finish()
	svc.AutoAdvance.Sync()

	select {
	case got := <-svc.PlaylistQueue:
		assert.Equal(t, 1, got.Index)
		assert.True(t, got.EngineStarted)
	case <-time.After(time.Second):
		t.Fatal("the finished item did not advance the playlist")
	}
}

// Media that appeared outside the playlist's launch is never advanced from.
func TestPlaylistMediaStartedOutsideALaunchIsNotAdvancedFrom(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.AutoAdvance = startAutoAdvance(svc, false)
	t.Cleanup(svc.AutoAdvance.Stop)
	pls := makeServicePlaylist()
	pls.Playing = true
	svc.State.SetActivePlaylist(pls)

	svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "x.rom", "x", "launcher"))
	require.True(t, svc.State.FinishActiveMediaIf(func(*models.ActiveMedia) bool { return true }))
	svc.AutoAdvance.Sync()

	requireNothingQueued(t, svc)
}

func engineMove(from, next *playlists.Playlist) *playlists.Playlist {
	next.EngineStarted = true
	next.EngineFromIndex = from.Index
	return next
}

func TestHandlePlaylist_EngineMoveDroppedWhenTheUserGotThereFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		change func(*ServiceContext, *playlists.Playlist)
		name   string
	}{
		{name: "playlist moved to another item", change: func(svc *ServiceContext, p *playlists.Playlist) {
			svc.State.SetActivePlaylist(playlists.Goto(*p, 2))
		}},
		{name: "another playlist is open", change: func(svc *ServiceContext, p *playlists.Playlist) {
			other := *p
			other.ID = "other"
			svc.State.SetActivePlaylist(&other)
		}},
		{name: "playlist closed", change: func(svc *ServiceContext, _ *playlists.Playlist) {
			svc.State.SetActivePlaylist(nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := setupPlaylistTestEnv(t)
			recorder := &servicePlaybackRecorder{}
			svc.PlaybackManager = recorder
			active := makeServicePlaylist()
			active.Playing = true
			svc.State.SetActivePlaylist(active)
			move := engineMove(active, playlists.Next(*active))
			clearMove := engineMove(active, &playlists.Playlist{ID: active.ID, Slot: mediaslot.Primary, Clear: true})
			tt.change(svc, active)
			before := svc.State.GetActivePlaylist()

			handlePlaylist(svc, move, nil)
			handlePlaylist(svc, clearMove, nil)

			assert.Same(t, before, svc.State.GetActivePlaylist(), "the stale move changed nothing")
			assert.Empty(t, recorder.played)
		})
	}
}

func TestHandlePlaylist_EngineMoveAppliesWhileTheIdentityMatches(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)

	clearMove := engineMove(active, &playlists.Playlist{ID: active.ID, Slot: mediaslot.Primary, Clear: true})
	handlePlaylist(svc, clearMove, nil)

	assert.Nil(t, svc.State.GetActivePlaylist())
}

// A move refused because another launch is still settling is reported to the
// engine, which tries it again once that launch is over.
func TestHandlePlaylist_RefusedEngineMoveIsRetried(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	svc.AutoAdvance = startAutoAdvance(svc, false)
	t.Cleanup(svc.AutoAdvance.Stop)

	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)
	svc.AutoAdvance.ItemLaunching(active)
	svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "a.rom", "a", "launcher"))
	svc.AutoAdvance.ItemLaunched(active, nil, false)
	require.True(t, svc.State.FinishActiveMediaIf(func(*models.ActiveMedia) bool { return true }))

	first := nextQueued(t, svc)
	require.Equal(t, 1, first.Index)

	lm := svc.State.LauncherManager()
	require.True(t, lm.TryBeginPlaylistLaunch(false), "another launch is under way")
	handlePlaylist(svc, first, nil)
	assert.Equal(t, 0, svc.State.GetActivePlaylist().Index, "the refused move was not installed")
	lm.EndPlaylistLaunch()

	again := nextQueued(t, svc)
	assert.Equal(t, 1, again.Index)
	assert.True(t, again.EngineStarted)
}

func TestLauncherPublishesLater(t *testing.T) {
	t.Parallel()

	assert.True(t, launcherPublishesLater(nil), "a launcher the platform picks later may publish later")
	assert.False(t, launcherPublishesLater(&platforms.Launcher{Lifecycle: platforms.LifecycleBlocking}))
	assert.False(t, launcherPublishesLater(&platforms.Launcher{Lifecycle: platforms.LifecycleFireAndForget}))
	assert.True(t, launcherPublishesLater(&platforms.Launcher{Lifecycle: platforms.LifecycleExternal}))
	assert.False(t, launcherPublishesLater(&platforms.Launcher{
		Lifecycle: platforms.LifecycleExternal, NoActiveMedia: true,
	}), "a launcher that starts no media publishes nothing")
}

// The playlist worker is told which launcher an item resolved to as the launch
// is about to run.
func TestRunTokenZapScript_ReportsWhetherMediaMayComeLaterToThePlaylistWorker(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB, mock.Anything).
		Return(nil).Once()

	var reported []bool
	pls := makeServicePlaylist()
	err := runTokenZapScript(svc, tokens.Token{
		Text: "**launch:" + path, Source: tokens.SourcePlaylist, ScanTime: time.Now(),
	}, playlists.PlaylistController{
		Active: pls, Current: pls, Queue: make(chan *playlists.Playlist, 1),
		LaunchResolved: func(later bool) { reported = append(reported, later) },
	}, nil, false)

	require.NoError(t, err)
	require.NotEmpty(t, reported)
	assert.True(t, reported[0], "the platform picks this launcher, so its media may come later")
}

type noticeOnlyRenderer struct{}

func (noticeOnlyRenderer) PresentUI(context.Context, *models.UIEvent) (func() error, error) {
	return func() error { return nil }, nil
}

func (noticeOnlyRenderer) RendersUIKind(kind models.UIEventKind) bool {
	return kind == models.UIEventKindNotice || kind == models.UIEventKindLoader
}

type allKindsRenderer struct{ noticeOnlyRenderer }

func (allKindsRenderer) RendersUIKind(models.UIEventKind) bool { return true }

type overMediaBlockedRenderer struct{ allKindsRenderer }

func (overMediaBlockedRenderer) RendersOverMedia() bool { return false }

func TestRendersPickers(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	assert.False(t, rendersPickers(mockPlatform, nil), "no renderer")
	assert.False(t, rendersPickers(mockPlatform, &EmbeddedOptions{}), "an embedded host with no renderer")
	assert.False(t, rendersPickers(mockPlatform, &EmbeddedOptions{Renderer: noticeOnlyRenderer{}}),
		"a host that shows only notices cannot announce a switch")
	assert.True(t, rendersPickers(mockPlatform, &EmbeddedOptions{Renderer: allKindsRenderer{}}))
	assert.False(t, rendersPickers(mockPlatform, &EmbeddedOptions{Renderer: overMediaBlockedRenderer{}}),
		"a display that replaces the running game cannot announce a switch away from it")
}

func userStop(svc *ServiceContext) {
	svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "a.rom", "a", "launcher"))
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)
}

// A stop that lands after the engine decided on a move but before the queue
// handler got to it cancels the move.
func TestHandlePlaylist_EngineMoveDroppedByAStopAfterTheDecision(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeIdlePlaylist()
	svc.State.SetActivePlaylist(active)
	move := engineMove(active, playlists.Next(*active))
	move.StopSeq = svc.State.UserStopSeq()
	move.StopStamped = true

	userStop(svc)
	handlePlaylist(svc, move, nil)

	assert.Same(t, active, svc.State.GetActivePlaylist(), "the move changed nothing")
	assert.Empty(t, recorder.played)
}

func TestHandlePlaylist_EngineMoveAppliesWhenNoStopCameAfter(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	userStop(svc)
	active := makeIdlePlaylist()
	svc.State.SetActivePlaylist(active)
	move := engineMove(active, playlists.Next(*active))
	move.StopSeq = svc.State.UserStopSeq()
	move.StopStamped = true

	mockUserDB, ok := svc.DB.UserDB.(*testhelpers.MockUserDBI)
	require.True(t, ok)
	mockUserDB.On("AddHistory", mock.Anything).Return(nil).Maybe()

	handlePlaylist(svc, move, nil)
	svc.BackgroundWG.Wait()

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got)
	assert.Equal(t, 2, got.Index)
}

// The stop and its re-arm both came first: the playlist is waiting again, and
// a move decided while it was playing must not start it.
func TestHandlePlaylist_EngineMoveDroppedAfterTheStopRearmedThePlaylist(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeIdlePlaylist()
	svc.State.SetActivePlaylist(active)
	move := engineMove(active, playlists.Next(*active))
	move.StopSeq = svc.State.UserStopSeq()
	move.StopStamped = true

	handlePlaylist(svc, nil, nil) // the stop's re-arm, with no media to count
	rearmed := svc.State.GetActivePlaylist()
	require.True(t, rearmed.Armed)
	handlePlaylist(svc, move, nil)

	assert.Same(t, rearmed, svc.State.GetActivePlaylist())
	assert.Empty(t, recorder.played)
}

// The move was installed and its launch queued; a stop, or the re-arm that
// follows it, lands before the launch starts, and nothing is launched.
func TestLaunchPlaylistMedia_AbortsAnEngineMoveOvertakenBeforeItLaunches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		overtake func(svc *ServiceContext)
		name     string
	}{
		{name: "a stop", overtake: userStop},
		{name: "the stop's re-arm", overtake: func(svc *ServiceContext) { handlePlaylist(svc, nil, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := setupPlaylistTestEnv(t)
			svc.PlaybackManager = &servicePlaybackRecorder{}
			active := makeIdlePlaylist()
			svc.State.SetActivePlaylist(active)
			installed := engineMove(active, playlists.Next(*active))
			installed.StopSeq = svc.State.UserStopSeq()
			installed.StopStamped = true
			svc.State.SetActivePlaylist(installed)

			tt.overtake(svc)
			// The platform mock has no LaunchMedia expectation: launching
			// would fail the test.
			launchPlaylistMedia(svc, installed, mocks.NewMockPlayer())

			mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
			require.True(t, ok)
			mockPlatform.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything,
				mock.Anything, mock.Anything, mock.Anything)
			require.NotNil(t, svc.State.GetActivePlaylist(), "the playlist is not cleared")
		})
	}
}

func TestEngineLaunchOvertaken(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	active := makeIdlePlaylist()
	active.EngineStarted = true
	active.StopStamped = true
	active.StopSeq = svc.State.UserStopSeq()
	active.StopStamped = true
	svc.State.SetActivePlaylist(active)

	assert.False(t, launchOvertaken(svc, active))
	assert.False(t, launchOvertaken(svc, makeIdlePlaylist()), "only the engine's own moves are checked")

	svc.State.RecordMediaStop(models.MediaStopReasonStopped) // no media: still a stop
	assert.True(t, launchOvertaken(svc, active), "a stop with nothing running overtakes it too")

	fresh := *active
	fresh.StopSeq = svc.State.UserStopSeq()
	fresh.StopStamped = true
	assert.True(t, launchOvertaken(svc, &fresh), "another playlist value is open")
}

// The launch is called off at the last point before it takes the launch gate:
// the stop landed after the worker's first check.
func TestRunTokenZapScript_EngineLaunchOvertakenAtTheGate(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))

	active := makeIdlePlaylist()
	active.EngineStarted = true
	active.StopSeq = svc.State.UserStopSeq()
	active.StopStamped = true
	svc.State.SetActivePlaylist(active)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)

	err := runTokenZapScript(svc, tokens.Token{
		Text: "**launch:" + path, Source: tokens.SourcePlaylist, ScanTime: time.Now(),
	}, playlists.PlaylistController{
		Active: active, Current: active, Queue: make(chan *playlists.Playlist, 1),
	}, nil, false)

	require.NoError(t, err, "a launch that is called off is not a failure")
	mockPlatform.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything)
}

func stampedAt(svc *ServiceContext, pls *playlists.Playlist) *playlists.Playlist {
	pls.StopSeq = svc.State.UserStopSeq()
	pls.StopStamped = true
	return pls
}

// A playlist command returns once its update is queued; the launch happens
// later in the queue worker. A stop in that gap cancels the update, and the
// state the stop leaves stands.
func TestHandlePlaylist_UpdateIssuedBeforeAStopIsDropped(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)
	next := stampedAt(svc, playlists.Next(*active))

	svc.State.RecordMediaStop(models.MediaStopReasonStopped) // the stop; nothing was running
	handlePlaylist(svc, nil, nil)                            // and its clear
	handlePlaylist(svc, next, nil)

	assert.Nil(t, svc.State.GetActivePlaylist(), "the stale update does not bring the playlist back")
	assert.Empty(t, recorder.played)
}

func TestHandlePlaylist_UpdateIssuedAfterAStopStillLaunches(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	mockUserDB, ok := svc.DB.UserDB.(*testhelpers.MockUserDBI)
	require.True(t, ok)
	mockUserDB.On("AddHistory", mock.Anything).Return(nil).Maybe()
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)

	// "**stop||**playlist.play": the play is issued after the stop.
	handlePlaylist(svc, stampedAt(svc, playlists.Next(*active)), nil)
	svc.BackgroundWG.Wait()

	assert.Equal(t, 1, svc.State.GetActivePlaylist().Index)
}

// Only an update that launches is cancelled: a pause or a cursor move on a
// paused playlist is stamped before the stop its own command makes.
func TestHandlePlaylist_UpdatesThatLaunchNothingSurviveAStop(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)

	pause := stampedAt(svc, playlists.Pause(*active))
	svc.State.RecordMediaStop(models.MediaStopReasonStopped) // the pause command's own stop
	handlePlaylist(svc, pause, nil)

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got)
	assert.False(t, got.Playing)

	moved := stampedAt(svc, playlists.Next(*got))
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	handlePlaylist(svc, moved, nil)
	assert.Equal(t, 1, svc.State.GetActivePlaylist().Index, "next on a paused playlist only moves the cursor")
}

// The whole chain: the command stamps its update from the real stop count, and
// a stop after that cancels it.
func TestRunTokenZapScript_PlaylistCommandUpdateIsStampedAndCancelledByAStop(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	recorder := &servicePlaybackRecorder{}
	svc.PlaybackManager = recorder
	active := makeServicePlaylist()
	active.Playing = true
	svc.State.SetActivePlaylist(active)
	queue := make(chan *playlists.Playlist, 1)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)

	require.NoError(t, runTokenZapScript(svc, tokens.Token{
		Text: "**playlist.next", Source: tokens.SourceAPI, ScanTime: time.Now(),
	}, playlists.PlaylistController{Active: active, Queue: queue}, nil, false))
	issued := <-queue
	assert.True(t, issued.StopStamped)
	assert.Equal(t, svc.State.UserStopSeq(), issued.StopSeq)

	svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	handlePlaylist(svc, issued, nil)

	assert.Equal(t, 0, svc.State.GetActivePlaylist().Index)
	assert.Empty(t, recorder.played)
}

// The worker has the update and is about to launch when the stop lands.
func TestRunTokenZapScript_UserLaunchOvertakenAtTheGate(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	active := makeServicePlaylist()
	active.Playing = true
	stampedAt(svc, active)
	svc.State.SetActivePlaylist(active)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)

	err := runTokenZapScript(svc, tokens.Token{
		Text: "**launch:" + path, Source: tokens.SourcePlaylist, ScanTime: time.Now(),
	}, playlists.PlaylistController{
		Active: active, Current: active, Queue: make(chan *playlists.Playlist, 1),
	}, nil, false)

	require.NoError(t, err)
	mockPlatform.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything)
}

func TestLaunchPlaylistMedia_UserUpdateOvertakenByAStopDoesNotLaunch(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	active := makeServicePlaylist()
	active.Playing = true
	stampedAt(svc, active)
	svc.State.SetActivePlaylist(active)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)

	launchPlaylistMedia(svc, active, mocks.NewMockPlayer())

	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	mockPlatform.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything)
}

// The engine decides a move while a slow stop is running; by the time the stop
// completes and the launch gets the gate, the move is called off, and the
// playlist the stop is clearing ends cleared with nothing launched.
func TestRunTokenZapScript_EngineMoveDecidedDuringAStopIsCalledOffWhenItCompletes(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))

	active := makeIdlePlaylist()
	svc.State.SetActivePlaylist(active)

	// The stop begins: it holds the gate and has counted itself.
	releaseStop, err := svc.State.AcquireMediaStop(t.Context())
	require.NoError(t, err)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)

	// The advance deadline fires now, and the engine stamps its move.
	move := engineMove(active, playlists.Next(*active))
	move.StopSeq = svc.State.StopEpoch()
	move.StopStamped = true

	// The move reaches the worker, is installed, and its launch waits on the gate.
	svc.State.SetActivePlaylist(move)
	done := make(chan error, 1)
	go func() {
		done <- runTokenZapScript(svc, tokens.Token{
			Text: "**launch:" + path, Source: tokens.SourcePlaylist, ScanTime: time.Now(),
		}, playlists.PlaylistController{
			Active: active, Current: move, Queue: make(chan *playlists.Playlist, 1),
		}, nil, false)
	}()

	releaseStop() // the stop completes
	require.NoError(t, <-done, "a launch that is called off is not a failure")
	mockPlatform.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything)

	// What the stop was doing: clearing the playlist.
	svc.State.SetActivePlaylist(active)
	svc.PlaybackManager = &servicePlaybackRecorder{}
	handlePlaylist(svc, move, nil)
	assert.Same(t, active, svc.State.GetActivePlaylist(), "the stale move installed nothing")
	handlePlaylist(svc, &playlists.Playlist{Slot: mediaslot.Primary, Clear: true}, nil)
	assert.Nil(t, svc.State.GetActivePlaylist())
}

// A move decided after the stop completed, or with no stop at all, launches.
func TestRunTokenZapScript_EngineMoveDecidedAfterAStopStillLaunches(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB, mock.Anything).
		Return(nil).Once()

	releaseStop, err := svc.State.AcquireMediaStop(t.Context())
	require.NoError(t, err)
	svc.State.RecordMediaStop(models.MediaStopReasonStopped)
	releaseStop()

	active := makeIdlePlaylist()
	svc.State.SetActivePlaylist(active)
	move := engineMove(active, playlists.Next(*active))
	move.StopSeq = svc.State.StopEpoch()
	move.StopStamped = true

	svc.State.SetActivePlaylist(move)
	require.NoError(t, runTokenZapScript(svc, tokens.Token{
		Text: "**launch:" + path, Source: tokens.SourcePlaylist, ScanTime: time.Now(),
	}, playlists.PlaylistController{
		Active: move, Current: move, Queue: make(chan *playlists.Playlist, 1),
	}, nil, false))
	mockPlatform.AssertCalled(t, "LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB, mock.Anything)
}
