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

package kiosk_test

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/require"
)

// flagLibrary indexes two NES games and two PSX disc sets and returns their
// paths by name.
func flagLibrary(t *testing.T, db *database.Database) map[string]string {
	t.Helper()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	db.MediaDB = mediaDB

	root := t.TempDir()
	paths := map[string]string{
		"mario":  filepath.Join(root, "NES", "Mario.nes"),
		"other":  filepath.Join(root, "NES", "Other.nes"),
		"saga1":  filepath.Join(root, "PSX", "Saga", "Saga (Disc 1).chd"),
		"saga2":  filepath.Join(root, "PSX", "Saga", "Saga (Disc 2).chd"),
		"saga3":  filepath.Join(root, "PSX", "Saga", "Saga (Disc 3).chd"),
		"quest1": filepath.Join(root, "PSX", "Quest", "Quest (Disc 1).chd"),
		"quest2": filepath.Join(root, "PSX", "Quest", "Quest (Disc 2).chd"),
	}
	scantest.IndexMediaPaths(t, mediaDB, "NES", paths["mario"], paths["other"])
	scantest.IndexMediaPaths(t, mediaDB, "PSX",
		paths["saga1"], paths["saga2"], paths["saga3"], paths["quest1"], paths["quest2"])
	return paths
}

func setUserFlags(t *testing.T, db *database.Database, systemID, path string, row *database.MediaUserData) {
	t.Helper()
	row.SystemID = systemID
	row.Path = pathutil.CanonicalMediaPath(path)
	require.NoError(t, db.UserDB.UpsertMediaUserData(row))
}

func TestFavoritesRuleAllowsFavoriteGames(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	paths := flagLibrary(t, db)
	setUserFlags(t, db, "NES", paths["mario"], &database.MediaUserData{IsFavorite: true})
	// A favorite on a path the index does not hold opens nothing.
	setUserFlags(t, db, "NES", filepath.Join(filepath.Dir(paths["mario"]), "Gone.nes"),
		&database.MediaUserData{IsFavorite: true})
	setRule(t, db, kidProfileID, database.ContentModeAllowed, tagRule(database.ContentEffectAllow, "user:favorite"))
	enableOpen(t, lock)

	require.NoError(t, check(lock, kidProfileID, "NES", paths["mario"]))
	requireRefused(t, check(lock, kidProfileID, "NES", paths["other"]), kiosk.ReasonNotAllowed)
	requireRefused(t, check(lock, kidProfileID, "NES",
		filepath.Join(filepath.Dir(paths["mario"]), "Gone.nes")), kiosk.ReasonNotAllowed)
}

func TestFavoritesRuleAllowsEveryDiscOfAFavoriteSet(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	paths := flagLibrary(t, db)
	setUserFlags(t, db, "PSX", paths["saga1"], &database.MediaUserData{IsFavorite: true})
	// Play later on one disc is not a favorite on another.
	setUserFlags(t, db, "PSX", paths["quest2"], &database.MediaUserData{IsPlayLater: true})
	setRule(t, db, kidProfileID, database.ContentModeAllowed, tagRule(database.ContentEffectAllow, "user:favorite"))
	enableOpen(t, lock)

	for _, disc := range []string{"saga1", "saga2", "saga3"} {
		require.NoError(t, check(lock, kidProfileID, "PSX", paths[disc]), disc)
	}
	for _, disc := range []string{"quest1", "quest2"} {
		requireRefused(t, check(lock, kidProfileID, "PSX", paths[disc]), kiosk.ReasonNotAllowed)
	}
}

func TestDislikedRuleRefusesEveryDiscOfADislikedSet(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	paths := flagLibrary(t, db)
	setUserFlags(t, db, "PSX", paths["quest2"], &database.MediaUserData{IsDisliked: true})
	// A disc disliked while another is liked is the set liked, as the
	// folder shows it.
	setUserFlags(t, db, "PSX", paths["saga1"], &database.MediaUserData{IsLiked: true})
	setUserFlags(t, db, "PSX", paths["saga3"], &database.MediaUserData{IsDisliked: true})
	// Hidden is one disc's own.
	setUserFlags(t, db, "PSX", paths["saga2"], &database.MediaUserData{IsHidden: true, IsLiked: true})
	setRule(t, db, "", database.ContentModeAll, tagRule(database.ContentEffectDeny, "user:disliked"))
	enableOpen(t, lock)

	requireRefused(t, check(lock, "", "PSX", paths["quest1"]), kiosk.ReasonDenied)
	requireRefused(t, check(lock, "", "PSX", paths["quest2"]), kiosk.ReasonDenied)
	require.NoError(t, check(lock, "", "PSX", paths["saga1"]))
	require.NoError(t, check(lock, "", "PSX", paths["saga3"]))
	requireRefused(t, check(lock, "", "PSX", paths["saga2"]), kiosk.ReasonHidden)
	require.NoError(t, check(lock, "", "NES", paths["mario"]))
}
