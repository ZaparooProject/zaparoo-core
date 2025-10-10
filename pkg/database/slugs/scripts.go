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

import "regexp"

// ScriptType represents different writing systems supported by the slug system.
// Each script type may require different normalization strategies.
type ScriptType int

const (
	ScriptLatin    ScriptType = iota // Latin alphabet (English, French, Spanish, etc.)
	ScriptCJK                        // Chinese, Japanese, Korean
	ScriptCyrillic                   // Russian, Ukrainian, Bulgarian, Serbian, etc.
	ScriptGreek                      // Greek
	ScriptIndic                      // Devanagari, Bengali, Tamil, Telugu, etc.
	ScriptArabic                     // Arabic, Urdu, Persian/Farsi
	ScriptHebrew                     // Hebrew
	ScriptThai                       // Thai (requires n-gram matching)
	ScriptBurmese                    // Burmese/Myanmar (requires n-gram matching)
	ScriptKhmer                      // Khmer/Cambodian (requires n-gram matching)
	ScriptLao                        // Lao (requires n-gram matching)
	ScriptAmharic                    // Amharic/Ethiopic
)

// Script detection regexes
var (
	// CJK: Chinese, Japanese, Korean (already defined in slugify.go)
	// cjkRegex = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}\x{30FC}\x{30FB}\x{3005}]`)

	// Cyrillic script (Russian, Ukrainian, Bulgarian, Serbian, etc.)
	cyrillicRegex = regexp.MustCompile(`[\p{Cyrillic}]`)

	// Greek script
	greekRegex = regexp.MustCompile(`[\p{Greek}]`)

	indicRegex = regexp.MustCompile(
		`[\p{Devanagari}\p{Bengali}\p{Tamil}\p{Telugu}\p{Kannada}\p{Malayalam}` +
			`\p{Gurmukhi}\p{Gujarati}\p{Oriya}\p{Sinhala}]`,
	)

	// Arabic script (includes Urdu, Persian/Farsi)
	arabicRegex = regexp.MustCompile(`[\p{Arabic}]`)

	// Hebrew script
	hebrewRegex = regexp.MustCompile(`[\p{Hebrew}]`)

	// Southeast Asian scripts (no word boundaries, require n-gram matching)
	thaiRegex    = regexp.MustCompile(`[\p{Thai}]`)
	burmeseRegex = regexp.MustCompile(`[\p{Myanmar}]`)
	khmerRegex   = regexp.MustCompile(`[\p{Khmer}]`)
	laoRegex     = regexp.MustCompile(`[\p{Lao}]`)

	// Amharic/Ethiopic script
	amharicRegex = regexp.MustCompile(`[\p{Ethiopic}]`)
)

// detectScript identifies the primary writing system used in a string.
// Detection uses short-circuit evaluation in order of prevalence to optimize performance.
// Returns the first matching script type, or ScriptLatin as the default.
func detectScript(s string) ScriptType {
	// Check in order of global prevalence for performance optimization
	if cjkRegex.MatchString(s) {
		return ScriptCJK
	}
	if cyrillicRegex.MatchString(s) {
		return ScriptCyrillic
	}
	if indicRegex.MatchString(s) {
		return ScriptIndic
	}
	if arabicRegex.MatchString(s) {
		return ScriptArabic
	}
	if thaiRegex.MatchString(s) {
		return ScriptThai
	}
	if greekRegex.MatchString(s) {
		return ScriptGreek
	}
	if hebrewRegex.MatchString(s) {
		return ScriptHebrew
	}
	if burmeseRegex.MatchString(s) {
		return ScriptBurmese
	}
	if khmerRegex.MatchString(s) {
		return ScriptKhmer
	}
	if laoRegex.MatchString(s) {
		return ScriptLao
	}
	if amharicRegex.MatchString(s) {
		return ScriptAmharic
	}
	return ScriptLatin
}

// needsUnicodeSlug returns true if the script requires preserving Unicode characters
// in the final slug (as opposed to ASCII-only slugs for pure Latin text).
func needsUnicodeSlug(script ScriptType) bool {
	switch script {
	case ScriptCJK, ScriptCyrillic, ScriptGreek, ScriptIndic,
		ScriptArabic, ScriptHebrew, ScriptThai, ScriptBurmese,
		ScriptKhmer, ScriptLao, ScriptAmharic:
		return true
	case ScriptLatin:
		return false
	default:
		return false
	}
}

// needsNGramMatching returns true if the script has no word boundaries and
// requires character n-gram matching instead of standard string similarity.
func needsNGramMatching(script ScriptType) bool {
	switch script {
	case ScriptThai, ScriptBurmese, ScriptKhmer, ScriptLao:
		return true
	default:
		return false
	}
}

// IsThai returns true if the string contains Thai characters.
// This is a convenience function for the resolution workflow.
func IsThai(s string) bool {
	return detectScript(s) == ScriptThai
}

// IsBurmese returns true if the string contains Burmese characters.
func IsBurmese(s string) bool {
	return detectScript(s) == ScriptBurmese
}

// IsKhmer returns true if the string contains Khmer characters.
func IsKhmer(s string) bool {
	return detectScript(s) == ScriptKhmer
}

// IsLao returns true if the string contains Lao characters.
func IsLao(s string) bool {
	return detectScript(s) == ScriptLao
}

func requiresNGramMatching(s string) bool {
	return needsNGramMatching(detectScript(s))
}
