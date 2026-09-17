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

import "strings"

// GameVariantTag names a scanner file tag whose presence makes a file a
// different game from the plain release that shares its title, rather than a
// version of it.
type GameVariantTag struct {
	Type  TagType
	Value TagValue
}

// GameVariantTags is the rule for which tags make a file its own game: ROM
// hacks, homebrew and public-domain works. Regions, revisions, translations,
// fix patches, bootlegs and hacked or modified dumps (mostly trainers, removed
// intros and copier headers) are versions of one game and are deliberately
// absent. A title launch must always carry these tags, even on a device that
// holds no plain sibling, or another device resolves the plain release
// instead. Changing the set changes which games are distinct everywhere the
// rule is read, so it is defined once here.
var GameVariantTags = []GameVariantTag{
	{Type: TagTypeUnlicensed, Value: TagUnlicensedHack},
	{Type: TagTypeRelease, Value: TagReleaseHomebrew},
	{Type: TagTypeRelease, Value: TagReleasePublicDomain},
	{Type: TagTypeCopyright, Value: TagCopyrightPD},
}

// IsGameVariantTag reports whether one tag makes its file a distinct game
// under GameVariantTags.
func IsGameVariantTag(tagType, value string) bool {
	for i := range GameVariantTags {
		rule := &GameVariantTags[i]
		if string(rule.Type) == tagType && string(rule.Value) == value {
			return true
		}
	}
	return false
}

// GameVariantTagSQLPredicate renders GameVariantTags as a parameterized SQL
// predicate over a tag type column and a stored tag value column. Every
// variant value is non-numeric, so the stored (padded) form equals the
// canonical value and the predicate compares it directly. The caller appends
// the returned args after any parameters that precede the clause.
func GameVariantTagSQLPredicate(typeCol, tagCol string) (clause string, args []any) {
	parts := make([]string, 0, len(GameVariantTags))
	args = make([]any, 0, 2*len(GameVariantTags))
	for i := range GameVariantTags {
		rule := &GameVariantTags[i]
		parts = append(parts, "("+typeCol+" = ? AND "+tagCol+" = ?)")
		args = append(args, string(rule.Type), string(rule.Value))
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// GameVariantTagStrings returns the subset of "type:value" tag strings that
// make a file a distinct game, in input order with duplicates removed. Values
// may themselves hold colons (dump:hacked:ffe), so only the first colon
// separates the type.
func GameVariantTagStrings(typeValues []string) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, tv := range typeValues {
		tagType, value, found := strings.Cut(tv, ":")
		if !found || !IsGameVariantTag(tagType, value) {
			continue
		}
		if _, dup := seen[tv]; dup {
			continue
		}
		seen[tv] = struct{}{}
		out = append(out, tv)
	}
	return out
}
