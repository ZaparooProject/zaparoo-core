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

package misterarcade

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// controlValues maps one catalog control phrase to the canonical input values
// it states. The catalog writes these columns as free text with inconsistent
// case and no fixed separator, so every phrase it actually uses is listed
// rather than pattern-matched.
//
// Phrases in ignoredControls name a control family whose canonical value
// requires a detail the catalog did not give; they are dropped rather than
// resolved to the nearest guess.
var controlValues = map[string][]tags.TagValue{ //nolint:gochecknoglobals // Static mapping table.
	"8-way":            {tags.TagInputJoystick8},
	"8-way double":     {tags.TagInputJoystick8, tags.TagInputJoystickDouble},
	"4-way":            {tags.TagInputJoystick4},
	"4-way diagonal":   {tags.TagInputJoystick4},
	"2-way horizontal": {tags.TagInputJoystick2H},
	"2-way vertical":   {tags.TagInputJoystick2V},
	"rotary joystick":  {tags.TagInputJoystickRotary},
	"rotary":           {tags.TagInputJoystickRotary},
	"twin stick":       {tags.TagInputStickTwin},
	"double joystick":  {tags.TagInputJoystickDouble},
	"double joysticks": {tags.TagInputJoystickDouble},
	"optical stick":    {tags.TagInputOptical},
	"trackball":        {tags.TagInputTrackball},
	"spinner":          {tags.TagInputSpinner},
	"dial":             {tags.TagInputDial},
	"paddle":           {tags.TagInputPaddle},
	"wheel":            {tags.TagInputWheel},
	"lightgun":         {tags.TagInputLightgun},
	"mahjong":          {tags.TagInputKeyboardMahjong},
	"pedal":            {tags.TagInputPedals1},
	"pedals":           {tags.TagInputPedals1},
}

// ignoredControls are the catalog's control phrases that deliberately write
// nothing: a throw count with no axis ("2", "2-way"), a bare "stick",
// "positional" with no position count, "buttons" (the button count column
// says how many) and "tilt". They are not reported as unmapped.
var ignoredControls = map[string]struct{}{ //nolint:gochecknoglobals // Static lookup set.
	"2":            {},
	"2-way":        {},
	"stick":        {},
	"positional":   {},
	"buttons":      {},
	"buttons only": {},
	"tilt":         {},
}

// controlSeparators splits a control column into phrases. A comma always
// separates, and so does a spaced hyphen — but only a spaced one, because the
// joystick phrases spell their throw count with a bare hyphen ("8-way").
var controlSeparators = regexp.MustCompile(`\s*,\s*|\s+-\s+|\s*/\s*`) //nolint:gochecknoglobals // Compiled once.

// controlTags resolves the two control columns to canonical input values,
// preserving first-seen order and dropping duplicates. Unrecognized phrases,
// other than the deliberately ignored ones, are returned separately so the
// caller can report what the catalog said without writing a guess.
func controlTags(columns ...string) (values []tags.TagValue, unknown []string) {
	seen := make(map[tags.TagValue]struct{})
	for _, column := range columns {
		for _, phrase := range controlSeparators.Split(field(column), -1) {
			phrase = strings.ToLower(strings.Join(strings.Fields(phrase), " "))
			if phrase == "" {
				continue
			}
			mapped, known := controlValues[phrase]
			if !known {
				if _, ignored := ignoredControls[phrase]; !ignored {
					unknown = append(unknown, phrase)
				}
				continue
			}
			for _, value := range mapped {
				if _, dup := seen[value]; dup {
					continue
				}
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
	}
	return values, unknown
}

// buttonTag resolves the button-count column to a canonical buttons value.
// Zero buttons is a real answer ("this game has none") but not a control the
// vocabulary names, so it writes nothing; a count the vocabulary does not list
// is not written either, and ok=false lets the caller report it.
func buttonTag(numButtons string) (value tags.TagValue, ok bool) {
	count, err := strconv.Atoi(field(numButtons))
	if err != nil || count <= 0 {
		return "", false
	}
	value = tags.TagValue("buttons:" + strconv.Itoa(count))
	if !tags.IsCanonicalValue(tags.TagTypeInput, value) {
		return "", false
	}
	return value, true
}

// maxPlayers bounds the player counts parsed from the catalog. Counts inside
// it that the vocabulary does not list (11, say) are still dropped.
const maxPlayers = 16

// playerPattern reads the catalog's player column: a count or a range, then an
// optional parenthesised mode. Examples: "1", "2 (simultaneous)",
// "2-4 (alternating)".
//
//nolint:gochecknoglobals // Compiled once.
var playerPattern = regexp.MustCompile(`^(\d+)(?:\s*-\s*(\d+))?(?:\s*\(([^)]*)\))?$`)

// playerTags resolves the player column to every supported count plus the play
// mode. A range writes each count in it, because players is an additive type
// and a game that seats two through four genuinely supports all three — a
// search for two-player games should find it. ok is false when the column
// said something that could not be written in full: an unparseable value, a
// count the vocabulary does not list, or an unknown mode.
func playerTags(players string) (values []tags.TagValue, ok bool) {
	raw := field(players)
	if raw == "" {
		return nil, true
	}
	match := playerPattern.FindStringSubmatch(raw)
	if match == nil {
		return nil, false
	}
	low, err := strconv.Atoi(match[1])
	if err != nil || low <= 0 || low > maxPlayers {
		return nil, false
	}
	high := low
	if match[2] != "" {
		high, err = strconv.Atoi(match[2])
		if err != nil || high < low || high > maxPlayers {
			return nil, false
		}
	}
	ok = true
	values = make([]tags.TagValue, 0, high-low+2)
	for count := low; count <= high; count++ {
		value := tags.TagValue(strconv.Itoa(count))
		if !tags.IsCanonicalValue(tags.TagTypePlayers, value) {
			ok = false
			continue
		}
		values = append(values, value)
	}
	switch strings.ToLower(strings.TrimSpace(match[3])) {
	case "":
	case "simultaneous":
		values = append(values, tags.TagPlayersSimultaneous)
	case "alternating":
		values = append(values, tags.TagPlayersAlt)
	default:
		ok = false
	}
	return values, ok
}
