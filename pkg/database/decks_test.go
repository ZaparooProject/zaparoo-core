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

package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeckID(t *testing.T) {
	t.Parallel()
	alphabet := strings.ToLower(DeckIDAlphabet)
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id, err := NewDeckID()
		require.NoError(t, err)
		assert.Len(t, id, DeckIDLength)
		assert.Equal(t, strings.ToLower(id), id, "minted IDs are lower-case")
		for _, r := range id {
			assert.True(t, strings.ContainsRune(alphabet, r), "%q is outside the alphabet", r)
		}
		normalized, normErr := NormalizeDeckID(id)
		require.NoError(t, normErr)
		assert.Equal(t, id, normalized)
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, 1000, "a thousand draws never collide")
}

func TestNormalizeDeckID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"0123456789AB", "0123456789ab", true},
		{"  abcdefghjkmn ", "abcdefghjkmn", true},
		{"ZZZZZZZZ", "zzzzzzzz", true},
		{"0123456789ABC", "", false},
		{"0123456", "", false},
		{"0123456789AI", "", false},
		{"0123456789AL", "", false},
		{"0123456789AO", "", false},
		{"0123456789AU", "", false},
		{"0123456789A-", "", false},
		{"", "", false},
		// Legacy eight-character IDs used every letter, so I, L, O and U are
		// their own characters there and are kept, never folded.
		{"KQ7RIL0U", "kq7ril0u", true},
		{" iloU1234 ", "ilou1234", true},
		{"KQ7RIL0-", "", false},
		{"KQ7RÍL0U", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDeckID(tc.raw)
			if !tc.ok {
				require.ErrorIs(t, err, ErrInvalidDeckID)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeckCardScriptsRoundTrip(t *testing.T) {
	t.Parallel()
	assert.Empty(t, EncodeDeckCardScripts(nil))
	assert.Nil(t, DecodeDeckCardScripts(""))
	assert.Nil(t, DecodeDeckCardScripts("not json"))
	scripts := []DeckCardScript{{Name: "Play", ZapScript: "**launch.system:SNES"}, {ZapScript: "**delay:100"}}
	assert.Equal(t, scripts, DecodeDeckCardScripts(EncodeDeckCardScripts(scripts)))
}

func TestIsMintedDeckID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		deckID string
		want   bool
	}{
		{name: "minted", deckID: "0123456789ab", want: true},
		{name: "minted upper case", deckID: "ABCDEFGHJKMN", want: true},
		{name: "legacy eight characters", deckID: "abcd1234", want: false},
		{name: "empty", deckID: "", want: false},
		{name: "too short", deckID: "0123456789a", want: false},
		{name: "too long", deckID: "0123456789abc", want: false},
		{name: "letter the minted alphabet leaves out", deckID: "0123456789ai", want: false},
		{name: "punctuation", deckID: "0123456789a-", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsMintedDeckID(tt.deckID))
		})
	}
}

// TestIsMintedDeckIDMatchesNewDeckID pins that what this device mints is
// always accepted, since only a minted ID can create a deck on an account.
func TestIsMintedDeckIDMatchesNewDeckID(t *testing.T) {
	t.Parallel()
	for range 50 {
		id, err := NewDeckID()
		require.NoError(t, err)
		assert.True(t, IsMintedDeckID(id), "minted %q must be accepted", id)
	}
}
