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
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCollapseSpaces(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello world", collapseSpaces("hello world"))
	assert.Equal(t, "hello world", collapseSpaces("hello  world"))
	assert.Equal(t, "hello world", collapseSpaces("  hello   world  "))
	assert.Equal(t, "a b", collapseSpaces("a\t\nb"))
	assert.Empty(t, collapseSpaces(""))
	assert.Empty(t, collapseSpaces("   "))
}

func TestStripLeadingNumberPrefix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Song Title", stripLeadingNumberPrefix("01 - Song Title"))
	assert.Equal(t, "Game Name", stripLeadingNumberPrefix("1. Game Name"))
	assert.Equal(t, "track", stripLeadingNumberPrefix("001-track"))
	assert.Equal(t, "rest", stripLeadingNumberPrefix("42  rest"))
	assert.Equal(t, "nodigits", stripLeadingNumberPrefix("nodigits"))
	assert.Equal(t, "123", stripLeadingNumberPrefix("123"))
	assert.Equal(t, "123abc", stripLeadingNumberPrefix("123abc"))
	assert.Empty(t, stripLeadingNumberPrefix(""))
}

func TestStartsWithYear(t *testing.T) {
	t.Parallel()
	assert.True(t, startsWithYear("1985.mp3"))
	assert.True(t, startsWithYear("2024 title"))
	assert.True(t, startsWithYear("1900 old"))
	assert.False(t, startsWithYear("0999 nope"))
	assert.False(t, startsWithYear("3000 nope"))
	assert.False(t, startsWithYear("abc"))
	assert.False(t, startsWithYear("19"))
	assert.False(t, startsWithYear(""))
}

func TestIsYearValue(t *testing.T) {
	t.Parallel()
	assert.True(t, isYearValue("1970"))
	assert.True(t, isYearValue("1999"))
	assert.True(t, isYearValue("2000"))
	assert.True(t, isYearValue("2099"))
	assert.False(t, isYearValue("1969"))
	assert.False(t, isYearValue("2100"))
	assert.False(t, isYearValue("abcd"))
	assert.False(t, isYearValue("19"))
	assert.False(t, isYearValue(""))
}

func TestParseBuildDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		// arcade YYMMDD, century pivot at 70
		{in: "931005", want: "1993-10-05", ok: true},
		{in: "961004", want: "1996-10-04", ok: true},
		{in: "040202", want: "2004-02-02", ok: true},
		{in: "000101", want: "2000-01-01", ok: true},
		{in: "691231", want: "2069-12-31", ok: true},
		{in: "700101", want: "1970-01-01", ok: true},
		// YYYYMMDD
		{in: "19920921", want: "1992-09-21", ok: true},
		// YYYY-MM-DD passthrough
		{in: "1992-09-21", want: "1992-09-21", ok: true},
		// invalid month/day
		{in: "999999", want: "", ok: false},
		{in: "931305", want: "", ok: false}, // month 13
		{in: "930032", want: "", ok: false}, // day 32
		{in: "1992-13-01", want: "", ok: false},
		// wrong shapes
		{in: "12345", want: "", ok: false},   // 5 digits
		{in: "1234567", want: "", ok: false}, // 7 digits
		{in: "world", want: "", ok: false},
		{in: "world-931005", want: "", ok: false},
		{in: "", want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := parseBuildDate(tt.in)
		assert.Equal(t, tt.ok, ok, "ok mismatch for %q", tt.in)
		assert.Equal(t, tt.want, got, "value mismatch for %q", tt.in)
	}
}

