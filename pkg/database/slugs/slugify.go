// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package slugs

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// SlugifyString converts a game title to a normalized slug for cross-platform matching.
//
// Multi-Stage Normalization Pipeline:
//   Stage 0: Leading Article Normalization - "The Legend" / "Legend, The" → "Legend"
//   Stage 1: Unicode Normalization - "Pokémon" → "Pokemon"
//   Stage 2: Ampersand Normalization - "Sonic & Knuckles" → "Sonic and Knuckles"
//   Stage 3: Metadata Stripping - "(USA) [!]" removed
//   Stage 4: Separator Normalization - "Zelda: Link's Awakening" → "Zelda Links Awakening"
//   Stage 5: Roman Numeral Conversion - "VII" → "7"
//   Stage 6: Final Slugification - Lowercase, alphanumeric only
//
// This function is deterministic and idempotent:
//   SlugifyString(SlugifyString(x)) == SlugifyString(x)
//
// Example:
//   SlugifyString("The Legend of Zelda: Ocarina of Time (USA) [!]")
//   → "legendofzeldaocarinaoftime"

// TODO: titles with no latin characters at all (e.g. Chinese-only) will be
// reduced to an empty string. need to handle during insert and search.

var (
	parenthesesRegex     = regexp.MustCompile(`\s*\([^)]*\)`)
	bracketsRegex        = regexp.MustCompile(`\s*\[[^\]]*\]`)
	separatorsRegex      = regexp.MustCompile(`[:_\-]+`)
	nonAlphanumRegex     = regexp.MustCompile(`[^a-z0-9]+`)
	romanNumeralI        = regexp.MustCompile(`\sI($|[\s:_\-])`)
	romanNumeralPatterns = map[string]*regexp.Regexp{
		"IX":   regexp.MustCompile(`\bIX\b`),
		"VIII": regexp.MustCompile(`\bVIII\b`),
		"VII":  regexp.MustCompile(`\bVII\b`),
		"VI":   regexp.MustCompile(`\bVI\b`),
		"IV":   regexp.MustCompile(`\bIV\b`),
		"V":    regexp.MustCompile(`\bV\b`),
		"III":  regexp.MustCompile(`\bIII\b`),
		"II":   regexp.MustCompile(`\bII\b`),
	}
	romanNumeralReplacements = map[string]string{
		"IX":   "9",
		"VIII": "8",
		"VII":  "7",
		"VI":   "6",
		"IV":   "4",
		"V":    "5",
		"III":  "3",
		"II":   "2",
	}
)

func SlugifyString(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(s), "the ") {
		s = s[4:]
		s = strings.TrimSpace(s)
	}
	if strings.HasSuffix(strings.ToLower(s), ", the") {
		s = s[:len(s)-5]
		s = strings.TrimSpace(s)
	}

	t := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	if normalized, _, err := transform.String(t, s); err == nil {
		s = normalized
	}

	s = strings.ReplaceAll(s, "&", " and ")

	s = parenthesesRegex.ReplaceAllString(s, "")
	s = bracketsRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	s = separatorsRegex.ReplaceAllString(s, " ")

	upperS := strings.ToUpper(s)

	upperS = romanNumeralI.ReplaceAllString(upperS, " 1$1")

	for roman, arabic := range romanNumeralReplacements {
		upperS = romanNumeralPatterns[roman].ReplaceAllString(upperS, arabic)
	}

	s = strings.ToLower(upperS)

	s = nonAlphanumRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	return s
}
