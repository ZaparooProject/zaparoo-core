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
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/autoadvance"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inputPlatform is a mock platform that can see input of the given classes.
type inputPlatform struct {
	*mocks.MockPlatform
	coverage platforms.InputClass
}

func (p inputPlatform) InputCoverage() platforms.InputClass { return p.coverage }

func (inputPlatform) WatchInput(context.Context, bool, func()) error { return nil }

func newInputPlatform(coverage platforms.InputClass) inputPlatform {
	return inputPlatform{MockPlatform: newPlaylistTestPlatform(), coverage: coverage}
}

const rotationPlaylistJSON = `{"id":"rot","name":"Rotation","items":[` +
	`{"name":"One","zapscript":"**echo:1"},{"name":"Two","zapscript":"**echo:2"}]}`

func rotationEnv(name string, args map[string]string) (env platforms.CmdEnv, queue chan *playlists.Playlist) {
	queue = make(chan *playlists.Playlist, 1)
	cmd := zapscript.Command{Name: name, Args: []string{rotationPlaylistJSON}, AdvArgs: zapscript.NewAdvArgs(args)}
	return platforms.CmdEnv{
		Cfg:      &config.Instance{},
		Cmd:      cmd,
		Playlist: playlists.PlaylistController{Queue: queue},
	}, queue
}

func TestPlaylistRotationArgs(t *testing.T) {
	t.Parallel()

	full := newInputPlatform(platforms.InputAll)
	tests := []struct {
		pl      platforms.Platform
		check   func(t *testing.T, pls *playlists.Playlist)
		wantErr error
		args    map[string]string
		name    string
	}{
		{
			name: "advance", pl: full, args: map[string]string{"advance": "5m"},
			check: func(t *testing.T, pls *playlists.Playlist) {
				assert.Equal(t, 5*time.Minute, pls.Advance)
				assert.Zero(t, pls.Idle)
				assert.False(t, pls.CountdownSet)
			},
		},
		{
			name: "idle", pl: full, args: map[string]string{"idle": "90s"},
			check: func(t *testing.T, pls *playlists.Playlist) {
				assert.Equal(t, 90*time.Second, pls.Idle)
				assert.Zero(t, pls.Advance)
			},
		},
		{
			name: "countdown zero is an explicit setting", pl: full, args: map[string]string{"countdown": "0"},
			check: func(t *testing.T, pls *playlists.Playlist) {
				assert.True(t, pls.CountdownSet)
				assert.Zero(t, pls.Countdown)
				assert.Zero(t, pls.EffectiveCountdown())
			},
		},
		{
			name: "countdown", pl: full, args: map[string]string{"advance": "1h", "countdown": "3s"},
			check: func(t *testing.T, pls *playlists.Playlist) {
				assert.Equal(t, 3*time.Second, pls.Countdown)
				assert.True(t, pls.CountdownSet)
			},
		},
		{name: "limit is accepted", pl: full, args: map[string]string{"advance": "24h", "countdown": "24h"}},
		{
			name: "advance and idle together", pl: full, wantErr: ErrRotationConflict,
			args: map[string]string{"advance": "5m", "idle": "5m"},
		},
		{name: "advance zero", pl: full, wantErr: ErrInvalidRotation, args: map[string]string{"advance": "0"}},
		{name: "idle under a second", pl: full, wantErr: ErrInvalidRotation, args: map[string]string{"idle": "500ms"}},
		{name: "advance over a day", pl: full, wantErr: ErrInvalidRotation, args: map[string]string{"advance": "25h"}},
		{
			name: "countdown over a day", pl: full, wantErr: ErrInvalidRotation,
			args: map[string]string{"countdown": "25h"},
		},
		{name: "not a duration", pl: full, wantErr: nil, args: map[string]string{"advance": "soon"}},
		{
			name: "idle without an input source", pl: newPlaylistTestPlatform(),
			wantErr: autoadvance.ErrIdleUnsupported, args: map[string]string{"idle": "5m"},
		},
		{
			name: "idle with partial coverage", pl: newInputPlatform(platforms.InputKeyboard | platforms.InputMouse),
			wantErr: autoadvance.ErrIdleUnsupported, args: map[string]string{"idle": "5m"},
		},
		{
			name: "advance does not need input", pl: newPlaylistTestPlatform(),
			args: map[string]string{"advance": "5m"},
		},
		{
			name: "advance on the background slot", pl: full, wantErr: ErrBackgroundRotation,
			args: map[string]string{"advance": "5m", "slot": "background"},
		},
		{
			name: "idle on the background slot", pl: full, wantErr: ErrBackgroundRotation,
			args: map[string]string{"idle": "5m", "slot": "background"},
		},
		{
			name: "countdown on the background slot", pl: full,
			args: map[string]string{"countdown": "3s", "slot": "background"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, _ := rotationEnv(zapscript.ZapScriptCmdPlaylistLoad, tt.args)
			pls, err := loadPlaylist(tt.pl, env)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.name == "not a duration" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, pls)
			}
		})
	}
}

