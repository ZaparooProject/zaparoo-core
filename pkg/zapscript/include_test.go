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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// includePlatform is a platform whose data directory is a fresh temp dir,
// returned with the scripts folder inside it.
func includePlatform(t *testing.T) (pl *mocks.MockPlatform, scriptsDir string) {
	t.Helper()
	dataDir := t.TempDir()
	scriptsDir = filepath.Join(dataDir, config.ScriptsDir)
	require.NoError(t, os.MkdirAll(scriptsDir, 0o750))
	settings := mocks.BasicMockSettings()
	settings.DataDir = dataDir
	settings.HostManagedPaths = true
	pl = mocks.NewMockPlatform()
	pl.On("Settings").Return(settings)
	return pl, scriptsDir
}

func writeScript(t *testing.T, scripts, name, text string) {
	t.Helper()
	path := filepath.Join(scripts, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

func runInclude(pl platforms.Platform, args ...string) (platforms.CmdResult, error) {
	return cmdInclude(pl, platforms.CmdEnv{
		Cmd: zapscript.Command{Name: zapscript.ZapScriptCmdInclude, Args: args},
	})
}

func TestIncludeScriptName(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"arcade-night":            "arcade-night.txt",
		"arcade-night.txt":        "arcade-night.txt",
		"Arcade Night.TXT":        "Arcade Night.TXT",
		"parties/arcade-night":    "parties/arcade-night.txt",
		"  spaced  ":              "spaced.txt",
		"notes.md":                "notes.md.txt",
		"parties/./arcade":        "parties/arcade.txt",
		"parties/../arcade-night": "arcade-night.txt",
	}
	for arg, want := range valid {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			got, err := includeScriptName(arg)
			require.NoError(t, err)
			assert.Equal(t, want, filepath.ToSlash(filepath.Clean(got)))
		})
	}

	for _, arg := range []string{"../outside", "parties/../../outside", "/etc/passwd", "../"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			_, err := includeScriptName(arg)
			require.ErrorIs(t, err, ErrInvalidArguments)
		})
	}
}

func TestInclude_ReturnsTheFilesCommands(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	writeScript(t, scripts, "parties/night.txt", "**echo:one ||\n\n  **echo:two\n")

	res, err := runInclude(pl, "parties/night")
	require.NoError(t, err)
	require.Len(t, res.NewCommands, 2)
	assert.Equal(t, []string{"one"}, res.NewCommands[0].Args)
	assert.Equal(t, []string{"two"}, res.NewCommands[1].Args)
}

func TestInclude_Arguments(t *testing.T) {
	t.Parallel()
	pl, _ := includePlatform(t)

	_, err := runInclude(pl)
	require.ErrorIs(t, err, ErrRequiredArgs)
	_, err = runInclude(pl, "  ")
	require.ErrorIs(t, err, ErrRequiredArgs)
	_, err = runInclude(pl, "a", "b")
	require.ErrorIs(t, err, ErrArgCount)
}

func TestInclude_RefusesWhatIsNotAScript(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	require.NoError(t, os.Mkdir(filepath.Join(scripts, "folder.txt"), 0o750))
	writeScript(t, scripts, "big.txt", "**echo:"+strings.Repeat("a", MaxScriptLength))
	writeScript(t, scripts, "empty.txt", " \n")
	writeScript(t, filepath.Dir(scripts), "outside.txt", "**echo:outside")

	_, err := runInclude(pl, "missing")
	require.ErrorIs(t, err, ErrFileNotFound)
	_, err = runInclude(pl, "folder")
	require.ErrorIs(t, err, ErrScriptUnreadable)
	_, err = runInclude(pl, "big")
	require.ErrorIs(t, err, ErrScriptUnreadable)
	require.ErrorIs(t, err, ErrScriptTooLong)
	_, err = runInclude(pl, "empty")
	require.ErrorIs(t, err, ErrInvalidScript)
	_, err = runInclude(pl, "../outside")
	require.ErrorIs(t, err, ErrInvalidArguments)
}

