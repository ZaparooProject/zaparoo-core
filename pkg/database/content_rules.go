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

// Content rule modes. A profile's mode decides what happens to media no
// entry matches.
const (
	// ContentModeAll allows everything except what a deny entry or the
	// hidden flag excludes.
	ContentModeAll = "all"
	// ContentModeAllowed allows only what an allow entry matches.
	ContentModeAllowed = "allowed"
)

// Content rule entry effects.
const (
	ContentEffectAllow = "allow"
	ContentEffectDeny  = "deny"
)

// Content rule entry kinds. The column is free text, so a kind added later
// is a new value here and nothing else.
const (
	// ContentKindSystem matches every media path of SystemID.
	ContentKindSystem = "system"
	// ContentKindDeck matches the members of the deck whose ID is Value.
	ContentKindDeck = "deck"
	// ContentKindMedia matches the one media path SystemID and Path name.
	ContentKindMedia = "media"
	// ContentKindTags matches media carrying the tag filter in Value, written
	// the way a tags argument is.
	ContentKindTags = "tags"
)

// ContentRuleEntry is one allow or deny entry of a profile's content rule.
type ContentRuleEntry struct {
	Effect   string `json:"effect"`
	Kind     string `json:"kind"`
	SystemID string `json:"systemId,omitempty"`
	Path     string `json:"path,omitempty"`
	Value    string `json:"value,omitempty"`
}

// ContentRule is what one profile may launch while the device is locked. An
// empty Mode means the profile has none of its own and the device default
// applies.
type ContentRule struct {
	Mode    string             `json:"mode"`
	Entries []ContentRuleEntry `json:"entries"`
}

// ValidContentMode reports whether mode is a known content rule mode.
func ValidContentMode(mode string) bool {
	return mode == ContentModeAll || mode == ContentModeAllowed
}
