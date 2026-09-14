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

package userdb

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibraryStateSyncRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	hack := database.LibraryStateSyncRow{
		IdentityKey: "game|SNES|supermarioworld|unlicensed:hack", MediaType: "Game", SystemID: "SNES",
		CoreSlug: "supermarioworld", VariantTags: []string{"unlicensed:hack"}, Title: "Super Mario World",
		Favorite: true, Intent: database.LibraryIntentPlayLater, Reaction: database.LibraryReactionNone,
		PreferredTags: []string{"lang:en"}, Revision: 12,
	}
	plain := database.LibraryStateSyncRow{
		IdentityKey: "game|NES|metroid|", MediaType: "Game", SystemID: "NES", CoreSlug: "metroid",
		Intent: database.LibraryIntentNone, Reaction: database.LibraryReactionLiked, Revision: 3,
		Unmatched: true, RejectedCode: "invalid_state", RejectedHash: "abc",
	}
	require.NoError(t, db.UpsertLibraryStateSync([]database.LibraryStateSyncRow{hack, plain}))

	rows, err := db.ListLibraryStateSync()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byKey := map[string]database.LibraryStateSyncRow{}
	for _, row := range rows {
		assert.NotZero(t, row.UpdatedAt)
		row.UpdatedAt = 0
		byKey[row.IdentityKey] = row
	}
	assert.Equal(t, hack, byKey[hack.IdentityKey])
	plain.VariantTags = []string{}
	plain.PreferredTags = []string{}
	assert.Equal(t, plain, byKey[plain.IdentityKey], "empty tag lists read back as empty, never nil")

	hack.Revision = 13
	hack.Favorite = false
	require.NoError(t, db.UpsertLibraryStateSync([]database.LibraryStateSyncRow{hack}))
	rows, err = db.ListLibraryStateSync()
	require.NoError(t, err)
	assert.Len(t, rows, 2, "an upsert replaces the row for the same identity")

	require.NoError(t, db.DeleteLibraryStateSync([]string{plain.IdentityKey, "missing"}))
	rows, err = db.ListLibraryStateSync()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(13), rows[0].Revision)

	require.NoError(t, db.ClearLibraryStateSync())
	rows, err = db.ListLibraryStateSync()
	require.NoError(t, err)
	assert.Empty(t, rows)
}
