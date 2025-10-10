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
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// removeDiacritics strips diacritical marks from text.
// Used for Latin, Cyrillic, and Greek scripts where diacritics can be safely removed for matching.
func removeDiacritics(s string) string {
	t := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	if normalized, _, err := transform.String(t, s); err == nil {
		return normalized
	}
	return s
}

// removeArabicVowelMarks strips Arabic diacritical marks (Tashkeel/Harakat).
// These are optional vowel marks that users typically don't type when searching.
// Removes characters in the range U+064B to U+065F.
func removeArabicVowelMarks(s string) string {
	vowelMarks := runes.Predicate(func(r rune) bool {
		return r >= 0x064B && r <= 0x065F
	})
	t := runes.Remove(vowelMarks)
	if result, _, err := transform.String(t, s); err == nil {
		return result
	}
	return s
}

// removeHebrewVowelMarks strips Hebrew diacritical marks (Niqqud).
// These are optional vowel points that users typically don't type when searching.
// Removes characters in the range U+0591 to U+05C7.
func removeHebrewVowelMarks(s string) string {
	vowelMarks := runes.Predicate(func(r rune) bool {
		return r >= 0x0591 && r <= 0x05C7
	})
	t := runes.Remove(vowelMarks)
	if result, _, err := transform.String(t, s); err == nil {
		return result
	}
	return s
}

// normalizeGreekPunctuation converts Greek-specific punctuation to standard forms.
func normalizeGreekPunctuation(s string) string {
	s = strings.ReplaceAll(s, ";", "?") // Greek question mark (looks like semicolon)
	return s
}

// normalizeArabicPunctuation converts Arabic-specific punctuation to standard forms.
func normalizeArabicPunctuation(s string) string {
	s = strings.ReplaceAll(s, "،", ",") // Arabic comma
	s = strings.ReplaceAll(s, "؛", ";") // Arabic semicolon
	s = strings.ReplaceAll(s, "؟", "?") // Arabic question mark
	return s
}

// normalizeAmharicPunctuation converts Ethiopic-specific punctuation to standard forms.
func normalizeAmharicPunctuation(s string) string {
	s = strings.ReplaceAll(s, "።", ".") // Ethiopic full stop
	s = strings.ReplaceAll(s, "፤", ";") // Ethiopic semicolon
	s = strings.ReplaceAll(s, "፣", ",") // Ethiopic comma
	s = strings.ReplaceAll(s, "፡", " ") // Ethiopic word space
	return s
}

// removeThaiToneMarks strips Thai tone marks and vowel signs that can vary.
// These are combining characters that can make matching more flexible.
func removeThaiToneMarks(s string) string {
	toneMarks := runes.Predicate(func(r rune) bool {
		// Thai tone marks and some vowel signs (U+0E34 to U+0E3A, U+0E47 to U+0E4E)
		return (r >= 0x0E34 && r <= 0x0E3A) || (r >= 0x0E47 && r <= 0x0E4E)
	})
	t := runes.Remove(toneMarks)
	if result, _, err := transform.String(t, s); err == nil {
		return result
	}
	return s
}

// normalizeLatinExtended handles special cases for Latin-based scripts with unique requirements.
// This includes Vietnamese (tone diacritics) and Turkish (dotted/dotless I).
// Note: Case folding happens later in the pipeline (final slugification stage).
func normalizeLatinExtended(s string, preserveVietnamese bool) string {
	// Vietnamese: optionally preserve tone diacritics for dual-slug generation
	if !preserveVietnamese {
		s = removeDiacritics(s)
	}

	return s
}

// containsTurkishChars checks if the string contains Turkish-specific characters.
func containsTurkishChars(s string) bool {
	for _, r := range s {
		// Turkish-specific: dotless i (ı), capital dotted I (İ), ğ, ş, ç
		if r == 'ı' || r == 'İ' || r == 'ğ' || r == 'Ğ' || r == 'ş' || r == 'Ş' {
			return true
		}
	}
	return false
}

// normalizeByScript applies script-specific normalization rules.
// This is called during Stage 2 of the slugification pipeline.
func normalizeByScript(s string, script ScriptType) string {
	switch script {
	case ScriptLatin:
		// Latin: NFKC + diacritic removal
		s = norm.NFKC.String(s)
		s = normalizeLatinExtended(s, false)

	case ScriptCJK:
		// CJK: NFC only (preserve characters, avoid NFKC which mangles katakana)
		s = norm.NFC.String(s)

	case ScriptCyrillic:
		// Cyrillic: NFKC + diacritic removal (ё→е)
		s = norm.NFKC.String(s)
		s = removeDiacritics(s)

	case ScriptGreek:
		// Greek: NFKC + diacritic removal + punctuation normalization
		s = norm.NFKC.String(s)
		s = removeDiacritics(s)
		s = normalizeGreekPunctuation(s)

	case ScriptIndic:
		// Indic scripts: NFKC but PRESERVE vowel marks (matras are essential)
		s = norm.NFKC.String(s)
		// Note: Do NOT remove diacritics - vowel signs are fundamental to the script

	case ScriptArabic:
		// Arabic: NFKC + remove vowel marks + punctuation normalization
		s = norm.NFKC.String(s)
		s = removeArabicVowelMarks(s)
		s = normalizeArabicPunctuation(s)

	case ScriptHebrew:
		// Hebrew: NFKC + remove vowel marks
		s = norm.NFKC.String(s)
		s = removeHebrewVowelMarks(s)

	case ScriptThai, ScriptBurmese, ScriptKhmer, ScriptLao:
		// Southeast Asian scripts: NFKC + remove tone marks
		s = norm.NFKC.String(s)
		// For Thai, optionally remove some combining marks
		if script == ScriptThai {
			s = removeThaiToneMarks(s)
		}

	case ScriptAmharic:
		// Amharic: NFKC + punctuation normalization
		s = norm.NFKC.String(s)
		s = normalizeAmharicPunctuation(s)

	default:
		// Fallback: apply NFKC
		s = norm.NFKC.String(s)
	}

	return s
}
