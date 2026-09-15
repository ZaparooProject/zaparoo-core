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

func TestDeckSyncRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	_, found, err := db.GetDeckSync("0123456789ab")
	require.NoError(t, err)
	assert.False(t, found)

	row := database.DeckSyncRow{
		DeckID: "0123456789ab", Snapshot: `{"name":"Weekend"}`, Revision: 7, Locked: true,
		Conflicts: 2, RejectedCode: "unknown_card", RejectedHash: "abc",
	}
	require.NoError(t, db.UpsertDeckSync([]database.DeckSyncRow{row, {DeckID: "bbbbbbbbbbbb"}}))
	got, found, err := db.GetDeckSync("0123456789ab")
	require.NoError(t, err)
	require.True(t, found)
	assert.NotZero(t, got.UpdatedAt)
	got.UpdatedAt = 0
	assert.Equal(t, row, got)

	rows, err := db.ListDeckSync()
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	require.NoError(t, db.DeleteDeckSync([]string{"bbbbbbbbbbbb"}))
	rows, err = db.ListDeckSync()
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	require.NoError(t, db.ClearDeckSync())
	rows, err = db.ListDeckSync()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestRenameDeckMovesItsSyncRow(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.CreateDeck(testDeck("0123456789ab", "Mine", scriptItem("A", "**a"))))
	require.NoError(t, db.UpsertDeckSync([]database.DeckSyncRow{{DeckID: "0123456789ab", Snapshot: "{}"}}))

	require.NoError(t, db.RenameDeck("0123456789ab", "zzzzzzzzzzzz"))
	deck, err := db.GetDeck("zzzzzzzzzzzz")
	require.NoError(t, err)
	assert.Equal(t, "Mine", deck.Name)
	require.Len(t, deck.Items, 1)
	_, found, err := db.GetDeckSync("zzzzzzzzzzzz")
	require.NoError(t, err)
	assert.True(t, found)

	require.ErrorIs(t, db.RenameDeck("0123456789ab", "yyyyyyyyyyyy"), database.ErrDeckNotFound)
}