func TestIdleUnsupportedMessageNamesTheInputs(t *testing.T) {
	t.Parallel()

	assert.Contains(t, autoadvance.ErrIdleUnsupported.Error(), "keyboard, mouse and controller")
}

func TestCmdPlaylistLoad_IdleArmsAndAdvanceDoesNot(t *testing.T) {
	t.Parallel()

	pl := newInputPlatform(platforms.InputAll)

	env, queue := rotationEnv(zapscript.ZapScriptCmdPlaylistLoad, map[string]string{"idle": "5m"})
	_, err := cmdPlaylistLoad(pl, env)
	require.NoError(t, err)
	armed := <-queue
	assert.True(t, armed.Armed, "a loaded idle playlist waits for the device to go quiet")
	assert.False(t, armed.Playing)

	env, queue = rotationEnv(zapscript.ZapScriptCmdPlaylistLoad, map[string]string{"advance": "5m"})
	_, err = cmdPlaylistLoad(pl, env)
	require.NoError(t, err)
	loaded := <-queue
	assert.False(t, loaded.Armed)
	assert.False(t, loaded.Playing, "an advance playlist does nothing until it is played")
}

func TestCmdPlaylistPlay_IdleStartsNow(t *testing.T) {
	t.Parallel()

	env, queue := rotationEnv(zapscript.ZapScriptCmdPlaylistPlay, map[string]string{"idle": "5m"})
	_, err := cmdPlaylistPlay(newInputPlatform(platforms.InputAll), env)
	require.NoError(t, err)
	got := <-queue
	assert.True(t, got.Playing)
	assert.False(t, got.Armed)
	assert.Equal(t, 5*time.Minute, got.Idle)
}

func TestCmdPlaylistPlay_IdleRefusedWithoutFullCoverage(t *testing.T) {
	t.Parallel()

	env, queue := rotationEnv(zapscript.ZapScriptCmdPlaylistPlay, map[string]string{"idle": "5m"})
	_, err := cmdPlaylistPlay(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, autoadvance.ErrIdleUnsupported)
	assert.Empty(t, queue, "nothing is queued for a refused command")
}

func TestCmdPlaylistOpen_IdleArms(t *testing.T) {
	t.Parallel()

	env, queue := rotationEnv(zapscript.ZapScriptCmdPlaylistOpen, map[string]string{"idle": "5m"})
	_, err := cmdPlaylistOpen(newInputPlatform(platforms.InputAll), env)
	require.NoError(t, err)
	assert.True(t, (<-queue).Armed)
}

func randomRotationEnv(args map[string]string) (env platforms.CmdEnv, queue chan *playlists.Playlist) {
	queue = make(chan *playlists.Playlist, 1)
	cmd := zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchRandom, Args: []string{"snes"}, AdvArgs: zapscript.NewAdvArgs(args),
	}
	return platforms.CmdEnv{
		Cfg:      &config.Instance{},
		Cmd:      cmd,
		Playlist: playlists.PlaylistController{Queue: queue},
	}, queue
}

