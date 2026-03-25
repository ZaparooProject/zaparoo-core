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

	m = findBracketedYear("Game (1969) title")
	assert.False(t, m.ok)

	m = findBracketedYear("")
	assert.False(t, m.ok)
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
}
