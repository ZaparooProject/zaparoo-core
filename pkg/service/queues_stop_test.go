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

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCommandStopsPrimaryNow(t *testing.T) {
	t.Parallel()

	parse := func(text string) gozapscript.Command {
		script, err := gozapscript.NewParser(text).ParseScript()
		require.NoError(t, err)
		return script.Cmds[0]
	}
	assert.True(t, commandStopsPrimaryNow(parse("**stop")))
	assert.True(t, commandStopsPrimaryNow(parse("**playlist.stop")))
	assert.True(t, commandStopsPrimaryNow(parse("**playlist.pause")))
	assert.True(t, commandStopsPrimaryNow(parse("**launch.system:menu")))
	assert.False(t, commandStopsPrimaryNow(parse("**stop?slot=background")))
	assert.False(t, commandStopsPrimaryNow(parse("**launch.system:snes")))
	assert.False(t, commandStopsPrimaryNow(parse("**launch:/x/y.rom")))
	assert.False(t, commandStopsPrimaryNow(parse("**playlist.next")))
}

// A stop run while a launch holds the launch gate waits for it, so it ends the
// media the launch publishes instead of finding nothing to stop.
func TestRunTokenZapScript_StopWaitsForALaunchInFlight(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	returns := func() int {
		count := 0
		for _, call := range mockPlatform.Calls {
			if call.Method == "ReturnToMenu" {
				count++
			}
		}
		return count
	}

	access, err := svc.State.AcquireMediaLaunch()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	failed := make(chan error, 1)
	go func() {
		failed <- runTokenZapScriptWithContext(ctx, svc, tokens.Token{
			Text: "**stop", Source: tokens.SourceAPI, ScanTime: time.Now(),
		}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}, nil, false)
	}()

	// The only way out while the launch holds the gate is the context.
	cancel()
	require.ErrorIs(t, <-failed, context.Canceled)
	assert.Zero(t, returns(), "the stop did not run while the launch was in flight")

	access.Release()
	require.NoError(t, runTokenZapScript(svc, tokens.Token{
		Text: "**stop", Source: tokens.SourceAPI, ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}, nil, false))
	assert.Equal(t, 1, returns(), "once the launch is over the stop runs")
}

// A launch that nobody queued, such as a card scan, is called off by a stop
// that comes before its media appears.
func TestRunTokenZapScript_ScanLaunchOvertakenByAStopPublishesNothing(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	stopped := make(chan struct{}, 1)
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).
		Run(func(mock.Arguments) { stopped <- struct{}{} }).Return(nil).Once()
	path := filepath.Join(t.TempDir(), "game.rom")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB, mock.Anything).
		Run(func(args mock.Arguments) {
			opts, isOpts := args.Get(4).(*platforms.LaunchOptions)
			require.True(t, isOpts)
			// The stop lands while the launcher is starting the process.
			svc.State.RecordMediaStop(models.MediaStopReasonStopped)
			opts.ActiveMediaPublisher(models.NewActiveMedia("snes", "SNES", path, "game", "launcher"))
		}).Return(nil).Once()

	err := runTokenZapScript(svc, tokens.Token{
		Text: "**launch:" + path, Source: tokens.SourceReader, ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}, nil, false)

	require.NoError(t, err)
	assert.Nil(t, svc.State.ActiveMedia())
	<-stopped
}

// A stop command that panics and is recovered must not leave the stop gate
// locked, or every later launch would wait on it forever.
func TestRunTokenZapScript_PanickingStopStillOpensTheStopGate(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	kept := mockPlatform.ExpectedCalls[:0]
	for _, call := range mockPlatform.ExpectedCalls {
		if call.Method != "ReturnToMenu" {
			kept = append(kept, call)
		}
	}
	mockPlatform.ExpectedCalls = kept
	mockPlatform.On("ReturnToMenu").Run(func(mock.Arguments) { panic("the menu failed to open") }).Return(nil)

	err := runTokenZapScriptRecovering(svc, tokens.Token{
		Text: "**stop", Source: tokens.SourceAPI, ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)})
	require.ErrorIs(t, err, errLaunchPanicked)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	release, gateErr := svc.State.AcquireMediaStop(ctx)
	require.NoError(t, gateErr, "the stop gate must be open once the command is over")
	release()
}
