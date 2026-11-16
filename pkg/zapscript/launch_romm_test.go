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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A RomM game is downloaded into its system's game folder and launched from
// there by that system's launcher, even when the token named the RomM
// placeholder launcher.
func TestLaunchClosureInstallsRomMGame(t *testing.T) {
	t.Parallel()

	var downloads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/roms/5":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(romm.Rom{
				ID: 5, PlatformSlug: "snes", Name: "Super Game",
				FSName: "Super Game (USA).sfc", FSSizeBytes: 4,
				Files: []romm.RomFile{{FileName: "Super Game (USA).sfc", FileSizeBytes: 4}},
			})
		case "/api/roms/5/content/Super Game (USA).sfc":
			downloads++
			_, _ = w.Write([]byte("rom!"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Instance{}
	cfg.SetLauncherServerURL(romm.LauncherID, server.URL)
	root := t.TempDir()
	want := filepath.Join(root, "SNES", "Super Game (USA).sfc")
	snes := platforms.Launcher{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES"}, Extensions: []string{".sfc"}}

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()}).Maybe()
	pl.On("RootDirs", cfg).Return([]string{root}).Maybe()
	pl.On("Launchers", cfg).Return([]platforms.Launcher{snes})
	pl.On("LaunchMedia", cfg, want,
		mock.MatchedBy(func(l *platforms.Launcher) bool { return l != nil && l.ID == snes.ID }),
		(*database.Database)(nil), mock.Anything).Return(nil).Twice()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "SNES"), 0o750))

	for range 2 {
		env := platforms.CmdEnv{
			Cfg: cfg,
			Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(map[string]string{
				string(zapscript.KeyLauncher): romm.LauncherID,
			})},
		}
		launch := getLaunchClosure(pl, &env, true)
		require.NoError(t, launch(launchTarget{path: romm.RomPath(5, "Super Game")}))
	}

	data, err := os.ReadFile(want) //nolint:gosec // test temp path
	require.NoError(t, err)
	assert.Equal(t, "rom!", string(data))
	assert.Equal(t, 1, downloads, "a downloaded game launches from disk")
	pl.AssertExpectations(t)
}

func TestLaunchClosureRomMUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	pl := mocks.NewMockPlatform()
	env := platforms.CmdEnv{Cfg: cfg, Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(nil)}}
	launch := getLaunchClosure(pl, &env, false)
	require.Error(t, launch(launchTarget{path: romm.RomPath(5, "Super Game")}))
	pl.AssertNotCalled(t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
