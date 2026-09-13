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

package steam

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/vdfbinary"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestSteamAppMetadataSource(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	library := filepath.Join(string(filepath.Separator), "library")
	game := filepath.Join(library, "steamapps", "common", "Game")
	require.NoError(t, fs.MkdirAll(game, 0o750))
	source := steamAppMetadataSource(fs, library, map[string]any{"installdir": "Game"})
	require.NotNil(t, source)
	require.Equal(t, game, source.Path)
	require.Equal(t, filepath.Join(library, "steamapps", "common"), source.Root)

	require.Nil(t, steamAppMetadataSource(fs, library, map[string]any{"installdir": "../outside"}))
}

func TestSteamShortcutMetadataSource(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	startDir := filepath.Join(string(filepath.Separator), "games", "Shortcut")
	require.NoError(t, fs.MkdirAll(startDir, 0o750))
	source := steamShortcutMetadataSource(fs, &vdfbinary.Shortcut{StartDir: startDir})
	require.NotNil(t, source)
	require.Equal(t, startDir, source.Path)
}
