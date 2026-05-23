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
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupNextActionTestEnv(t *testing.T) (*ServiceContext, *mocks.MockPlatform, *config.Instance) {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform").Maybe()
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

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan *tokens.Token, 10),
		PlaylistQueue:       make(chan *playlists.Playlist, 10),
	}
	return svc, mockPlatform, cfg
}

func TestHandleNextActionPreflight_ArmsLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	parser := gozapscript.NewParser("**launch?launcher=3do-dualram")
	script, err := parser.ParseScript()
	require.NoError(t, err)
	token := tokens.Token{UID: "source", Text: "**launch?launcher=3do-dualram", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionArmed, result)
	pending := svc.State.GetPendingLaunchOverride()
	require.NotNil(t, pending)
	assert.Equal(t, "3do-dualram", pending.LauncherID)
	assert.Equal(t, "source", pending.Source.UID)
}

func TestRunTokenZapScript_AppliesPendingLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	launcher := platforms.Launcher{ID: "3do-dualram", SystemID: "3do"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{launcher})

	path := filepath.Join(t.TempDir(), "game.chd")
	mockPlatform.On("LaunchMedia", cfg, path,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "3do-dualram"
		}),
		svc.DB,
		(*platforms.LaunchOptions)(nil)).Return(nil).Once()

	svc.State.SetPendingLaunchOverride(&state.PendingLaunchOverride{
		LauncherID: "3do-dualram",
		Source:     tokens.Token{UID: "source"},
		CreatedAt:  time.Now(),
	})

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	token := tokens.Token{Text: "**launch:" + path, ScanTime: time.Now(), Source: tokens.SourceReader}
	err := runTokenZapScript(svc, token, plsc, nil, false)

	require.NoError(t, err)
	assert.Nil(t, svc.State.GetPendingLaunchOverride())
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_PlaylistDoesNotConsumePendingOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	path := filepath.Join(t.TempDir(), "playlist-game.chd")
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("LaunchMedia", cfg, path,
		(*platforms.Launcher)(nil),
		svc.DB,
		(*platforms.LaunchOptions)(nil)).Return(nil).Once()
	svc.State.SetPendingLaunchOverride(&state.PendingLaunchOverride{
		LauncherID: "3do-dualram",
		Source:     tokens.Token{UID: "source"},
		CreatedAt:  time.Now(),
	})

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	token := tokens.Token{Text: "**launch:" + path, ScanTime: time.Now(), Source: tokens.SourcePlaylist}
	err := runTokenZapScript(svc, token, plsc, nil, false)

	require.NoError(t, err)
	assert.NotNil(t, svc.State.GetPendingLaunchOverride())
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_LaunchSystemDoesNotConsumePendingOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, _ := setupNextActionTestEnv(t)
	mockPlatform.On("LaunchSystem", mock.AnythingOfType("*config.Instance"), "menu").Return(nil).Maybe()
	mockPlatform.On("ReturnToMenu").Return(nil).Once()
	svc.State.SetPendingLaunchOverride(&state.PendingLaunchOverride{
		LauncherID: "3do-dualram",
		Source:     tokens.Token{UID: "source"},
		CreatedAt:  time.Now(),
	})

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	err := runTokenZapScript(svc, tokens.Token{Text: "**launch.system:menu", ScanTime: time.Now()}, plsc, nil, false)

	require.NoError(t, err)
	assert.NotNil(t, svc.State.GetPendingLaunchOverride())
}

func TestHandlePendingWrite_WritesTargetAndConsumesScan(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	reader := mocks.NewMockReader()
	reader.SetupBasicMock()
	svc.State.SetReader(reader)
	readerID := "mock-reader-0123456789abcdef"
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: readerID}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: readerID}
	written := &tokens.Token{UID: "target", Text: "payload", ScanTime: time.Now(), ReaderID: readerID}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})
	reader.On("WriteTarget", mock.Anything, "payload", readers.WriteOptions{
		TargetUID:  "target",
		ExcludeUID: "source",
	}).Return(written, nil).Once()

	consumed := handlePendingWrite(svc, target)

	require.True(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
	assert.Equal(t, written, svc.State.GetWroteToken())
	reader.AssertCalled(t, "WriteTarget", mock.Anything, "payload", readers.WriteOptions{
		TargetUID:  "target",
		ExcludeUID: "source",
	})
}

func TestHandlePendingWrite_ExpiresStaleWrite(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: "reader"}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: "reader"}
	svc.State.SetPendingWrite(&state.PendingWrite{
		Payload:   "payload",
		Source:    source,
		CreatedAt: time.Now().Add(-pendingWriteTTL - time.Second),
	})

	consumed := handlePendingWrite(svc, target)

	require.False(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
}

func TestHandlePendingWrite_IgnoresSourceToken(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: "reader"}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})

	consumed := handlePendingWrite(svc, &source)

	require.True(t, consumed)
	assert.NotNil(t, svc.State.GetPendingWrite())
}
