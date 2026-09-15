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

package librarysync

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
)

func fields(favorite bool, intent, reaction string) stateFields {
	return stateFields{Favorite: favorite, Intent: intent, Reaction: reaction}
}

const (
	none      = database.LibraryIntentNone
	playLater = database.LibraryIntentPlayLater
	liked     = database.LibraryReactionLiked
	disliked  = database.LibraryReactionDisliked
)

func TestLocalGameFieldsRollUpCopies(t *testing.T) {
	t.Parallel()
	game := &localGame{rows: []database.MediaUserData{
		{IsDisliked: true},
		{IsPlayLater: true},
	}}
	assert.Equal(t, fields(false, playLater, disliked), game.fields())

	game.rows = append(game.rows, database.MediaUserData{IsFavorite: true})
	assert.Equal(t, fields(true, playLater, none), game.fields(), "a favorite copy outranks a disliked one")

	game.rows = append(game.rows, database.MediaUserData{IsLiked: true})
	assert.Equal(t, fields(true, playLater, liked), game.fields())
}

func TestPreferredTagsNeedOneStarredCopy(t *testing.T) {
	t.Parallel()
	usa := database.MediaUserData{IsFavorite: true, Tags: []string{"region:us", "extension:nes", "unlicensed:hack"}}
	eu := database.MediaUserData{IsFavorite: true, Tags: []string{"region:eu"}}
	assert.Equal(t, []string{"region:us"}, (&localGame{rows: []database.MediaUserData{usa}}).preferredTags(),
		"context and variant tags are not versions")
	assert.Nil(t, (&localGame{rows: []database.MediaUserData{usa, eu}}).preferredTags())
	assert.Nil(t, (&localGame{rows: []database.MediaUserData{{IsLiked: true}}}).preferredTags())
}

func TestMergeFields(t *testing.T) {
	t.Parallel()
	base := fields(true, none, none)
	tests := []struct {
		name    string
		desired stateFields
		server  stateFields
		want    stateFields
	}{
		{
			name:    "nothing changed here",
			desired: base, server: fields(false, playLater, liked), want: fields(false, playLater, liked),
		},
		{
			name:    "each side keeps its own field",
			desired: fields(true, playLater, none), server: fields(true, none, liked),
			want: fields(true, playLater, liked),
		},
		{
			name:    "a dislike here clears the favorite there",
			desired: fields(false, none, disliked), server: fields(true, none, none),
			want: fields(false, none, disliked),
		},
		{
			name:    "an unchanged favorite follows the account",
			desired: fields(true, none, none), server: fields(false, none, disliked),
			want: fields(false, none, disliked),
		},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, mergeFields(tt.desired, base, tt.server), tt.name)
	}
	assert.Equal(t, fields(true, none, none),
		mergeFields(fields(true, none, none), fields(false, none, none), fields(false, none, disliked)),
		"a new favorite here wins over a dislike there")
}

func TestAdoptedFieldsOnlyNameWhatTheAccountChangedAndWon(t *testing.T) {
	t.Parallel()
	held := fields(true, none, none)
	server := fields(false, playLater, liked)
	assert.Equal(t, fieldMask{Favorite: true, Intent: true, Reaction: true},
		adoptedFields(held, server, server), "every account change the merge adopted")
	assert.Equal(t, fieldMask{Intent: true, Reaction: true},
		adoptedFields(held, server, fields(true, playLater, liked)), "a favorite kept here is not applied")
	assert.Equal(t, fieldMask{}, adoptedFields(held, held, held), "nothing changed on the account")
}

func TestPresentFieldsNeverTurnAnythingOff(t *testing.T) {
	t.Parallel()
	assert.Equal(t, fieldMask{}, presentFields(defaultStateFields))
	assert.Equal(t, fieldMask{Favorite: true, Reaction: true}, presentFields(fields(true, none, disliked)))
}

func TestSameStringsIgnoresOrder(t *testing.T) {
	t.Parallel()
	assert.True(t, sameStrings([]string{"region:us", "lang:en"}, []string{"lang:en", "region:us"}))
	assert.True(t, sameStrings(nil, []string{}))
	assert.False(t, sameStrings([]string{"region:us"}, []string{"region:eu"}))
	assert.False(t, sameStrings([]string{"region:us"}, nil))
}

func TestDesiredFieldsForGameHeldNowhere(t *testing.T) {
	t.Parallel()
	base := &database.LibraryStateSyncRow{Favorite: true, Intent: none, Reaction: liked, Unmatched: true}
	assert.Equal(t, fields(true, none, liked), desiredFields(nil, base), "no copy is not a cleared state")

	game := &localGame{rows: []database.MediaUserData{{IsPlayLater: true}}}
	assert.Equal(t, fields(true, playLater, liked), desiredFields(game, base), "local additions layer on top")

	game = &localGame{rows: []database.MediaUserData{{IsDisliked: true}}}
	assert.Equal(t, fields(false, none, disliked), desiredFields(game, base))

	future := &database.LibraryStateSyncRow{Intent: "watched", Reaction: "loved"}
	assert.Equal(t, fields(false, "watched", "loved"), desiredFields(nil, future),
		"values this Core does not know are kept")
}
