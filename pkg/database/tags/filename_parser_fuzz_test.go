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
	"unicode/utf8"
)

// FuzzParseFilenameToCanonicalTags tests the main filename parsing function with random inputs
// to discover edge cases in regex patterns and state machine logic.
func FuzzParseFilenameToCanonicalTags(f *testing.F) {
	// Real ROM filename patterns (No-Intro/TOSEC conventions)
	f.Add("Super Mario Bros. (USA).nes")
	f.Add("Sonic the Hedgehog (Europe) (En,Fr,De,Es,It).md")
	f.Add("Legend of Zelda, The (USA) (Rev 1).nes")
	f.Add("Final Fantasy III (USA) (v1.1).sfc")
	f.Add("Pokemon Red (USA) [!].gb")
	f.Add("Game (Japan) (Disc 1 of 2).bin")
	f.Add("ROM (World) (T+Eng v1.0).rom")
	f.Add("Game [T-Chi(Traditional)Big5_100_Kuyagi].sfc")

	// Edge cases - deeply nested brackets
	f.Add("Game (((((((((())))))))))")
	f.Add("Game [[[[[[[[[]]]]]]]]")
	f.Add("Game {{{{{{{{}}}}}}}}")
	f.Add("Game <<<<<<<<>>>>>>>>")

	// Mixed unbalanced brackets
	f.Add("Game [USA (En {Beta")
	f.Add("Game (USA] [Europe)")
	f.Add("Game ({[<nested>]})")

	// Very long version strings (ReDoS potential)
	f.Add("Game (v1.2.3.4.5.6.7.8.9.10.11.12)")
	f.Add("Game (Rev AAAAAAAAAAAAAAAAAAAAAA)")

	// Complex translation patterns
	f.Add("[T+ChiEnglish(Big5)100_Kuyagi]")
	f.Add("T+Esp v1.0 T+Fra v2.0")
	f.Add("[TChi]")

	// Unicode in tags
	f.Add("Game (日本)") //nolint:gosmopolitan // testing Unicode
	f.Add("Game [Россия]")
	f.Add("ゲーム (日本) [!]") //nolint:gosmopolitan // testing Unicode
	f.Add("Game (café) (naïve) (Zürich)")
	f.Add("Super Mario Bros. (USA) 🎮")
	f.Add("ポケットモンスター 赤 (Japan).gb") //nolint:gosmopolitan // testing Unicode

	// Control characters
	f.Add("Game\x00(USA)")
	f.Add("Game\t(USA)\n[!]")
	f.Add("Game\r\n(USA)")

	// Empty and whitespace
	f.Add("")
	f.Add("   ")
	f.Add("()")
	f.Add("[]")
	f.Add("()()()")

	// Scene release patterns
	f.Add("The.Dark.Knight.2008.1080p.BluRay.x264-GROUP")
	f.Add("Movie.Title.2010.WEB-DL.DD5.1.H264")
	f.Add("Show.S01E01.PROPER.720p.HDTV")

	// Edge case filenames
	f.Add("1942 (USA).nes") // Year that looks like a year tag
	f.Add("2048 (World).rom")
	f.Add(".hidden (USA)")
	f.Add("Game....multiple...dots (USA)")

	f.Fuzz(func(t *testing.T, filename string) {
		result := ParseFilenameToCanonicalTags(filename)

		for _, tag := range result {
			if tag.Type == "" {
				t.Errorf("Tag with empty type from filename: %q", filename)
			}
		}

		for _, tag := range result {
			if !utf8.ValidString(string(tag.Value)) {
				t.Errorf("Tag with invalid UTF-8 value %q from filename: %q", tag.Value, filename)
			}
		}

		// Deterministic - same input always produces same result
		result2 := ParseFilenameToCanonicalTags(filename)
		if len(result) != len(result2) {
			t.Errorf("Non-deterministic result count: %d vs %d for filename: %q",
				len(result), len(result2), filename)
		}

		if len(result) > 100 {
			t.Errorf("Unreasonable number of tags (%d) from filename: %q", len(result), filename)
		}
	})
}

// FuzzExtractTags tests the bracket extraction state machine with random inputs.
func FuzzExtractTags(f *testing.F) {
	// Balanced brackets
	f.Add("Game (USA) [!]")
	f.Add("Title {info} <meta>")
	f.Add("(a)(b)(c)[x][y][z]")

	// Deeply nested (same type)
	f.Add("((((((((((()))))))))))")
	f.Add("[[[[[[[[[[]]]]]]]]]]")

	// Unbalanced
	f.Add("(((((")
	f.Add("]]]]]")
	f.Add("(a[b)c]")
	f.Add("[a(b]c)")

	// Mixed types
	f.Add("([{<>}])")
	f.Add("(<[{nested}]>)")

	// Edge cases
	f.Add("")
	f.Add("no brackets at all")
	f.Add("((()))")
	f.Add("[]{}()<>")

	// Unicode inside brackets
	f.Add("(日本)") //nolint:gosmopolitan // testing Unicode
	f.Add("[中文]") //nolint:gosmopolitan // testing Unicode

	// Control characters
	f.Add("(\x00)")
	f.Add("[\t\n]")

	f.Fuzz(func(t *testing.T, filename string) {
		// Call the function - should never panic
		parenTags, bracketTags := extractTags(filename)

		if parenTags == nil || bracketTags == nil {
			t.Error("extractTags returned nil slices")
		}
		_ = parenTags
		_ = bracketTags

		// Total characters in extracted tags should not exceed input length
		totalLen := 0
		for _, tag := range parenTags {
			totalLen += len(tag)
		}
		for _, tag := range bracketTags {
			totalLen += len(tag)
		}
		if totalLen > len(filename) {
			t.Errorf("Extracted more characters (%d) than input length (%d) for: %q",
				totalLen, len(filename), filename)
		}

		parenTags2, bracketTags2 := extractTags(filename)
		if len(parenTags) != len(parenTags2) || len(bracketTags) != len(bracketTags2) {
			t.Errorf("Non-deterministic result for filename: %q", filename)
		}
	})
}