func TestFindBracketedYear(t *testing.T) {
	t.Parallel()

	m := findBracketedYear("Game (1995) title")
	assert.True(t, m.ok)
	assert.Equal(t, "1995", "Game (1995) title"[m.cap1s:m.cap1e])
	assert.Equal(t, 6, m.end-m.start)

	m = findBracketedYear("Movie (2024).mkv")
	assert.True(t, m.ok)
	assert.Equal(t, "2024", "Movie (2024).mkv"[m.cap1s:m.cap1e])

	m = findBracketedYear("Game (USA) title")
	assert.False(t, m.ok)

	// 1969 is now in range for bracketed years (1950-2099 covers early computing era)
	m = findBracketedYear("Game (1969) title")
	assert.True(t, m.ok)
	assert.Equal(t, "1969", "Game (1969) title"[m.cap1s:m.cap1e])

	m = findBracketedYear("Game (1950) title")
	assert.True(t, m.ok)

	m = findBracketedYear("Game (1949) title")
	assert.False(t, m.ok)

	m = findBracketedYear("")
	assert.False(t, m.ok)
}

func TestIsBracketedYearValue(t *testing.T) {
	t.Parallel()
	assert.True(t, isBracketedYearValue("1950"))
	assert.True(t, isBracketedYearValue("1969"))
	assert.True(t, isBracketedYearValue("1970"))
	assert.True(t, isBracketedYearValue("1999"))
	assert.True(t, isBracketedYearValue("2000"))
	assert.True(t, isBracketedYearValue("2099"))
	assert.False(t, isBracketedYearValue("1949"))
	assert.False(t, isBracketedYearValue("2100"))
	assert.False(t, isBracketedYearValue("abcd"))
	assert.False(t, isBracketedYearValue(""))
}

func TestFindDiscPattern(t *testing.T) {
	t.Parallel()

	m := findDiscPattern("Game (Disc 1 of 2)")
	assert.True(t, m.ok)
	assert.Equal(t, "1", "Game (Disc 1 of 2)"[m.cap1s:m.cap1e])
	assert.Equal(t, "2", "Game (Disc 1 of 2)"[m.cap2s:m.cap2e])

	m = findDiscPattern("Game (disc 1 of 3)")
	assert.True(t, m.ok)

	m = findDiscPattern("Game (DISC 2 OF 4)")
	assert.True(t, m.ok)
	assert.Equal(t, "2", "Game (DISC 2 OF 4)"[m.cap1s:m.cap1e])
	assert.Equal(t, "4", "Game (DISC 2 OF 4)"[m.cap2s:m.cap2e])

	assert.False(t, findDiscPattern("Game (USA)").ok)
	assert.False(t, findDiscPattern("Game (Disc 1of 2)").ok)
	assert.False(t, findDiscPattern("Game (Disc 1 of2)").ok)
	assert.False(t, findDiscPattern("Game (Disc 1 of 2").ok)
	assert.False(t, findDiscPattern("").ok)

	// "Disk" (with k) accepted as synonym.
	m = findDiscPattern("Game (Disk 1 of 3)")
	assert.True(t, m.ok)
	assert.Equal(t, "1", "Game (Disk 1 of 3)"[m.cap1s:m.cap1e])
	assert.Equal(t, "3", "Game (Disk 1 of 3)"[m.cap2s:m.cap2e])
	assert.Equal(t, byte(0), m.side)

	// Side suffix — letter.
	m = findDiscPattern("Game (Disc 1 of 3 Side A)")
	assert.True(t, m.ok)
	assert.Equal(t, byte('A'), m.side)

	m = findDiscPattern("Game (Disk 2 of 2 Side B)")
	assert.True(t, m.ok)
	assert.Equal(t, byte('B'), m.side)

	// Side suffix — numeric alias.
	m = findDiscPattern("Game (Disk 1 of 2 Side 1)")
	assert.True(t, m.ok)
	assert.Equal(t, byte('A'), m.side)

	// Invalid side letter — no match.
	assert.False(t, findDiscPattern("Game (Disk 1 of 3 Side Z)").ok)

	// "Disco" must not match (extra letter after prefix).
	assert.False(t, findDiscPattern("Game (Disco 1 of 2)").ok)
}

