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

// Lookups shared by more than one metadata source. Each maps a source's own
// wording onto a canonical value and reports ok=false for anything it does not
// know, which the caller drops. A source-specific table (one scraper's genre
// strings) lives with that scraper instead.

// lookupKey folds case, surrounding space, runs of inner whitespace and the
// common separators so "Capcom CPS-2", "capcom cps2" and "CAPCOM  CPS 2" all
// meet the same key.
func lookupKey(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			_, _ = b.WriteRune(r)
		case r > 0x7f:
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

// LookupLanguageWord resolves a language code or name to its canonical lang
// value: "en", "English", "EN". ok is false for a word the vocabulary lacks.
func LookupLanguageWord(raw string) (value TagValue, ok bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if IsCanonicalValue(TagTypeLang, TagValue(key)) {
		return TagValue(key), true
	}
	key = strings.ReplaceAll(key, " ", "-")
	for _, tag := range allTagMappings[key] {
		if tag.Type == TagTypeLang {
			return tag.Value, true
		}
	}
	return "", false
}

// LookupArcadeBoard resolves a source's arcade hardware name ("CPS2",
// "Capcom CPS-2", "Neo-Geo MVS") to its canonical arcadeboard value.
func LookupArcadeBoard(raw string) (value TagValue, ok bool) {
	value, ok = arcadeBoardAliases[lookupKey(raw)]
	return value, ok
}

// LookupFranchise resolves a source's series or family name ("Street
// Fighter", "Castlevania / Akumajo Dracula") to its canonical
// search:franchise value, e.g. "franchise:streetfighter".
func LookupFranchise(raw string) (value TagValue, ok bool) {
	value, ok = franchiseAliases[lookupKey(raw)]
	return value, ok
}
