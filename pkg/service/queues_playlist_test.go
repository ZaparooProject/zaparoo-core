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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type playlistTestEnv struct {
	platform *mocks.MockPlatform
	cfg      *config.Instance
	st       *state.State
	db       *database.Database
	lsq      chan *tokens.Token
}

func setupPlaylistTestEnv(t *testing.T) *playlistTestEnv {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("ReturnToMenu").Return(nil).Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	cfg := &config.Instance{}

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

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	return &playlistTestEnv{
		platform: mockPlatform,
		cfg:      cfg,
		st:       st,
		db:       &database.Database{UserDB: mockUserDB},
		lsq:      make(chan *tokens.Token, 10),
	}
}

func TestRunTokenZapScript_ClearsPlaylistOnMediaChange(t *testing.T) {
	t.Parallel()

	env := setupPlaylistTestEnv(t)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**launch.system:menu",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(env.platform, env.cfg, env.st, token, env.db, env.lsq, plsc, nil)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		assert.Nil(t, pls, "playlist queue should receive nil to clear the active playlist")
	case <-time.After(time.Second):
		t.Fatal("expected nil on playlist queue but nothing was sent")
	}
}

func TestRunTokenZapScript_SkipsPlaylistClearForPlaylistSource(t *testing.T) {
	t.Parallel()

	env := setupPlaylistTestEnv(t)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**launch.system:menu",
		ScanTime: time.Now(),
		Source:   tokens.SourcePlaylist,
	}

	err := runTokenZapScript(env.platform, env.cfg, env.st, token, env.db, env.lsq, plsc, nil)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		t.Fatalf("playlist queue should NOT receive anything for playlist-sourced tokens, got: %v", pls)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing sent
	}
}

func TestRunTokenZapScript_NoPlaylistClearForNonMediaCommand(t *testing.T) {
	t.Parallel()

	env := setupPlaylistTestEnv(t)
	env.platform.On("KeyboardPress", "{f2}").Return(nil)

	plq := make(chan *playlists.Playlist, 10)
	plsc := playlists.PlaylistController{Queue: plq}

	token := tokens.Token{
		Text:     "**input.keyboard:{f2}",
		ScanTime: time.Now(),
	}

	err := runTokenZapScript(env.platform, env.cfg, env.st, token, env.db, env.lsq, plsc, nil)
	require.NoError(t, err)

	select {
	case pls := <-plq:
		t.Fatalf("playlist queue should NOT receive anything for non-media commands, got: %v", pls)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing sent
	}
}