func TestInclude_MissingScriptsFolder(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	require.NoError(t, os.Remove(scripts))

	_, err := runInclude(pl, "a")
	require.ErrorIs(t, err, ErrFileNotFound)
}

// A link inside the folder that points outside it is refused, so the folder
// is the only place a card can read from.
func TestInclude_LinkOutOfTheFolderIsRefused(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("**echo:secret"), 0o600))
	if err := os.Symlink(outside, filepath.Join(scripts, "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(scripts, "dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := runInclude(pl, "escape")
	require.ErrorIs(t, err, ErrScriptUnreadable)
	_, err = runInclude(pl, "dir/secret")
	require.ErrorIs(t, err, ErrScriptUnreadable)
}

func TestInclude_LinkInsideTheFolderIsFollowed(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	writeScript(t, scripts, "real.txt", "**echo:real")
	if err := os.Symlink("real.txt", filepath.Join(scripts, "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res, err := runInclude(pl, "alias")
	require.NoError(t, err)
	require.Len(t, res.NewCommands, 1)
}

// An error names the file and says what is wrong without quoting it.
func TestInclude_ParseErrorDoesNotQuoteTheFile(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	const secret = "hunter2-secret-value"
	writeScript(t, scripts, "broken.txt", "**launch:[["+secret)

	_, err := runInclude(pl, "broken")
	require.ErrorIs(t, err, ErrInvalidScript)
	assert.Contains(t, err.Error(), "broken.txt")
	assert.NotContains(t, err.Error(), secret)
}

func TestInclude_ChargesTheRunsBudget(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	writeScript(t, scripts, "a.txt", "**echo:a||**echo:b")

	var gotCmds, gotSize int
	_, err := cmdInclude(pl, platforms.CmdEnv{
		Cmd: zapscript.Command{Name: zapscript.ZapScriptCmdInclude, Args: []string{"a"}},
		SpendExpansion: func(cmds, size int) error {
			gotCmds, gotSize = cmds, size
			return ErrExpansionLimit
		},
	})
	require.ErrorIs(t, err, ErrExpansionLimit)
	assert.Equal(t, 2, gotCmds)
	assert.Equal(t, len("**echo:a||**echo:b"), gotSize)
}

func TestExpansionBudget(t *testing.T) {
	t.Parallel()

	t.Run("expansions", func(t *testing.T) {
		t.Parallel()
		var b ExpansionBudget
		for range MaxScriptExpansions {
			require.NoError(t, b.Spend(1, 1))
		}
		require.ErrorIs(t, b.Spend(1, 1), ErrExpansionLimit)
	})

	t.Run("commands", func(t *testing.T) {
		t.Parallel()
		var b ExpansionBudget
		require.NoError(t, b.Spend(MaxExpandedCommands, 1))
		require.ErrorIs(t, b.Spend(1, 1), ErrExpansionLimit)
	})

	t.Run("bytes", func(t *testing.T) {
		t.Parallel()
		var b ExpansionBudget
		require.NoError(t, b.Spend(1, MaxExpandedBytes))
		require.ErrorIs(t, b.Spend(1, 1), ErrExpansionLimit)
	})

	t.Run("a refused spend charges nothing", func(t *testing.T) {
		t.Parallel()
		var b ExpansionBudget
		require.NoError(t, b.Spend(MaxExpandedCommands-1, 1))
		require.ErrorIs(t, b.Spend(2, 1), ErrExpansionLimit)
		require.NoError(t, b.Spend(1, 1))
	})
}

func TestRunControlScript_RefusesInclude(t *testing.T) {
	t.Parallel()
	err := RunControlScript(
		t.Context(), mocks.NewMockPlatform(), &config.Instance{}, nil, "**include:a", &zapscript.ArgExprEnv{},
	)
	require.ErrorIs(t, err, ErrControlCommandNotAllowed)
}

// A ZapLink whose body links back to itself is cut off by the budget the run
// shares, the way a self-including file is. This drives RunCommand the way
// the script runner does, feeding each result's commands back in.
func TestRunCommand_SelfLinkingZapLinkStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	var fetches int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.Header().Set("Content-Type", MIMEZaparooZapScript)
		_, _ = w.Write([]byte("**echo:again||**launch:" + server.URL + "/loop"))
	}))
	defer server.Close()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetZapLinkHost", server.URL).Return(true, true, nil)
	mockUserDB.On("UpdateZapLinkCache", mock.Anything, mock.Anything).Return(nil)
	db := &database.Database{UserDB: mockUserDB}
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")

	opts := &RunCommandOptions{Expansions: &ExpansionBudget{}}
	queue := []zapscript.Command{{Name: zapscript.ZapScriptCmdLaunch, Args: []string{server.URL + "/loop"}}}
	var err error
	for len(queue) > 0 && err == nil {
		cmd := queue[0]
		queue = queue[1:]
		var res platforms.CmdResult
		res, err = RunCommand(
			t.Context(), pl, &config.Instance{}, playlists.PlaylistController{},
			tokens.Token{Source: tokens.SourceReader}, cmd, 1, 0, db, opts, &zapscript.ArgExprEnv{},
		)
		queue = append(res.NewCommands, queue...)
		require.LessOrEqual(t, fetches, MaxScriptExpansions+1, "the loop was not stopped")
	}
	require.ErrorIs(t, err, ErrExpansionLimit)
	assert.Equal(t, MaxScriptExpansions+1, fetches)
}

