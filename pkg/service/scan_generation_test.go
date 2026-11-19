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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// settled reports whether c completed within d, and with what.
func settled(c *tokens.Completion, d time.Duration) (done bool, err error) {
	select {
	case err := <-c.Done():
		return true, err
	case <-time.After(d):
		return false, nil
	}
}

func TestHandleQueuedToken_NewerScanCutsEarlierDelayShort(t *testing.T) {
	t.Parallel()

	for _, superseding := range []string{"**playlist.stop", "**launch.random:NES"} {
		t.Run(superseding, func(t *testing.T) {
			t.Parallel()
			env := setupRunFailedEnv(t)
			env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

			first := env.queue(t, tokens.SourceReader, "**delay:20s||**playlist.stop")
			done, _ := settled(first, 100*time.Millisecond)
			require.False(t, done, "the delay is still pending")

			// The newer scan's own result does not matter here: a launch
			// with no media to pick fails, but it still supersedes.
			env.queue(t, tokens.SourceReader, superseding)
			done, err := settled(first, runFailedTimeout)
			require.True(t, done, "the newer scan ended the delay")
			require.NoError(t, err, "a delay cut short is not a failure")
		})
	}
}

func TestHandleQueuedToken_CutShortDelayIsNotAFailure(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**playlist.stop")
	env.queue(t, tokens.SourceReader, "**playlist.stop")
	require.NoError(t, waitRunCompletion(t, first))
	env.expectNoRunFailed(t)
}

func TestHandleQueuedToken_CommandOnlyScanLeavesDelayAlone(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**echo:after")
	second := env.queue(t, tokens.SourceReader, "**echo:volume")
	require.NoError(t, waitRunCompletion(t, second))
	done, _ := settled(first, 200*time.Millisecond)
	assert.False(t, done, "a command-only scan does not end the delay")

	// A scan that does supersede it still can.
	env.queue(t, tokens.SourceReader, "**playlist.stop")
	done, _ = settled(first, runFailedTimeout)
	assert.True(t, done)
}

func TestHandleQueuedToken_PlaylistItemLeavesDelayAlone(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**echo:after")
	item := env.queue(t, tokens.SourcePlaylist, "**playlist.stop")
	require.NoError(t, waitRunCompletion(t, item))
	done, _ := settled(first, 200*time.Millisecond)
	assert.False(t, done, "a playlist's own item does not end the delay")
}

// runSuperseded runs script under a scan generation a newer scan has already
// ended, as the script of an earlier tap is when the next one arrives.
func runSuperseded(t *testing.T, env *exitTestEnv, script string) error {
	t.Helper()
	parent := env.svc.State.GetContext()
	earlier := env.svc.scans.current(parent)
	env.svc.scans.supersede(parent)
	return runTokenZapScriptWithContext(withScanContext(parent, earlier), env.svc, tokens.Token{
		UID:      "card",
		Text:     script,
		ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 10)}, nil, false)
}

func TestRunTokenZapScript_CutShortDelaySkipsTheRest(t *testing.T) {
	t.Parallel()
	env := setupExitTestEnv(t, keypressHook)

	require.NoError(t, runSuperseded(t, env, "**delay:20s||**launch:"+env.gamePath))
	assert.Zero(t, env.rec.count("launch"), "nothing after the delay runs")
}

// A before_media_start hook's delay cut short ends the launch it was run for:
// the newer scan owns what plays now.
func TestRunTokenZapScript_CutShortHookDelayEndsTheLaunch(t *testing.T) {
	t.Parallel()
	env := setupExitTestEnv(t, keypressHook)
	require.NoError(t, env.svc.Config.LoadTOML(`[launchers]
before_media_start = "**delay:20s"`))

	require.NoError(t, runSuperseded(t, env, "**launch:"+env.gamePath), "a cut-short delay is not a failure")
	assert.Zero(t, env.rec.count("launch"))
}
