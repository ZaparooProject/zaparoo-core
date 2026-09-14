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

func TestIsGameVariantTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tagType string
		value   string
		want    bool
	}{
		{"unlicensed", "hack", true},
		{"dump", "hacked", true},
		{"dump", "hacked:ffe", true},
		{"dump", "hacked:intro-removed", true},
		{"dump", "modified", true},
		{"release", "homebrew", true},
		{"release", "public-domain", true},
		{"copyright", "pd", true},
		// Versions of one game, never distinct games.
		{"unlicensed", "translation", false},
		{"unlicensed", "bootleg", false},
		{"unlicensed", "pirate", false},
		{"dump", "fixed", false},
		{"dump", "bad", false},
		{"dump", "translated", false},
		{"dump", "hackedx", false},
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

func TestGameVariantTagStrings(t *testing.T) {
	t.Parallel()
	got := GameVariantTagStrings([]string{
		"region:us", "unlicensed:hack", "dump:hacked:ffe", "unlicensed:hack", "lang:en", "notatag", "release:homebrew",
	})
	assert.Equal(t, []string{"unlicensed:hack", "dump:hacked:ffe", "release:homebrew"}, got)
	assert.Empty(t, GameVariantTagStrings(nil))
	assert.Empty(t, GameVariantTagStrings([]string{"region:us"}))
}

// The SQL predicate carries one bind per comparison, and the prefix rule
// escapes LIKE metacharacters so a value cannot widen the match.
func TestGameVariantTagSQLPredicate(t *testing.T) {
	t.Parallel()
	clause, args := GameVariantTagSQLPredicate("tt.Type", "t.Tag")
	assert.True(t, strings.HasPrefix(clause, "("))
	assert.True(t, strings.HasSuffix(clause, ")"))
	assert.Len(t, args, strings.Count(clause, "?"))
	assert.Contains(t, clause, "t.Tag LIKE ? ESCAPE '\\'")
	assert.Contains(t, args, "hacked:%")
	for _, rule := range GameVariantTags {
		assert.Contains(t, args, string(rule.Type))
		assert.Contains(t, args, string(rule.Value))
	}
	require.NotEmpty(t, args)
}

func TestEscapeLikePattern(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hacked", escapeLikePattern("hacked"))
	assert.Equal(t, `a\%b\_c\\d`, escapeLikePattern(`a%b_c\d`))
}
