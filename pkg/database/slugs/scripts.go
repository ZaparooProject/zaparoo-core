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

package slugs

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

// DetectScript identifies the primary writing system used in a string.
// Returns the first matching script type, or ScriptLatin as the default.
func DetectScript(s string) ScriptType {
	// Fast path: Check if pure ASCII
	hasNonASCII := false
	for _, r := range s {
		if r > 127 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		return ScriptLatin
	}

	// Single pass through string checking Unicode ranges
	// Ordered by global prevalence for early exit
	for _, r := range s {
		// CJK Detection
		if (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
			(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
			(r >= 0x3040 && r <= 0x309F) || // Hiragana
			(r >= 0x30A0 && r <= 0x30FF) || // Katakana
			(r >= 0xAC00 && r <= 0xD7A3) || // Hangul
			r == 0x30FC || r == 0x30FB || r == 0x3005 {
			return ScriptCJK
		}

		// Cyrillic
		if r >= 0x0400 && r <= 0x04FF {
			return ScriptCyrillic
		}

		// Indic scripts
		if (r >= 0x0900 && r <= 0x097F) || // Devanagari
			(r >= 0x0980 && r <= 0x09FF) || // Bengali
			(r >= 0x0B80 && r <= 0x0BFF) || // Tamil
			(r >= 0x0C00 && r <= 0x0C7F) || // Telugu
			(r >= 0x0C80 && r <= 0x0CFF) || // Kannada
			(r >= 0x0D00 && r <= 0x0D7F) || // Malayalam
			(r >= 0x0A00 && r <= 0x0A7F) || // Gurmukhi
			(r >= 0x0A80 && r <= 0x0AFF) || // Gujarati
			(r >= 0x0B00 && r <= 0x0B7F) || // Oriya
			(r >= 0x0D80 && r <= 0x0DFF) { // Sinhala
			return ScriptIndic
		}

		// Arabic
		if r >= 0x0600 && r <= 0x06FF {
			return ScriptArabic
		}

		// Thai
		if r >= 0x0E00 && r <= 0x0E7F {
			return ScriptThai
		}

		// Greek
		if r >= 0x0370 && r <= 0x03FF {
			return ScriptGreek
		}

		// Hebrew
		if r >= 0x0590 && r <= 0x05FF {
			return ScriptHebrew
		}

		// Burmese/Myanmar
		if r >= 0x1000 && r <= 0x109F {
			return ScriptBurmese
		}

		// Khmer
		if r >= 0x1780 && r <= 0x17FF {
			return ScriptKhmer
		}

		// Lao
		if r >= 0x0E80 && r <= 0x0EFF {
			return ScriptLao
		}

		// Ethiopic/Amharic
		if r >= 0x1200 && r <= 0x137F {
			return ScriptAmharic
		}
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
	return DetectScript(s) == ScriptThai
}

// IsBurmese returns true if the string contains Burmese characters.
func IsBurmese(s string) bool {
	return DetectScript(s) == ScriptBurmese
}

// IsKhmer returns true if the string contains Khmer characters.
func IsKhmer(s string) bool {
	return DetectScript(s) == ScriptKhmer
}

// IsLao returns true if the string contains Lao characters.
func IsLao(s string) bool {
	return DetectScript(s) == ScriptLao
}
