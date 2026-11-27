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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/runfailure"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// includeEnv runs scripts through the real runner with a scripts folder the
// test fills in.
type includeEnv struct {
	svc     *ServiceContext
	scripts string
}

func setupIncludeEnv(t *testing.T) *includeEnv {
	t.Helper()

	dataDir := t.TempDir()
	scripts := filepath.Join(dataDir, config.ScriptsDir)
	require.NoError(t, os.MkdirAll(scripts, 0o750))

	settings := mocks.BasicMockSettings()
	settings.DataDir = dataDir
	settings.HostManagedPaths = true
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(settings)
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	st.SetRunZapScript(true)
	t.Cleanup(st.StopService)

	userDB := helpers.NewMockUserDBI()
	userDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	userDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	return &includeEnv{
		svc: &ServiceContext{
			Platform:            mockPlatform,
			Config:              &config.Instance{},
			State:               st,
			DB:                  &database.Database{UserDB: userDB},
			LaunchSoftwareQueue: make(chan softwareTokenUpdate, 10),
			PlaylistQueue:       make(chan *playlists.Playlist, 10),
			BackgroundWG:        &sync.WaitGroup{},
		},
		scripts: scripts,
	}
}

func (env *includeEnv) write(t *testing.T, name, text string) {
	t.Helper()
	path := filepath.Join(env.scripts, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

func (env *includeEnv) run(t *testing.T, token *tokens.Token) error {
	t.Helper()
	if token.Source == "" {
		token.Source = tokens.SourceReader
	}
	token.ScanTime = time.Now()
	return runTokenZapScript(env.svc, *token, playlists.PlaylistController{Queue: env.svc.PlaylistQueue}, nil, false)
}

// failedCommand returns the name of the command a run stopped on.
func failedCommand(t *testing.T, err error) string {
	t.Helper()
	cmdErr, ok := errors.AsType[*runfailure.CommandError](err)
	require.True(t, ok, "run did not stop on a command: %v", err)
	return cmdErr.Name
}

// The file's commands run where the include stands, before the rest of the
// card. Each probe is an unknown command, so the one the run stops on shows
// how far it got.
func TestInclude_RunsAtItsPositionThenContinues(t *testing.T) {
	t.Parallel()
	env := setupIncludeEnv(t)
	env.write(t, "a.txt", "**echo:from file ||\n**probefile:x\n")

	err := env.run(t, &tokens.Token{Text: "**include:a||**probecard:x"})
	require.ErrorIs(t, err, zapscript.ErrUnknownCommand)
	assert.Equal(t, "probefile", failedCommand(t, err))

	env.write(t, "b.txt", "**echo:from file\n")
	err = env.run(t, &tokens.Token{Text: "**include:b.txt||**probecard:x"})
	assert.Equal(t, "probecard", failedCommand(t, err), "the card carries on after the file")
}

func TestInclude_NestedFilesInSubfolders(t *testing.T) {
	t.Parallel()
	env := setupIncludeEnv(t)
	env.write(t, "a.txt", "**include:parties/b||**probea:x")
	env.write(t, "parties/b.txt", "**probeb:x")

	err := env.run(t, &tokens.Token{Text: "**include:a"})
	assert.Equal(t, "probeb", failedCommand(t, err))
}

func TestInclude_SelfIncludeStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	env := setupIncludeEnv(t)
	env.write(t, "loop.txt", "**echo:again||**include:loop")

	err := env.run(t, &tokens.Token{Text: "**include:loop"})
	require.ErrorIs(t, err, zapscript.ErrExpansionLimit)
	assert.Equal(t, "include", failedCommand(t, err))
}

func TestInclude_EachScanGetsItsOwnBudget(t *testing.T) {
	t.Parallel()
	env := setupIncludeEnv(t)
	env.write(t, "a.txt", "**echo:a")

	card := strings.Repeat("**include:a||", zapscript.MaxScriptExpansions-1) + "**include:a"
	for range 2 {
		require.NoError(t, env.run(t, &tokens.Token{Text: card}))
	}
	err := env.run(t, &tokens.Token{Text: card + "||**include:a"})
	require.ErrorIs(t, err, zapscript.ErrExpansionLimit)
}

// Commands from a file answer to the same checks as the card's own: the
// blocked list, the bound the token carries, and its trust.
func TestInclude_FileCommandsInheritTheTokensLimits(t *testing.T) {
	t.Parallel()

	t.Run("blocked command", func(t *testing.T) {
		t.Parallel()
		env := setupIncludeEnv(t)
		require.NoError(t, env.svc.Config.LoadTOML("[zapscript]\nblock_commands = ['echo']"))
		env.write(t, "a.txt", "**echo:blocked")

		err := env.run(t, &tokens.Token{Text: "**include:a"})
		require.ErrorIs(t, err, zapscript.ErrCommandBlocked)
	})

	t.Run("token bound", func(t *testing.T) {
		t.Parallel()
		env := setupIncludeEnv(t)
		env.write(t, "a.txt", "**echo:outside the bound")

		err := env.run(t, &tokens.Token{
			Text:            "**include:a",
			AllowedCommands: tokens.NewCommandPolicy(gozapscript.ZapScriptCmdInclude),
		})
		require.ErrorIs(t, err, zapscript.ErrCommandNotPermitted)
		assert.Equal(t, "echo", failedCommand(t, err))
	})

	t.Run("untrusted token", func(t *testing.T) {
		t.Parallel()
		env := setupIncludeEnv(t)
		env.write(t, "a.txt", "**input.keyboard:a")

		err := env.run(t, &tokens.Token{Text: "**include:a", Unsafe: true})
		require.ErrorIs(t, err, zapscript.ErrRemoteSource)
	})
}
