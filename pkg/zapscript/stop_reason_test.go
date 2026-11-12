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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stopLog records, in order, the stop recordings and the platform stops they
// must precede.
type stopLog struct{ events []string }

func (l *stopLog) record(reason string) func() {
	l.events = append(l.events, "record:"+reason)
	return func() { l.events = append(l.events, "cancel") }
}

func (l *stopLog) stopped(_ mock.Arguments) { l.events = append(l.events, "stop") }

func TestStopPaths_RecordStopBeforeStopping(t *testing.T) {
	t.Parallel()

	wantStopped := []string{"record:" + models.MediaStopReasonStopped, "stop"}

	t.Run("stop command", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := mocks.NewMockPlatform()
		pl.On("ReturnToMenu").Run(log.stopped).Return(nil).Once()
		env, _ := stopEnv("")
		env.RecordMediaStop = log.record

		_, err := cmdStop(pl, env)
		require.NoError(t, err)
		assert.Equal(t, wantStopped, log.events)
	})

	t.Run("stop command on background slot records nothing", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := mocks.NewMockPlatform()
		env, _ := stopEnv("background")
		env.RecordMediaStop = log.record

		_, err := cmdStop(pl, env)
		require.NoError(t, err)
		assert.Empty(t, log.events)
	})

	t.Run("launch.system menu", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := mocks.NewMockPlatform()
		pl.On("ReturnToMenu").Run(log.stopped).Return(nil).Once()

		_, err := cmdSystem(pl, platforms.CmdEnv{
			Cmd:             zapscript.Command{Args: []string{platforms.SystemMenu}},
			RecordMediaStop: log.record,
		})
		require.NoError(t, err)
		assert.Equal(t, wantStopped, log.events)
	})

	t.Run("playlist.stop", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := newPlaylistTestPlatform()
		pl.On("StopActiveLauncher", platforms.StopForMenu).Run(log.stopped).Return(nil).Once()
		pls, queue := makePlaylistEnv()

		_, err := cmdPlaylistStop(pl, platforms.CmdEnv{
			Playlist:        playlists.PlaylistController{Active: pls, Queue: queue},
			RecordMediaStop: log.record,
		})
		require.NoError(t, err)
		assert.Equal(t, wantStopped, log.events)
	})

	t.Run("playlist.pause", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := newPlaylistTestPlatform()
		pl.On("StopActiveLauncher", platforms.StopForMenu).Run(log.stopped).Return(nil).Once()
		pls, queue := makePlaylistEnv()
		pls.Playing = true

		_, err := cmdPlaylistPause(pl, platforms.CmdEnv{
			Playlist:        playlists.PlaylistController{Active: pls, Queue: queue},
			RecordMediaStop: log.record,
		})
		require.NoError(t, err)
		assert.Equal(t, wantStopped, log.events)
	})

	for _, tt := range []struct {
		name   string
		action string
		want   []string
	}{
		{name: "control stop", action: platforms.ControlStop, want: []string{"record:stopped", "control"}},
		{name: "control pause", action: platforms.ControlPause, want: []string{"control"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &stopLog{}
			ctl := platforms.Control{
				Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
					log.events = append(log.events, "control")
					return nil
				},
			}
			_, err := cmdControl(mocks.NewMockPlatform(), platforms.CmdEnv{
				Cmd:     zapscript.Command{Name: "control", Args: []string{tt.action}},
				ExprEnv: newControlExprEnv("test-launcher"),
				LauncherCache: newControlLauncherCache([]platforms.Launcher{{
					ID:       "test-launcher",
					Controls: map[string]platforms.Control{platforms.ControlStop: ctl, platforms.ControlPause: ctl},
				}}),
				RecordMediaStop: log.record,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, log.events)
		})
	}
}

type launchEndRecorder struct{ ends []state.MediaEnd }

func (*launchEndRecorder) MediaStarted(state.MediaStart) {}

//nolint:gocritic // state.MediaObserver passes events by value
func (r *launchEndRecorder) MediaEnded(event state.MediaEnd) { r.ends = append(r.ends, event) }

// A launcher that calls the MediaFinished it was handed reaches observers as
// finished, for a launch that goes through the real state's launch gate.
func TestLaunchClosureSuppliesMediaFinished(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		slot       string
		wantFinish bool
	}{
		{name: "primary launch", slot: "", wantFinish: true},
		{name: "background launch gets none", slot: "background", wantFinish: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Instance{}
			path := filepath.Join("games", "game.sfc")
			pl := mocks.NewMockPlatform()
			st, _ := state.NewState(pl, "test-boot")
			t.Cleanup(st.StopService)
			rec := &launchEndRecorder{}
			st.AddMediaObserver(rec)

			var gotFinished, gotRecord bool
			pl.On("LaunchMedia", cfg, path, (*platforms.Launcher)(nil), (*database.Database)(nil), mock.Anything).
				Run(func(args mock.Arguments) {
					opts, ok := args.Get(4).(*platforms.LaunchOptions)
					require.True(t, ok)
					gotFinished = opts.MediaFinished != nil
					gotRecord = opts.RecordMediaStop != nil
					assert.Equal(t, tt.wantFinish, opts.MediaExited != nil)
					opts.ActiveMediaPublisher(models.NewActiveMedia("SNES", "SNES", path, "Game", "emulator"))
					if opts.MediaFinished != nil {
						opts.MediaFinished()
						opts.MediaFinished()
					}
				}).Return(nil).Once()

			advArgs := map[string]string(nil)
			if tt.slot != "" {
				advArgs = map[string]string{string(zapscript.KeySlot): tt.slot}
			}
			env := platforms.CmdEnv{
				Cfg:                cfg,
				Cmd:                zapscript.Command{AdvArgs: zapscript.NewAdvArgs(advArgs)},
				AcquireMediaLaunch: st.AcquireMediaLaunch,
			}
			launch := getLaunchClosure(pl, &env, true)
			require.NoError(t, launch(launchTarget{path: path, systemID: "SNES"}))

			assert.Equal(t, tt.wantFinish, gotFinished)
			assert.True(t, gotRecord)
			if !tt.wantFinish {
				return
			}
			require.Len(t, rec.ends, 1, "the second call must be a no-op")
			assert.Equal(t, models.MediaStopReasonFinished, rec.ends[0].Reason)
			assert.Nil(t, st.ActiveMedia())
		})
	}
}

