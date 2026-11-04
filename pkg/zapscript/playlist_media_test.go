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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pickerMarioPath = "/games/SNES/Super Mario World (USA).sfc"
	pickerZeldaPath = "/games/SNES/Legend of Zelda, The - A Link to the Past (USA).sfc"
)

func pickerMediaDB(t *testing.T) *database.Database {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, mediaDB, "SNES", pickerMarioPath, pickerZeldaPath)
	return &database.Database{MediaDB: mediaDB}
}

func TestPlaylistChoiceMedia(t *testing.T) {
	t.Parallel()

	db := pickerMediaDB(t)
	env := &platforms.CmdEnv{ServiceCtx: t.Context(), Database: db, Cfg: &config.Instance{}}
	launch := zapscript.Command{
		Name:    zapscript.ZapScriptCmdLaunch,
		Args:    []string{pickerMarioPath},
		AdvArgs: zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySystem): "SNES"}),
	}.String()
	items := []playlists.PlaylistItem{
		{Name: "Mario", ZapScript: launch},
		{Name: "Zelda", ZapScript: "@SNES/Legend of Zelda, The - A Link to the Past"},
		{Name: "Missing file", ZapScript: zapscript.Command{
			Name:    zapscript.ZapScriptCmdLaunch,
			Args:    []string{"/games/SNES/Nothing.sfc"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySystem): "SNES"}),
		}.String()},
		{Name: "Path without a system", ZapScript: pickerMarioPath},
		{Name: "Unknown title", ZapScript: "@SNES/Completely Unknown Game Title"},
		{Name: "Not a game", ZapScript: "**input.keyboard:a"},
	}

	media := playlistChoiceMedia(env, items)
	require.Len(t, media, len(items))
	require.NotNil(t, media[0])
	assert.Equal(t, "SNES", media[0].System)
	assert.Equal(t, pickerMarioPath, media[0].Path)
	assert.NotEmpty(t, media[0].Name)
	require.NotNil(t, media[1], "a title launch names the file it resolves to")
	assert.Equal(t, "SNES", media[1].System)
	assert.Equal(t, pickerZeldaPath, media[1].Path)
	for i := 2; i < len(items); i++ {
		assert.Nil(t, media[i], items[i].Name)
	}
}

func TestPlaylistChoiceMediaWithoutMediaDB(t *testing.T) {
	t.Parallel()

	media := playlistChoiceMedia(&platforms.CmdEnv{}, []playlists.PlaylistItem{{ZapScript: "@SNES/Mario"}})
	assert.Equal(t, []*apimodels.UIChoiceMedia{nil}, media)
}

func TestCmdPlaylistOpen_PickerChoicesCarryMedia(t *testing.T) {
	t.Parallel()

	db := pickerMediaDB(t)
	launch := zapscript.Command{
		Name:    zapscript.ZapScriptCmdLaunch,
		Args:    []string{pickerMarioPath},
		AdvArgs: zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySystem): "SNES"}),
	}.String()
	active := &playlists.Playlist{
		ID:   "picker-media",
		Name: "Picker",
		Items: []playlists.PlaylistItem{
			{Name: "Mario", ZapScript: launch},
			{Name: "Action", ZapScript: "**input.keyboard:a"},
		},
	}
	ui := uievents.New(clockwork.NewFakeClock(), nil, nil)
	env := platforms.CmdEnv{
		ServiceCtx: t.Context(),
		UI:         ui,
		Database:   db,
		Cmd:        zapscript.Command{Name: "playlist.open"},
		Cfg:        &config.Instance{},
		Playlist: playlists.PlaylistController{
			Active: active,
			Queue:  make(chan *playlists.Playlist, 1),
		},
	}

	_, err := cmdPlaylistOpen(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	state := ui.State()
	require.Len(t, state.Events, 1)
	choices := state.Events[0].Choices
	require.Len(t, choices, 2)
	require.NotNil(t, choices[0].Media)
	assert.Equal(t, pickerMarioPath, choices[0].Media.Path)
	assert.Nil(t, choices[1].Media)
	// The playing item is flagged for the host widget, never written into
	// the label clients show.
	assert.Equal(t, "Mario", choices[0].Label)
	assert.True(t, choices[0].Current)
	assert.False(t, choices[1].Current)
	require.NoError(t, ui.Respond(state.Events[0].ID, apimodels.UIResponseActionDismiss, ""))
}