func TestCmdRandom_RotationBuildsARepeatingOneItemPlaylist(t *testing.T) {
	t.Parallel()

	env, queue := randomRotationEnv(map[string]string{
		"advance": "5m", "countdown": "3s", "action": "details", "slot": "primary",
	})
	policy := tokens.NewCommandPolicy(zapscript.ZapScriptCmdLaunchRandom, zapscript.ZapScriptCmdPlaylistPlay)
	env.Unsafe = true
	env.AllowedCommands = policy
	hold := &tokens.Token{UID: "card", ReaderID: "reader"}
	env.Playlist.HoldToken = hold

	result, err := cmdRandom(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	pls := <-queue
	assert.Same(t, pls, result.Playlist)

	require.Len(t, pls.Items, 1)
	assert.Equal(t, "**launch.random:snes?action=details&slot=primary", pls.Items[0].ZapScript,
		"the item is the same command without its rotation, keeping its other arguments")
	assert.True(t, pls.Loop)
	assert.True(t, pls.Playing)
	assert.True(t, pls.ForceRelaunch, "a rescan rolls again")
	assert.Equal(t, mediaslot.Primary, pls.Slot)
	assert.Equal(t, 5*time.Minute, pls.Advance)
	assert.Equal(t, 3*time.Second, pls.Countdown)
	assert.True(t, pls.CountdownSet)
	assert.NotEmpty(t, pls.ID)
	assert.True(t, pls.Unsafe, "trust carries into the playlist")
	assert.Equal(t, policy, pls.AllowedCommands)
	require.NotNil(t, pls.HoldToken)
	assert.Equal(t, hold.UID, pls.HoldToken.UID)

	reparsed, err := zapscript.NewParser(pls.Items[0].ZapScript).ParseScript()
	require.NoError(t, err)
	require.Len(t, reparsed.Cmds, 1)
	assert.Empty(t, reparsed.Cmds[0].AdvArgs.Get(zapscript.KeyAdvance))
	assert.Equal(t, []string{"snes"}, reparsed.Cmds[0].Args)
}

func TestCmdRandom_IdleRotation(t *testing.T) {
	t.Parallel()

	env, queue := randomRotationEnv(map[string]string{"idle": "10m"})
	_, err := cmdRandom(newInputPlatform(platforms.InputAll), env)
	require.NoError(t, err)
	pls := <-queue
	assert.Equal(t, 10*time.Minute, pls.Idle)
	assert.True(t, pls.Playing, "it starts now, like playlist.play")
	assert.False(t, pls.Armed)
	assert.Equal(t, "**launch.random:snes", pls.Items[0].ZapScript)
}

func TestCmdRandom_RotationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pl      platforms.Platform
		wantErr error
		args    map[string]string
		name    string
	}{
		{
			name: "advance with idle", pl: newInputPlatform(platforms.InputAll),
			args: map[string]string{"advance": "5m", "idle": "5m"}, wantErr: ErrRotationConflict,
		},
		{
			name: "background slot", pl: newInputPlatform(platforms.InputAll),
			args: map[string]string{"advance": "5m", "slot": "background"}, wantErr: ErrBackgroundRotation,
		},
		{
			name: "countdown alone", pl: newInputPlatform(platforms.InputAll),
			args: map[string]string{"countdown": "3s"}, wantErr: ErrCountdownWithoutRotation,
		},
		{
			name: "idle without full coverage", pl: newPlaylistTestPlatform(),
			args: map[string]string{"idle": "5m"}, wantErr: autoadvance.ErrIdleUnsupported,
		},
		{
			name: "advance out of range", pl: newInputPlatform(platforms.InputAll),
			args: map[string]string{"advance": "48h"}, wantErr: ErrInvalidRotation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, queue := randomRotationEnv(tt.args)
			_, err := cmdRandom(tt.pl, env)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, queue, "nothing is queued")
		})
	}
}

// The rotation shorthand goes through the real command path, with a real
// launch manager, so its own handling of the launch guard is exercised: it
// queues a playlist rather than launching, and must not be refused by a lock
// the command itself holds.
func TestRunCommand_RandomRotationIsNotRefusedByItsOwnLaunchGuard(t *testing.T) {
	t.Parallel()

	active := testRotationActive()
	tests := []struct {
		active *playlists.Playlist
		name   string
	}{
		{name: "nothing playing"},
		{name: "a playlist already active", active: active},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lm := state.NewLauncherManager()
			queue := make(chan *playlists.Playlist, 1)
			script, err := zapscript.NewParser("**launch.random:snes?advance=12s&countdown=2s").ParseScript()
			require.NoError(t, err)

			result, err := RunCommand(
				t.Context(), newPlaylistTestPlatform(), &config.Instance{},
				playlists.PlaylistController{Active: tt.active, Queue: queue},
				tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, nil,
				&RunCommandOptions{LauncherManager: lm}, &zapscript.ArgExprEnv{},
			)

			require.NoError(t, err)
			assert.True(t, result.PlaylistChanged)
			pls := <-queue
			assert.Equal(t, 12*time.Second, pls.Advance)
			assert.True(t, pls.Playing)
			assert.False(t, lm.Launching(), "the command leaves no launch lock behind")
		})
	}
}

