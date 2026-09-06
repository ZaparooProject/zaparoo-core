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

package gamelistxml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The scrape systems carry the extensions each system's launchers index, which
// is what stops a shared ROM folder handing one system's gamelist entries to
// another. A system whose launchers accept files through a Test function has no
// stateable set and must be left unknown rather than partially listed.
func TestResolveSystemsFromPlatformCarriesLauncherExtensions(t *testing.T) {
	t.Parallel()

	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	root := t.TempDir()
	gbDir := filepath.Join(root, "GAMEBOY")
	gbcDir := filepath.Join(root, "GBC")
	atariDir := filepath.Join(root, "ATARI7800")
	for _, dir := range []string{gbDir, gbcDir, atariDir} {
		require.NoError(t, os.MkdirAll(dir, 0o750))
	}
	scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboy, filepath.Join(gbDir, "Game.gb"))
	scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboyColor, filepath.Join(gbDir, "Game.gbc"))
	scantest.IndexMediaPaths(t, db, systemdefs.SystemAtari2600, filepath.Join(atariDir, "Game.a26"))

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	pl := &mocks.MockPlatform{}
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{root})
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{
		{
			ID: "Gameboy", SystemID: systemdefs.SystemGameboy,
			Folders: []string{"GAMEBOY"}, Extensions: []string{".gb", ".mgl"},
		},
		{ID: "RAGameboy", SystemID: systemdefs.SystemGameboy},
		{
			ID: "GameboyColor", SystemID: systemdefs.SystemGameboyColor,
			Folders: []string{"GAMEBOY", "GBC"}, Extensions: []string{".gbc", ".mgl"},
		},
		{
			ID: "Atari2600", SystemID: systemdefs.SystemAtari2600,
			Folders: []string{"ATARI7800"}, Extensions: []string{".a26"},
			Test: func(*config.Instance, string) bool { return false },
		},
	})

	systems, err := resolveSystemsFromPlatform(context.Background(), cfg, pl, db, nil)
	require.NoError(t, err)

	byID := make(map[string]struct {
		roots []string
		exts  []string
	}, len(systems))
	for _, system := range systems {
		byID[system.ID] = struct {
			roots []string
			exts  []string
		}{system.ROMPaths, system.Extensions}
	}

	gb, ok := byID[systemdefs.SystemGameboy]
	require.True(t, ok)
	assert.Equal(t, []string{gbDir}, gb.roots)
	assert.Equal(t, []string{".gb", ".mgl"}, gb.exts)

	gbc, ok := byID[systemdefs.SystemGameboyColor]
	require.True(t, ok)
	assert.Equal(t, []string{gbDir, gbcDir}, gbc.roots)
	assert.Equal(t, []string{".gbc", ".mgl"}, gbc.exts)

	atari, ok := byID[systemdefs.SystemAtari2600]
	require.True(t, ok)
	assert.Empty(t, atari.exts, "a Test function leaves the set unknown, not partial")
}
