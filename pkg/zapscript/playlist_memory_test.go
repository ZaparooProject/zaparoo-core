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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePlaylistMemory remembers one position for every playlist.
type fakePlaylistMemory struct {
	prepared []*playlists.Playlist
	starts   []int
	index    int
}

func (f *fakePlaylistMemory) Prepare(pls *playlists.Playlist, start int) {
	f.prepared = append(f.prepared, pls)
	f.starts = append(f.starts, start)
	if start > 0 {
		pls.Index = start - 1
		return
	}
	pls.Index = f.index
}

func startArgs(start string) zapscript.AdvArgs {
	var aa zapscript.AdvArgs
	return aa.With(zapscript.KeyStart, start)
}

const memoryTestPlaylist = `{"id":"id","name":"name","items":[` +
	`{"name":"Item 1","zapscript":"**test1"},{"name":"Item 2","zapscript":"**test2"},` +
	`{"name":"Item 3","zapscript":"**test3"}]}`

func TestLoadPlaylist_OpensWhereMemoryLeftIt(t *testing.T) {
	t.Parallel()

	memory := &fakePlaylistMemory{index: 2}
	pls, err := loadPlaylist(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx:     t.Context(),
		Cfg:            &config.Instance{},
		PlaylistMemory: memory,
		Cmd:            zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{memoryTestPlaylist}},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, pls.Index)
	assert.Equal(t, []int{0}, memory.starts)
	assert.False(t, pls.Shuffled)
}

func TestLoadPlaylist_StartWithoutMemory(t *testing.T) {
	t.Parallel()

	pls, err := loadPlaylist(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx: t.Context(),
		Cfg:        &config.Instance{},
		Cmd: zapscript.Command{
			Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{memoryTestPlaylist}, AdvArgs: startArgs("2"),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, pls.Index)
}

func TestLoadPlaylist_RejectsBadStart(t *testing.T) {
	t.Parallel()

	for _, start := range []string{"0", "-1", "two", "1.5"} {
		_, err := loadPlaylist(newPlaylistTestPlatform(), platforms.CmdEnv{
			ServiceCtx: t.Context(),
			Cfg:        &config.Instance{},
			Cmd: zapscript.Command{
				Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{memoryTestPlaylist}, AdvArgs: startArgs(start),
			},
		})
		require.Error(t, err, start)
	}
}

func TestLoadPlaylist_MarksShuffledPlaylists(t *testing.T) {
	t.Parallel()

	plsFile := filepath.Join(t.TempDir(), "mix.pls")
	require.NoError(t, os.WriteFile(plsFile, []byte("[playlist]\nFile1=**a\nFile2=**b\n"), 0o600))
	var aa zapscript.AdvArgs
	memory := &fakePlaylistMemory{}
	pls, err := loadPlaylist(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx:     t.Context(),
		Cfg:            &config.Instance{},
		PlaylistMemory: memory,
		Cmd: zapscript.Command{
			Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{plsFile},
			AdvArgs: aa.With(zapscript.KeyMode, zapscript.ModeShuffle),
		},
	})
	require.NoError(t, err)
	assert.True(t, pls.Shuffled)
	require.Len(t, memory.prepared, 1)
	assert.True(t, memory.prepared[0].Shuffled, "memory sees the shuffle so it can restore the order")
}

func TestCmdPlaylistPlay_StartPlaysOpenPlaylistFromThatItem(t *testing.T) {
	t.Parallel()

	active, queue := makePlaylistEnv()
	active.Index = 2
	active.Playing = true

	result, err := cmdPlaylistPlay(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx: t.Context(),
		Cfg:        &config.Instance{},
		Cmd: zapscript.Command{
			Name: zapscript.ZapScriptCmdPlaylistPlay, Args: []string{memoryTestPlaylist}, AdvArgs: startArgs("1"),
		},
		Playlist: playlists.PlaylistController{Active: active, Queue: queue},
	})
	require.NoError(t, err)
	require.True(t, result.PlaylistChanged)
	queued := <-queue
	assert.Equal(t, 0, queued.Index, "start wins over moving on")
	assert.True(t, queued.Playing)
}

func TestCmdPlaylistPlay_WithoutStartStillAdvancesOpenPlaylist(t *testing.T) {
	t.Parallel()

	active, queue := makePlaylistEnv()
	active.Index = 1
	active.Playing = true

	_, err := cmdPlaylistPlay(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx:     t.Context(),
		Cfg:            &config.Instance{},
		PlaylistMemory: &fakePlaylistMemory{index: 1},
		Cmd:            zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistPlay, Args: []string{memoryTestPlaylist}},
		Playlist:       playlists.PlaylistController{Active: active, Queue: queue},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, (<-queue).Index)
}

func TestRunCommand_RandomRotationIsEphemeral(t *testing.T) {
	t.Parallel()

	queue := make(chan *playlists.Playlist, 1)
	script, err := zapscript.NewParser("**launch.random:snes?advance=12s").ParseScript()
	require.NoError(t, err)

	_, err = RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{Queue: queue},
		tokens.Token{Source: tokens.SourceAPI}, script.Cmds[0], 1, 0, nil,
		&RunCommandOptions{LauncherManager: state.NewLauncherManager()}, &zapscript.ArgExprEnv{},
	)
	require.NoError(t, err)
	pls := <-queue
	assert.True(t, pls.Ephemeral)
	assert.Empty(t, playlists.MemoryKey(pls))
}

func TestCmdPlaylistOpen_StartPicksItemOfOpenPlaylist(t *testing.T) {
	t.Parallel()

	active, queue := makePlaylistEnv()
	active.Index = 2
	ui := uievents.New(clockwork.NewFakeClock(), nil, nil)

	result, err := cmdPlaylistOpen(newPlaylistTestPlatform(), platforms.CmdEnv{
		ServiceCtx: t.Context(),
		UI:         ui,
		Cfg:        &config.Instance{},
		Cmd: zapscript.Command{
			Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{memoryTestPlaylist}, AdvArgs: startArgs("2"),
		},
		Playlist: playlists.PlaylistController{Active: active, Queue: queue},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Playlist.Index, "start wins over the open playlist's position")
	uiState := ui.State()
	require.Len(t, uiState.Events, 1)
	assert.Equal(t, 1, selectedChoiceIndex(&uiState.Events[0]))
	require.NoError(t, ui.Respond(uiState.Events[0].ID, apimodels.UIResponseActionDismiss, ""))
}