func TestStopPaths_FailedStopWithdrawsNote(t *testing.T) {
	t.Parallel()

	want := []string{"record:" + models.MediaStopReasonStopped, "stop", "cancel"}
	failed := platforms.ErrStopFailed

	t.Run("stop command", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := mocks.NewMockPlatform()
		pl.On("ReturnToMenu").Run(log.stopped).Return(failed).Once()
		env, _ := stopEnv("")
		env.RecordMediaStop = log.record

		_, err := cmdStop(pl, env)
		require.Error(t, err)
		assert.Equal(t, want, log.events)
	})

	t.Run("launch.system menu", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := mocks.NewMockPlatform()
		pl.On("ReturnToMenu").Run(log.stopped).Return(failed).Once()

		_, err := cmdSystem(pl, platforms.CmdEnv{
			Cmd:             zapscript.Command{Args: []string{platforms.SystemMenu}},
			RecordMediaStop: log.record,
		})
		require.Error(t, err)
		assert.Equal(t, want, log.events)
	})

	t.Run("playlist.stop", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := newPlaylistTestPlatform()
		pl.On("StopActiveLauncher", platforms.StopForMenu).Run(log.stopped).Return(failed).Once()
		pls, queue := makePlaylistEnv()

		_, err := cmdPlaylistStop(pl, platforms.CmdEnv{
			Playlist:        playlists.PlaylistController{Active: pls, Queue: queue},
			RecordMediaStop: log.record,
		})
		require.Error(t, err)
		assert.Equal(t, want, log.events)
	})

	t.Run("playlist.pause", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		pl := newPlaylistTestPlatform()
		pl.On("StopActiveLauncher", platforms.StopForMenu).Run(log.stopped).Return(failed).Once()
		pls, queue := makePlaylistEnv()
		pls.Playing = true

		_, err := cmdPlaylistPause(pl, platforms.CmdEnv{
			Playlist:        playlists.PlaylistController{Active: pls, Queue: queue},
			RecordMediaStop: log.record,
		})
		require.Error(t, err)
		assert.Equal(t, want, log.events)
	})

	t.Run("control stop", func(t *testing.T) {
		t.Parallel()
		log := &stopLog{}
		ctl := platforms.Control{
			Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
				log.events = append(log.events, "stop")
				return failed
			},
		}
		_, err := cmdControl(mocks.NewMockPlatform(), platforms.CmdEnv{
			Cmd:     zapscript.Command{Name: "control", Args: []string{platforms.ControlStop}},
			ExprEnv: newControlExprEnv("test-launcher"),
			LauncherCache: newControlLauncherCache([]platforms.Launcher{{
				ID: "test-launcher", Controls: map[string]platforms.Control{platforms.ControlStop: ctl},
			}}),
			RecordMediaStop: log.record,
		})
		require.Error(t, err)
		assert.Equal(t, want, log.events)
	})
}

func TestLaunchClosureMarksPlaylistItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		slot   string
		want   bool
	}{
		{name: "playlist item", source: tokens.SourcePlaylist, want: true},
		{name: "reader scan", source: tokens.SourceReader},
		{name: "background playlist item", source: tokens.SourcePlaylist, slot: "background"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Instance{}
			path := filepath.Join("games", "game.sfc")
			pl := mocks.NewMockPlatform()
			st, _ := state.NewState(pl, "test-boot")
			t.Cleanup(st.StopService)

			var got, finishedSet bool
			pl.On("LaunchMedia", cfg, path, (*platforms.Launcher)(nil), (*database.Database)(nil), mock.Anything).
				Run(func(args mock.Arguments) {
					opts, ok := args.Get(4).(*platforms.LaunchOptions)
					require.True(t, ok)
					got = opts.PlaylistItem
					finishedSet = opts.MediaFinished != nil
				}).Return(nil).Once()

			advArgs := map[string]string(nil)
			if tt.slot != "" {
				advArgs = map[string]string{string(zapscript.KeySlot): tt.slot}
			}
			env := platforms.CmdEnv{
				Cfg:                cfg,
				Source:             tt.source,
				Cmd:                zapscript.Command{AdvArgs: zapscript.NewAdvArgs(advArgs)},
				AcquireMediaLaunch: st.AcquireMediaLaunch,
			}
			launch := getLaunchClosure(pl, &env, true)
			require.NoError(t, launch(launchTarget{path: path, systemID: "SNES"}))

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.slot == "", finishedSet, "MediaFinished is supplied for every primary launch")
		})
	}
}
