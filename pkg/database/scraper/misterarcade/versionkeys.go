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

package misterarcade

import (
	"strings"

	"github.com/ZaparooProject/go-slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
)

// alternativesDir is the folder MiSTer arcade distributions file a game's
// other versions under, one subfolder per game.
const alternativesDir = "_alternatives"

// alternativesFolder returns the game folder a descriptor is filed in when it
// sits under an alternatives tree: "Green Beret" for
// "_Arcade/_alternatives/_Green Beret/Rush'n Attack (US).mra".
func alternativesFolder(path string) (folder string, ok bool) {
	segments := strings.Split(strings.ReplaceAll(path, `\`, "/"), "/")
	// The last segment is the file itself, so the game folder must come
	// before it.
	for i := 0; i+2 < len(segments); i++ {
		if !strings.EqualFold(segments[i], alternativesDir) {
			continue
		}
		folder = strings.TrimSpace(strings.TrimPrefix(segments[i+1], "_"))
		return folder, folder != ""
	}
	return "", false
}

// versionKeys lists what a descriptor says about which game it is a version
// of.
//
// A game's alternates are named after neither its main descriptor nor each
// other, so no single source links them all. An alternate points at its game
// by the folder it is filed under and by its parent set. A main descriptor
// answers to its own set and its own title and to nothing else, which is how
// "1942 (Revision B)" meets the folder "_1942".
//
// Three things that look like links are left out because on real libraries
// they join different games. A main descriptor's parent set names the
// hardware it runs on, which would file Balloon Bomber under Space Invaders.
// The catalog's parent title names a series, which would make Street Fighter
// II and its Champion Edition one game. And an alternate that is another
// game's, such as Satan's Hollow on Tron hardware filed under Tron, belongs
// to that game and not to the folder, through neither its folder nor the
// parent set it shares with the folder's game.
func versionKeys(row *descriptorRow, identity mra.Identity) []database.VersionKey {
	var keys []database.VersionKey
	add := func(keyType, key string) {
		if key != "" {
			keys = append(keys, database.VersionKey{Type: keyType, Key: key})
		}
	}
	addSet := func(set string) {
		if set != "" && mra.SetStem(set) == set {
			add(database.VersionKeyMAMESet, set)
		}
	}
	addSet(identity.SetName)
	folder, alternate := alternativesFolder(row.path)
	if !alternate {
		add(database.VersionKeyAltFolder, row.titleSlug)
		return keys
	}
	// Another game's alternate meets its own game through the title it
	// shares with that game's main. Its parent set names the folder's
	// hardware, as a main's does, so it is left out too.
	if row.otherGame {
		return keys
	}
	if identity.Parent != identity.SetName {
		addSet(identity.Parent)
	}
	add(database.VersionKeyAltFolder, slugs.Slugify(slugs.MediaTypeGame, folder))
	return keys
}

// mainIndex is what a system's main descriptors say about its games: which
// titles have one, and which title each main's slug belongs to.
type mainIndex struct {
	titles map[int64]struct{}
	bySlug map[string]int64
}

// indexMains reads the main descriptors among rows. Their title slugs must
// already be filled.
func indexMains(rows []descriptorRow) mainIndex {
	mains := mainIndex{titles: make(map[int64]struct{}), bySlug: make(map[string]int64)}
	for i := range rows {
		if _, alternate := alternativesFolder(rows[i].path); alternate {
			continue
		}
		mains.titles[rows[i].titleDBID] = struct{}{}
		if rows[i].titleSlug != "" {
			mains.bySlug[rows[i].titleSlug] = rows[i].titleDBID
		}
	}
	return mains
}

// otherGame reports whether an alternate is another game's: the folder it is
// filed under is named after one main descriptor and its own title has a
// different one. An alternate that shares the title of a main in a folder
// named after nothing is the only thing tying that folder to its game, so it
// stays the folder's.
func (m mainIndex) otherGame(row *descriptorRow) bool {
	folder, alternate := alternativesFolder(row.path)
	if !alternate {
		return false
	}
	folderMain, named := m.bySlug[slugs.Slugify(slugs.MediaTypeGame, folder)]
	_, own := m.titles[row.titleDBID]
	return named && own && folderMain != row.titleDBID
}
