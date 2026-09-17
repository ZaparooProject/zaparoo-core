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

package tags

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hacks, homebrew and public-domain works are distinct games; regions,
// translations, bootlegs, hacked or modified dumps and other versions of one
// game are not.
func TestIsGameVariantTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tagType string
		value   string
		want    bool
	}{
		{"unlicensed", "hack", true},
		{"release", "homebrew", true},
		{"release", "public-domain", true},
		{"copyright", "pd", true},
		// Versions of one game, never distinct games.
		{"unlicensed", "translation", false},
		{"unlicensed", "bootleg", false},
		{"unlicensed", "pirate", false},
		{"dump", "hacked", false},
		{"dump", "hacked:ffe", false},
		{"dump", "hacked:intro-removed", false},
		{"dump", "modified", false},
		{"dump", "fixed", false},
		{"dump", "bad", false},
		{"dump", "translated", false},
		{"region", "us", false},
		{"unfinished", "beta", false},
		// Values that belong to another type do not cross over.
		{"release", "hack", false},
		{"unlicensed", "homebrew", false},
		{"", "hack", false},
		{"unlicensed", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.tagType+":"+tc.value, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsGameVariantTag(tc.tagType, tc.value))
		})
	}
}

// Only variant "type:value" strings survive, in input order and deduplicated.
// Only the first colon separates the type, so a value holding a colon is kept
// whole and never matches a shorter variant value.
func TestGameVariantTagStrings(t *testing.T) {
	t.Parallel()
	got := GameVariantTagStrings([]string{
		"region:us", "unlicensed:hack", "dump:hacked:ffe", "unlicensed:hack:extra", "unlicensed:hack",
		"lang:en", "notatag", "release:homebrew",
	})
	assert.Equal(t, []string{"unlicensed:hack", "release:homebrew"}, got)
	assert.Empty(t, GameVariantTagStrings(nil))
	assert.Empty(t, GameVariantTagStrings([]string{"region:us"}))
}

// The SQL predicate is one bound (type, value) pair per rule, in rule order.
func TestGameVariantTagSQLPredicate(t *testing.T) {
	t.Parallel()
	clause, args := GameVariantTagSQLPredicate("tt.Type", "t.Tag")
	require.NotEmpty(t, GameVariantTags)
	assert.Equal(t, len(GameVariantTags), strings.Count(clause, "(tt.Type = ? AND t.Tag = ?)"))
	assert.True(t, strings.HasPrefix(clause, "("))
	assert.True(t, strings.HasSuffix(clause, ")"))
	assert.Len(t, args, strings.Count(clause, "?"))
	want := make([]any, 0, 2*len(GameVariantTags))
	for _, rule := range GameVariantTags {
		want = append(want, string(rule.Type), string(rule.Value))
	}
	assert.Equal(t, want, args)
}
