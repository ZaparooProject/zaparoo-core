//go:build linux

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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformSteamAppsDirsWithoutHomeYieldsNothing(t *testing.T) {
	t.Parallel()

	// Every Linux candidate hangs off the home directory. With no home the
	// alternative would be bare relative paths resolved against the working
	// directory, which is never where Steam lives.
	assert.Nil(t, platformSteamAppsDirs(""))
}

func TestPlatformSteamAppsDirsCoversLinuxInstallKinds(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "home", "player")
	dirs := platformSteamAppsDirs(home)
	require.NotEmpty(t, dirs)

	for _, dir := range dirs {
		assert.True(t, filepath.IsAbs(dir), "candidate must be absolute: %s", dir)
		assert.True(t, strings.HasPrefix(dir, home+string(filepath.Separator)),
			"candidate must live under the home directory: %s", dir)
		assert.Equal(t, "steamapps", filepath.Base(dir))
	}

	joined := strings.Join(dirs, "\n")
	assert.Contains(t, joined, filepath.Join(home, ".steam", "steam", "steamapps"),
		"native install")
	assert.Contains(t, joined, filepath.Join(home, ".var", "app", FlatpakSteamID),
		"Flatpak install")
	assert.Contains(t, joined, filepath.Join(home, "snap", "steam"),
		"Snap install")
}