func TestFindRevPattern(t *testing.T) {
	t.Parallel()
	input := "Game (Rev A)"
	m := findRevPattern(input)
	assert.True(t, m.ok)
	assert.Equal(t, "A", input[m.cap1s:m.cap1e])

	m = findRevPattern("Game (Rev-B)")
	assert.True(t, m.ok)

	m = findRevPattern("Game (rev a)")
	assert.True(t, m.ok)

	input = "Game (Review Copy) (Rev B)"
	m = findRevPattern(input)
	assert.True(t, m.ok)
	assert.Equal(t, "B", input[m.cap1s:m.cap1e])

	assert.False(t, findRevPattern("Game (Review)").ok)
	assert.False(t, findRevPattern("Game (RevA)").ok)
	assert.False(t, findRevPattern("Game (Rev )").ok)
	assert.False(t, findRevPattern("").ok)
}

func TestFindBracketedVersion(t *testing.T) {
	t.Parallel()
	input := "Game (v1.2.3)"
	m := findBracketedVersion(input)
	assert.True(t, m.ok)
	assert.Equal(t, "1.2.3", input[m.cap1s:m.cap1e])

	m = findBracketedVersion("Game (v1)")
	assert.True(t, m.ok)

	m = findBracketedVersion("Game (V2)")
	assert.True(t, m.ok)

	assert.False(t, findBracketedVersion("Game (v)").ok)
	assert.False(t, findBracketedVersion("Game (v1.2").ok)
	assert.False(t, findBracketedVersion("Game (v1.2 beta)").ok)
	assert.False(t, findBracketedVersion("").ok)
}

func TestFindBracketlessVersion(t *testing.T) {
	t.Parallel()
	input := "game v1.2.3 stuff"
	m := findBracketlessVersion(input)
	assert.True(t, m.ok)
	assert.Equal(t, "1.2.3", input[m.cap1s:m.cap1e])

	m = findBracketlessVersion("v2 game")
	assert.True(t, m.ok)

	assert.False(t, findBracketlessVersion("game_v1.2").ok)
	assert.False(t, findBracketlessVersion("review something").ok)
	assert.False(t, findBracketlessVersion("game v").ok)
	assert.False(t, findBracketlessVersion("evolve something").ok)
	assert.False(t, findBracketlessVersion("").ok)
}

func TestFindVolumeNumber(t *testing.T) {
	t.Parallel()
	v, ok := findVolumeNumber("Book (Vol. 2)")
	assert.True(t, ok)
	assert.Equal(t, "2", v)

	v, ok = findVolumeNumber("Series (Volume 10)")
	assert.True(t, ok)
	assert.Equal(t, "10", v)

	v, ok = findVolumeNumber("Book (Vol.4)")
	assert.True(t, ok)
	assert.Equal(t, "4", v)

	_, ok = findVolumeNumber("Book (Volumes)")
	assert.False(t, ok)

	_, ok = findVolumeNumber("Book (Vol. )")
	assert.False(t, ok)

	_, ok = findVolumeNumber("")
	assert.False(t, ok)
}

func TestRemoveVolumeNumber(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Book   Title", removeVolumeNumber("Book (Vol. 2) Title"))
	assert.Equal(t, "No volume here", removeVolumeNumber("No volume here"))
	assert.Equal(t, "Book Title", removeVolumeNumber("Book(Vol. 1)Title"))
}

func TestFindSeasonEpisode(t *testing.T) {
	t.Parallel()
	s, e, ok := findSeasonEpisode("Show.S01E02.title")
	assert.True(t, ok)
	assert.Equal(t, "01", s)
	assert.Equal(t, "02", e)

	s, e, ok = findSeasonEpisode("s02e15")
	assert.True(t, ok)
	assert.Equal(t, "02", s)
	assert.Equal(t, "15", e)

	s, e, ok = findSeasonEpisode("S1E100")
	assert.True(t, ok)
	assert.Equal(t, "1", s)
	assert.Equal(t, "100", e)

	_, _, ok = findSeasonEpisode("Season 1 Episode 2")
	assert.False(t, ok)

	_, _, ok = findSeasonEpisode("")
	assert.False(t, ok)
}

