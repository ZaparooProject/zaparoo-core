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

// CanonicalIsExclusive maps each canonical tag type to its IsExclusive flag.
//
// IsExclusive = true  → single-value per entity; scrapers perform delete-then-insert.
// IsExclusive = false → additive; scrapers use INSERT OR IGNORE.
//
// Types absent from this map are considered non-exclusive (false) by default.
// This map is used both by SeedCanonicalTags (to set the flag at insert time) and
// by the indexing pipeline when creating dynamic tag types at runtime.
var CanonicalIsExclusive = map[TagType]bool{
	// Title-level metadata — one authoritative value per title
	TagTypeDeveloper: true,
	TagTypePublisher: true,
	TagTypeYear:      true,
	TagTypeRating:    true,

	// File-level single-value types
	TagTypeRev:       true,
	TagTypeDisc:      true,
	TagTypeDiscTotal: true,
	TagTypePlayers:   true,
	TagTypeExtension: true,
	TagTypeMedia:     true,
	TagTypeArcadeBoard: true,

	// Sequential ordering types
	TagTypeSeason:  true,
	TagTypeEpisode: true,
	TagTypeTrack:   true,
	TagTypeVolume:  true,
	TagTypeIssue:   true,

	// Single status types
	TagTypeUnfinished: true,
	TagTypeCopyright:  true,

	// Additive types — explicit false for documentation; these are the default
	TagTypeLang:          false,
	TagTypeRegion:        false,
	TagTypeDump:          false,
	TagTypeGameGenre:     false, // seeded as "gamegenre" — scrapers use TagTypeGenre
	TagTypeGenre:         false,
	TagTypeCompatibility: false,
}

// IsExclusiveType returns true if the given tag type is exclusive (single-value).
// Unknown types default to false (additive).
func IsExclusiveType(t TagType) bool {
	return CanonicalIsExclusive[t]
}