func TestRunCommand_PlainRandomStillTakesTheLaunchGuard(t *testing.T) {
	t.Parallel()

	lm := state.NewLauncherManager()
	require.NoError(t, lm.TryStartLaunch())
	script, err := zapscript.NewParser("**launch.random:snes").ParseScript()
	require.NoError(t, err)

	_, err = RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{}, tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, nil,
		&RunCommandOptions{LauncherManager: lm}, &zapscript.ArgExprEnv{},
	)

	require.ErrorIs(t, err, state.ErrLaunchInProgress)
}

func testRotationActive() *playlists.Playlist {
	return &playlists.Playlist{
		ID: "old", Slot: mediaslot.Primary, Playing: true,
		Items: []playlists.PlaylistItem{{Name: "x", ZapScript: "**echo:x"}},
	}
}

func TestQueuePlaylistUpdate_StampsLaunchingUpdatesWithTheStopCount(t *testing.T) {
	t.Parallel()

	queue := make(chan *playlists.Playlist, 2)
	env := platforms.CmdEnv{
		Playlist: playlists.PlaylistController{Queue: queue},
		StopSeq:  func() uint64 { return 7 },
	}
	require.NoError(t, queuePlaylistUpdate(&env, &playlists.Playlist{Playing: true}))
	got := <-queue
	assert.True(t, got.StopStamped)
	assert.Equal(t, uint64(7), got.StopSeq)

	env.StopSeq = nil
	require.NoError(t, queuePlaylistUpdate(&env, &playlists.Playlist{Playing: true}))
	assert.False(t, (<-queue).StopStamped, "without a source of the count nothing is stamped")
}

func TestCmdRandom_RotationUpdateIsStamped(t *testing.T) {
	t.Parallel()

	env, queue := randomRotationEnv(map[string]string{"advance": "5m"})
	env.StopSeq = func() uint64 { return 3 }
	_, err := cmdRandom(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	got := <-queue
	assert.True(t, got.StopStamped)
	assert.Equal(t, uint64(3), got.StopSeq)
}

func openRotatingDeck(t *testing.T, args map[string]string, pl platforms.Platform) (*playlists.Playlist, error) {
	t.Helper()
	env, db, queue := deckTestEnv(t, "deck://0123456789ab")
	env.Cmd.AdvArgs = zapscript.NewAdvArgs(args)
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Rotating", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**launch.system:NES"}},
	}))
	if _, err := cmdPlaylistLoad(pl, env); err != nil {
		assert.Empty(t, queue)
		return nil, err
	}
	return <-queue, nil
}

func TestDeckPlaylist_RotationComesFromTheCommand(t *testing.T) {
	t.Parallel()

	full := newInputPlatform(platforms.InputAll)
	pls, err := openRotatingDeck(t, nil, full)
	require.NoError(t, err)
	assert.Zero(t, pls.Advance)
	assert.Zero(t, pls.Idle)
	assert.False(t, pls.CountdownSet)

	pls, err = openRotatingDeck(t, map[string]string{"advance": "2m", "countdown": "0"}, full)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, pls.Advance)
	assert.Zero(t, pls.Idle)
	assert.True(t, pls.CountdownSet)
	assert.Zero(t, pls.Countdown)

	pls, err = openRotatingDeck(t, map[string]string{"idle": "5m"}, full)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, pls.Idle)
	assert.Zero(t, pls.Advance)
}

func TestDeckPlaylist_IdleRefusedWithoutFullCoverage(t *testing.T) {
	t.Parallel()

	_, err := openRotatingDeck(t, map[string]string{"idle": "1m"}, newPlaylistTestPlatform())
	require.ErrorIs(t, err, autoadvance.ErrIdleUnsupported)
}

func TestDeckPlaylist_BackgroundSlotRefusesRotation(t *testing.T) {
	t.Parallel()

	_, err := openRotatingDeck(t, map[string]string{"slot": "background", "advance": "1m"},
		newInputPlatform(platforms.InputAll))
	require.ErrorIs(t, err, ErrBackgroundRotation)
}