func TestRemoveSeasonEpisode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Show   Title", removeSeasonEpisode("Show S01E02 Title"))
	assert.Equal(t, "No episodes", removeSeasonEpisode("No episodes"))
	assert.Equal(t, "Show Title", removeSeasonEpisode("ShowS01E02Title"))
}

func TestFindUnbracketedYear(t *testing.T) {
	t.Parallel()
	input := "Movie 2018 720p"
	m := findUnbracketedYear(input)
	assert.True(t, m.ok)
	assert.Equal(t, "2018", input[m.cap1s:m.cap1e])

	m = findUnbracketedYear("1995 title")
	assert.True(t, m.ok)

	m = findUnbracketedYear("title 2020")
	assert.True(t, m.ok)

	assert.False(t, findUnbracketedYear("abc2018def").ok)
	assert.False(t, findUnbracketedYear("title_2018_cut").ok)
	assert.False(t, findUnbracketedYear("title 1969 stuff").ok)
	assert.False(t, findUnbracketedYear("title 1234 stuff").ok)
	assert.False(t, findUnbracketedYear("").ok)
}

func TestWordBoundary(t *testing.T) {
	t.Parallel()
	s := "hello world"
	assert.True(t, isWordBoundaryBefore(s, 0))
	assert.False(t, isWordBoundaryBefore(s, 1))
	assert.True(t, isWordBoundaryBefore(s, 6))
	assert.True(t, isWordBoundaryAfter(s, 5))
	assert.False(t, isWordBoundaryAfter(s, 4))
	assert.True(t, isWordBoundaryAfter(s, len(s)))

	s = "hello_world"
	assert.False(t, isWordBoundaryBefore(s, 6))
	assert.False(t, isWordBoundaryAfter(s, 5))
}

// transOracleRegex is the regex that findBracketlessTranslation replaced,
// kept here as a behavioral oracle.
var transOracleRegex = regexp.MustCompile(
	`(^|\s)(T)([+-])([A-Za-z]{2,3})(?:\s+v(\d+(?:\.\d+)*))?(?:\s|[.]|$)`,
)

func TestFindBracketlessTranslation_RegexEquivalence(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"",
		"Final Fantasy V T+Eng",
		"T+Eng",
		"T-Ger",
		"Game T-Ger v1.0",
		"Game T+Spa v2.1.3 extra",
		"T+Eng v1.2x",
		"T+Engl",
		"FTL Faster Than Light",
		"The Legend of Zelda",
		"T+E",
		"AT+Eng",
		" T+Eng.",
		"T+eng v1.",
		"Game T+Fra version",
		"T-Chi v12.34.56",
		"T+Eng v1..2",
		"x T+Xxx T+Eng ",
		"Mother 3 (Japan) T+Eng v1.3",
		"T+Eng  v2",
		"T+Eng v",
		"T+Eng vx1",
		"T*Eng",
		"T+En.",
		"T+EngT+Fra",
		"Tales of Phantasia T-Eng.sfc",
		"T+Por v1.0 T+Eng",
		"\tT+Eng\t",
		"T+ABC v007.0042",
		"T+ab",
		"game.T+Eng.rom",
	}
	for _, in := range inputs {
		got := findBracketlessTranslation(in)
		indices := transOracleRegex.FindStringSubmatchIndex(in)
		if indices == nil {
			assert.False(t, got.ok, "input %q: regex found no match but parser did", in)
			continue
		}
		if !assert.True(t, got.ok, "input %q: regex matched but parser did not", in) {
			continue
		}
		assert.Equal(t, indices[0], got.start, "input %q: start", in)
		assert.Equal(t, indices[1], got.end, "input %q: end", in)
		assert.Equal(t, in[indices[6]], got.plusMinus, "input %q: plusMinus", in)
		assert.Equal(t, indices[8], got.langS, "input %q: langS", in)
		assert.Equal(t, indices[9], got.langE, "input %q: langE", in)
		assert.Equal(t, indices[10], got.verS, "input %q: verS", in)
		assert.Equal(t, indices[11], got.verE, "input %q: verE", in)
	}
}