func commandNames(cmds []zapscript.Command) []string {
	names := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		name := cmd.Name
		if len(cmd.Args) > 0 {
			name += ":" + cmd.Args[0]
		}
		names = append(names, name)
	}
	return names
}

func parseCommands(t *testing.T, text string) []zapscript.Command {
	t.Helper()
	script, err := zapscript.NewParser(text).ParseScript()
	require.NoError(t, err)
	return script.Cmds
}

// Checks made before a run see each included file's commands where the
// include stands, nested includes too.
func TestExpandIncludes_ReplacesIncludesInPlace(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	writeScript(t, scripts, "outer.txt", "**echo:o1||**include:inner||**echo:o2")
	writeScript(t, scripts, "inner.txt", "**launch:game.rom")

	got := ExpandIncludes(pl, parseCommands(t, "**echo:a||**include:outer||**echo:b"))
	assert.Equal(t, []string{"echo:a", "echo:o1", "launch:game.rom", "echo:o2", "echo:b"}, commandNames(got))
}

// An include that cannot be loaded stays as it is, so the run reports it.
func TestExpandIncludes_KeepsIncludesItCannotLoad(t *testing.T) {
	t.Parallel()
	pl, _ := includePlatform(t)
	cmds := parseCommands(t, "**include:missing||**include:../escape||**include:[[platform]]")

	assert.Equal(t, commandNames(cmds), commandNames(ExpandIncludes(pl, cmds)))
}

// A file that includes itself expands only as far as a run would, then the
// include that would go past the limit is kept.
func TestExpandIncludes_StopsAtTheExpansionLimit(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	writeScript(t, scripts, "loop.txt", "**echo:x||**include:loop")

	got := ExpandIncludes(pl, parseCommands(t, "**include:loop"))
	require.Len(t, got, MaxScriptExpansions+1)
	assert.Equal(t, zapscript.ZapScriptCmdInclude, got[len(got)-1].Name)
}

// A script with no include is returned as it is, without touching the disk.
func TestExpandIncludes_NoIncludeReturnsTheSameCommands(t *testing.T) {
	t.Parallel()
	cmds := parseCommands(t, "**echo:a||**echo:b")
	got := ExpandIncludes(nil, cmds)
	assert.Equal(t, cmds, got)
}
