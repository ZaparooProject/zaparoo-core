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

package playnite

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// IndexedSystem returns the system a game is indexed under, or false when
// Core leaves the game out: hidden and uninstalled entries cannot be played,
// a game on an unknown platform has no system, and a Steam game is already
// indexed by the Steam launcher when skipSteam is set.
func IndexedSystem(game *Game, skipSteam bool) (string, bool) {
	if game.Hidden || !game.IsInstalled {
		return "", false
	}
	return trackedSystem(game, skipSteam)
}

// trackedSystem is the part of IndexedSystem that also applies to a game
// that is already running, where hidden and installed no longer matter.
func trackedSystem(game *Game, skipSteam bool) (string, bool) {
	if skipSteam && strings.EqualFold(strings.TrimSpace(game.LibraryPluginID), SteamLibraryPluginID) {
		return "", false
	}
	if _, err := NormalizeGameID(game.ID); err != nil {
		return "", false
	}
	name := strings.TrimSpace(game.Name)
	if name == "" || virtualpath.ContainsControlChar(name) {
		return "", false
	}
	return SystemForGame(game)
}

// ScanResults converts the games that belong to systemID into indexable
// media. Every game becomes a playnite:// virtual path keyed by its ID.
func ScanResults(games []Game, systemID string, skipSteam bool) []platforms.ScanResult {
	results := make([]platforms.ScanResult, 0)
	for i := range games {
		game := &games[i]
		gameSystem, ok := IndexedSystem(game, skipSteam)
		if !ok || gameSystem != systemID {
			continue
		}
		id, err := NormalizeGameID(game.ID)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(game.Name)
		results = append(results, platforms.ScanResult{
			Path: GamePath(id, name), Name: name, Source: romSource(game.RomPath), NoExt: true,
		})
	}
	return results
}

// romSource points local scrapers at the game's ROM file, when it has one.
func romSource(romPath string) *platforms.MediaSource {
	path := filepath.Clean(strings.Trim(strings.TrimSpace(romPath), `"`))
	if path == "." || !filepath.IsAbs(path) || virtualpath.ContainsControlChar(path) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	return &platforms.MediaSource{Path: path, Root: filepath.Dir(path), Kind: platforms.MediaSourceFile}
}