// FuzzParseTitleFromFilename tests title extraction with random inputs.
func FuzzParseTitleFromFilename(f *testing.F) {
	// ROM filenames
	f.Add("Super Mario Bros. (USA).nes", false)
	f.Add("Sonic the Hedgehog (Europe).md", false)
	f.Add("1942 (USA).nes", false)     // Year-like title
	f.Add("1. Game Title (USA)", true) // Leading number
	f.Add("01 - Song Name.mp3", true)  // Track number

	// Scene releases
	f.Add("The.Dark.Knight.2008.1080p.BluRay.x264-GROUP.mkv", false)
	f.Add("Movie.Title.2010.WEB-DL.DD5.1.H264.mp4", false)
	f.Add("Show.S01E01.PROPER.720p.HDTV.avi", false)

	// Edge cases
	f.Add("", false)
	f.Add("   ", false)
	f.Add("()", false)
	f.Add(".....", false)
	f.Add("_____", false)
	f.Add("-----", false)

	// Unicode
	f.Add("日本ゲーム (Japan).rom", false) //nolint:gosmopolitan // testing Unicode
	f.Add("Spëcîål Chàrs (World)", false)

	// Very long filename
	f.Add(strings.Repeat("A", 500)+"(USA).rom", false)

	// Control characters
	f.Add("Game\x00Name (USA)", false)
	f.Add("Game\tName\n(USA)", false)

	// Multiple extensions
	f.Add("File.rom.backup.zip", false)
	f.Add("Game..zip", false)

	f.Fuzz(func(t *testing.T, filename string, stripLeadingNumbers bool) {
		result := ParseTitleFromFilename(filename, stripLeadingNumbers)

		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in result: %q from filename: %q", result, filename)
		}

		// Invalid UTF-8 bytes get replaced with 3-byte replacement chars,
		// so output can be up to 3x input plus some overhead.
		if len(result) > len(filename)*3+10 {
			t.Errorf("Result unexpectedly longer: input=%d, output=%d for filename: %q",
				len(filename), len(result), filename)
		}

		if strings.Contains(result, "  ") {
			t.Errorf("Result contains multiple consecutive spaces: %q from filename: %q",
				result, filename)
		}

		if result != strings.TrimSpace(result) {
			t.Errorf("Result has leading/trailing whitespace: %q from filename: %q",
				result, filename)
		}

		result2 := ParseTitleFromFilename(filename, stripLeadingNumbers)
		if result != result2 {
			t.Errorf("Non-deterministic result for filename: %q", filename)
		}
	})
}

// FuzzExtractSpecialPatterns tests special pattern extraction (disc, rev, version, year, etc.)
func FuzzExtractSpecialPatterns(f *testing.F) {
	// Disc patterns
	f.Add("Game (Disc 1 of 2)")
	f.Add("Game (Disc 99 of 100)")

	// Revision patterns
	f.Add("Game (Rev A)")
	f.Add("Game (Rev-123)")
	f.Add("Game (Rev ZZZZZZZZZZZ)")

	// Version patterns
	f.Add("Game (v1.0)")
	f.Add("Game (v1.2.3.4.5.6.7.8.9.10)")
	f.Add("Game v1.0 outside brackets")

	// Year patterns
	f.Add("Game (1999)")
	f.Add("Game (2024)")

	// Translation patterns
	f.Add("T+Eng v1.0")
	f.Add("[T+Chi]")
	f.Add("[T-Spa v2.0]")

	// Edge cases
	f.Add("")
	f.Add("No special patterns here")
	f.Add("()()()")
	f.Add("(v) (Rev) (Disc)") // Incomplete patterns

	f.Fuzz(func(t *testing.T, filename string) {
		tags, remaining := extractSpecialPatterns(filename)

		if !utf8.ValidString(remaining) {
			t.Errorf("Invalid UTF-8 in remaining: %q from filename: %q", remaining, filename)
		}

		for _, tag := range tags {
			if tag.Type == "" {
				t.Errorf("Tag with empty type from filename: %q", filename)
			}
			if !utf8.ValidString(string(tag.Value)) {
				t.Errorf("Invalid UTF-8 in tag value from filename: %q", filename)
			}
		}

		if len(remaining) > len(filename) {
			t.Errorf("Remaining longer than input: %d > %d for filename: %q",
				len(remaining), len(filename), filename)
		}

		tags2, remaining2 := extractSpecialPatterns(filename)
		if len(tags) != len(tags2) || remaining != remaining2 {
			t.Errorf("Non-deterministic result for filename: %q", filename)
		}
	})
}