// editionWordOracleRegex is the regex that findEditionWord replaced, kept here
// as a behavioral oracle.
var editionWordOracleRegex = regexp.MustCompile(
	`(?i)\s+(version|edition|ausgabe|versione|edizione|versao|edicao|バージョン|エディション|ヴァージョン)(\s*[\(\[{<]|\s*$)`,
)

func TestFindEditionWord_RegexEquivalence(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"",
		"Pokemon Red Version",
		"Pokemon Red Version (USA)",
		"Game Edition",
		"Special Edition (Rev 1)",
		"Game VERSIONE (It)",
		"versione italiana",
		"Game Version2",
		"Game Versions (USA)",
		"ゲーム バージョン (Japan)",
		"Game エディション",
		"Game edicao",
		"Game Editions",
		"Game Ausgabe  [b]",
		"Game version  ",
		" edition",
		"edition",
		"Game Edizione <jp>",
		"Game eDiTiOn {x}",
		"Game ヴァージョン",
		"Game versao(br)",
		"Game version edition",
		"Limited Edition Version (USA)",
		"Game\tEdition",
		"Game Editionversion",
	}
	for _, in := range inputs {
		gotWord, gotOK := findEditionWord(in)
		indices := editionWordOracleRegex.FindStringSubmatchIndex(in)
		if indices == nil {
			assert.False(t, gotOK, "input %q: regex found no match but parser did", in)
			continue
		}
		if !assert.True(t, gotOK, "input %q: regex matched but parser did not", in) {
			continue
		}
		wantWord := strings.ToLower(in[indices[2]:indices[3]])
		assert.Equal(t, wantWord, gotWord, "input %q: word", in)
	}
}

// FuzzFindBracketlessTranslationEquivalence cross-checks the manual parser
// against the regex it replaced.
func FuzzFindBracketlessTranslationEquivalence(f *testing.F) {
	seeds := []string{
		"Final Fantasy V T+Eng", "T+Eng v1.2x", "T+Engl", " T+Eng.",
		"T-Chi v12.34.56", "x T+Xxx T+Eng ", "game.T+Eng.rom", "T+ABC v007.0042",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := findBracketlessTranslation(in)
		indices := transOracleRegex.FindStringSubmatchIndex(in)
		if indices == nil {
			if got.ok {
				t.Fatalf("input %q: regex found no match but parser matched %+v", in, got)
			}
			return
		}
		if !got.ok {
			t.Fatalf("input %q: regex matched %v but parser did not", in, indices)
		}
		if got.start != indices[0] || got.end != indices[1] ||
			got.langS != indices[8] || got.langE != indices[9] ||
			got.verS != indices[10] || got.verE != indices[11] ||
			got.plusMinus != in[indices[6]] {
			t.Fatalf("input %q: parser %+v != regex %v", in, got, indices)
		}
	})
}

// FuzzFindEditionWordEquivalence cross-checks the manual parser against the
// regex it replaced.
func FuzzFindEditionWordEquivalence(f *testing.F) {
	seeds := []string{
		"Pokemon Red Version (USA)", "Game eDiTiOn {x}", "ゲーム バージョン (Japan)",
		"Game versao(br)", "Game version edition", "Game Editionversion",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		gotWord, gotOK := findEditionWord(in)
		indices := editionWordOracleRegex.FindStringSubmatchIndex(in)
		if indices == nil {
			if gotOK {
				t.Fatalf("input %q: regex found no match but parser matched %q", in, gotWord)
			}
			return
		}
		if !gotOK {
			t.Fatalf("input %q: regex matched but parser did not", in)
		}
		wantWord := strings.ToLower(in[indices[2]:indices[3]])
		if gotWord != wantWord {
			t.Fatalf("input %q: parser word %q != regex word %q", in, gotWord, wantWord)
		}
	})
}
