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

// Allocation-free replacements for regex patterns used in the hot path of
// filename parsing during media indexing. Each function documents the regex
// it replaces and is tested against the same inputs.

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isAlphanumeric(b byte) bool {
	return isDigit(b) || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// isWordBoundary checks if the position i in s is at a word boundary.
// A word boundary exists between a word char and a non-word char (or start/end).
func isWordBoundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	return !isAlphanumeric(s[i-1])
}

func isWordBoundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	return !isAlphanumeric(s[i])
}

// collapseSpaces replaces runs of whitespace with a single space.
// Also trims leading/trailing whitespace (unlike the regex it replaces).
// Callers always call strings.TrimSpace afterward, so this is equivalent.
// Replaces: reMultiSpace = regexp.MustCompile(`\s+`)
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stripLeadingNumberPrefix removes a leading number followed by separators.
// Replaces: reLeadingNum = regexp.MustCompile(`^\d+[.\s\-]+`)
// Used as: reLeadingNum.ReplaceAllString(title, "")
func stripLeadingNumberPrefix(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == 0 {
		return s
	}
	j := i
	for j < len(s) && (s[j] == '.' || s[j] == '-' || isWhitespace(s[j])) {
		j++
	}
	if j == i {
		return s
	}
	return s[j:]
}

// startsWithYear checks if s starts with a 4-digit year (19XX or 20XX).
// Replaces: reYear4Digit = regexp.MustCompile(`^(19\d{2}|20\d{2})`)
// Used as: reYear4Digit.MatchString(remaining)
func startsWithYear(s string) bool {
	if len(s) < 4 || !isDigit(s[2]) || !isDigit(s[3]) {
		return false
	}
	return (s[0] == '1' && s[1] == '9') || (s[0] == '2' && s[1] == '0')
}

// isYearValue checks if a 4-character string is a valid year in range 1970-2099.
func isYearValue(s string) bool {
	if len(s) != 4 || !isDigit(s[0]) || !isDigit(s[1]) || !isDigit(s[2]) || !isDigit(s[3]) {
		return false
	}
	if s[0] == '1' && s[1] == '9' && (s[2] >= '7') {
		return true
	}
	return s[0] == '2' && s[1] == '0'
}

// isBracketedYearValue checks if a 4-character string is a valid year in range 1950-2099.
// Used for bracketed year detection in TOSEC filenames where early computing-era dates
// (1950s–1960s) appear, unlike the stricter 1970+ bound used for unbracketed scene years.
func isBracketedYearValue(s string) bool {
	if len(s) != 4 || !isDigit(s[0]) || !isDigit(s[1]) || !isDigit(s[2]) || !isDigit(s[3]) {
		return false
	}
	if s[0] == '1' && s[1] == '9' && (s[2] >= '5') { // 1950-1999
		return true
	}
	return s[0] == '2' && s[1] == '0' // 2000-2099
}

// parseMatch holds the result of a string-based pattern match, replacing []int
// from FindStringSubmatchIndex. Fields are indices into the original string.
type parseMatch struct {
	start, end   int  // full match bounds
	cap1s, cap1e int  // first capture group
	cap2s, cap2e int  // second capture group (if applicable)
	side         byte // optional side letter ('A'–'D'), 0 if absent
	ok           bool
}

// findBracketedYear finds "(YYYY)" where YYYY is 1950-2099.
// Replaces: reYear = regexp.MustCompile(`\((19[789]\d|20\d{2})\)`)
func findBracketedYear(s string) parseMatch {
	for i := 0; i <= len(s)-6; i++ {
		if s[i] == '(' && s[i+5] == ')' && isBracketedYearValue(s[i+1:i+5]) {
			return parseMatch{
				start: i, end: i + 6,
				cap1s: i + 1, cap1e: i + 5,
				ok: true,
			}
		}
	}
	return parseMatch{}
}

// findDiscPattern finds "(Disc X of Y)" or "(Disk X of Y)" case-insensitively,
// with an optional "Side A/B/C/D" (or numeric aliases 1–4) suffix before the ")".
// Replaces: reDisc = regexp.MustCompile(`(?i)\(Disc\s+(\d+)\s+of\s+(\d+)\)`)
func findDiscPattern(s string) parseMatch {
	lower := strings.ToLower(s)

	// Loop to find the next "(disc" or "(disk" with whitespace after.
	// This handles cases like "Discotheque (Disc 1 of 2)" where the first
	// "(disc" is part of the word, but a later occurrence is the pattern.
	idx := -1
	prefixLen := 0
	found := false

	searchFrom := 0
	for {
		nextDisc := strings.Index(lower[searchFrom:], "(disc")
		nextDisk := strings.Index(lower[searchFrom:], "(disk")

		// Determine which comes first (or if neither exists)
		var candidateIdx int
		var foundMatch bool
		switch {
		case nextDisc != -1 && nextDisk != -1:
			if nextDisc < nextDisk {
				candidateIdx = searchFrom + nextDisc
			} else {
				candidateIdx = searchFrom + nextDisk
			}
			foundMatch = true
		case nextDisc != -1:
			candidateIdx = searchFrom + nextDisc
			foundMatch = true
		case nextDisk != -1:
			candidateIdx = searchFrom + nextDisk
			foundMatch = true
		default:
			// No more occurrences
		}

		if !foundMatch {
			break
		}

		// Determine prefix length (5 for both "(disc" and "(disk")
		prefixLen = 5
		// Check if the char immediately after the prefix is whitespace
		pos := candidateIdx + prefixLen
		if pos < len(s) && isWhitespace(s[pos]) {
			idx = candidateIdx
			found = true
			break
		}

		// Continue searching from after this occurrence
		searchFrom = candidateIdx + 1
	}

	if !found {
		return parseMatch{}
	}

	pos := idx + prefixLen // after "(disc" or "(disk"
	// skip whitespace
	for pos < len(s) && isWhitespace(s[pos]) {
		pos++
	}
	// parse first number
	numStart := pos
	for pos < len(s) && isDigit(s[pos]) {
		pos++
	}
	if pos == numStart {
		return parseMatch{}
	}
	numEnd := pos
	// skip mandatory whitespace before "of"
	wsBeforeOf := pos
	for pos < len(s) && isWhitespace(s[pos]) {
		pos++
	}
	if pos == wsBeforeOf {
		return parseMatch{}
	}
	// expect "of" (case insensitive)
	if pos+2 > len(s) || lower[pos:pos+2] != "of" {
		return parseMatch{}
	}
	pos += 2
	// skip whitespace
	wsStart := pos
	for pos < len(s) && isWhitespace(s[pos]) {
		pos++
	}
	if pos == wsStart {
		return parseMatch{}
	}
	// parse second number
	num2Start := pos
	for pos < len(s) && isDigit(s[pos]) {
		pos++
	}
	if pos == num2Start {
		return parseMatch{}
	}
	num2End := pos
	// Optional "Side X" suffix — whitespace + "side" + whitespace + letter/digit.
	var side byte
	if pos < len(s) && isWhitespace(s[pos]) {
		tmp := pos
		for tmp < len(s) && isWhitespace(s[tmp]) {
			tmp++
		}
		if tmp+4 <= len(s) && lower[tmp:tmp+4] == "side" {
			tmp += 4
			for tmp < len(s) && isWhitespace(s[tmp]) {
				tmp++
			}
			if tmp < len(s) {
				switch lower[tmp] {
				case 'a', '1':
					side = 'A'
					pos = tmp + 1
				case 'b', '2':
					side = 'B'
					pos = tmp + 1
				case 'c', '3':
					side = 'C'
					pos = tmp + 1
				case 'd', '4':
					side = 'D'
					pos = tmp + 1
				}
			}
		}
	}
	// expect ")"
	if pos >= len(s) || s[pos] != ')' {
		return parseMatch{}
	}
	return parseMatch{
		start: idx, end: pos + 1,
		cap1s: numStart, cap1e: numEnd,
		cap2s: num2Start, cap2e: num2End,
		side: side,
		ok:   true,
	}
}

// findRevPattern finds "(Rev X)" or "(Rev-X)" case-insensitively.
// Replaces: reRev = regexp.MustCompile(`(?i)\(Rev[\s-]([A-Z0-9]+)\)`)
func findRevPattern(s string) parseMatch {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "(rev")
	if idx == -1 || idx+4 >= len(s) {
		return parseMatch{}
	}
	pos := idx + 4
	// expect whitespace or '-'
	if !isWhitespace(s[pos]) && s[pos] != '-' {
		return parseMatch{}
	}
	pos++
	// parse alphanumeric value
	valStart := pos
	for pos < len(s) && isAlphanumeric(s[pos]) {
		pos++
	}
	if pos == valStart {
		return parseMatch{}
	}
	// expect ")"
	if pos >= len(s) || s[pos] != ')' {
		return parseMatch{}
	}
	return parseMatch{
		start: idx, end: pos + 1,
		cap1s: valStart, cap1e: pos,
		ok: true,
	}
}

// findBracketedVersion finds "(vN.N.N)" case-insensitively.
// Replaces: reVersion = regexp.MustCompile(`(?i)\(v(\d+(?:\.\d+)*)\)`)
func findBracketedVersion(s string) parseMatch {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "(v")
	for idx != -1 && idx+2 < len(s) {
		pos := idx + 2
		// parse version: digits, optionally followed by .digits repeating
		vStart := pos
		if pos < len(s) && isDigit(s[pos]) {
			for pos < len(s) && isDigit(s[pos]) {
				pos++
			}
			for pos+1 < len(s) && s[pos] == '.' && isDigit(s[pos+1]) {
				pos++ // skip dot
				for pos < len(s) && isDigit(s[pos]) {
					pos++
				}
			}
			if pos < len(s) && s[pos] == ')' {
				return parseMatch{
					start: idx, end: pos + 1,
					cap1s: vStart, cap1e: pos,
					ok: true,
				}
			}
		}
		// try next occurrence
		next := strings.Index(lower[idx+1:], "(v")
		if next == -1 {
			break
		}
		idx = idx + 1 + next
	}
	return parseMatch{}
}

// findBracketlessVersion finds "v" at word boundary followed by digit.digit pattern.
// Replaces: reBracketlessVersion = regexp.MustCompile(`\bv(\d+(?:\.\d+)*)`)
func findBracketlessVersion(s string) parseMatch {
	for i := range len(s) {
		if (s[i] != 'v' && s[i] != 'V') || !isWordBoundaryBefore(s, i) {
			continue
		}
		pos := i + 1
		if pos >= len(s) || !isDigit(s[pos]) {
			continue
		}
		vStart := pos
		for pos < len(s) && isDigit(s[pos]) {
			pos++
		}
		for pos+1 < len(s) && s[pos] == '.' && isDigit(s[pos+1]) {
			pos++
			for pos < len(s) && isDigit(s[pos]) {
				pos++
			}
		}
		return parseMatch{
			start: i, end: pos,
			cap1s: vStart, cap1e: pos,
			ok: true,
		}
	}
	return parseMatch{}
}

// findVolumeNumber finds "(Vol. N)" or "(Volume N)" case-insensitively.
// Replaces: reVolumeNumber = regexp.MustCompile(`(?i)\((?:vol\.|volume)\s*(\d+)\)`)
// Returns the volume number string directly since callers need FindStringSubmatch semantics.
func findVolumeNumber(s string) (volumeNum string, ok bool) {
	lower := strings.ToLower(s)
	for _, keyword := range []string{"(vol.", "(volume"} {
		idx := strings.Index(lower, keyword)
		if idx == -1 {
			continue
		}
		pos := idx + len(keyword)
		// skip optional whitespace
		for pos < len(s) && isWhitespace(s[pos]) {
			pos++
		}
		// parse digits
		numStart := pos
		for pos < len(s) && isDigit(s[pos]) {
			pos++
		}
		if pos == numStart {
			continue
		}
		if pos < len(s) && s[pos] == ')' {
			return s[numStart:pos], true
		}
	}
	return "", false
}

// removeVolumeNumber removes all "(Vol. N)" / "(Volume N)" occurrences, replacing with space.
// Replaces: reVolumeNumber.ReplaceAllString(remaining, " ")
func removeVolumeNumber(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	i := 0
	changed := false
	for i < len(s) {
		found := false
		for _, keyword := range []string{"(vol.", "(volume"} {
			if i+len(keyword) > len(s) {
				continue
			}
			if lower[i:i+len(keyword)] != keyword {
				continue
			}
			pos := i + len(keyword)
			for pos < len(s) && isWhitespace(s[pos]) {
				pos++
			}
			numStart := pos
			for pos < len(s) && isDigit(s[pos]) {
				pos++
			}
			if pos > numStart && pos < len(s) && s[pos] == ')' {
				if !changed {
					b.Grow(len(s))
					_, _ = b.WriteString(s[:i])
					changed = true
				}
				_ = b.WriteByte(' ')
				i = pos + 1
				found = true
				break
			}
		}
		if !found {
			if changed {
				_ = b.WriteByte(s[i])
			}
			i++
		}
	}
	if !changed {
		return s
	}
	return b.String()
}

// findSeasonEpisode finds "S##E###" pattern case-insensitively.
// Replaces: reSeasonEpisode = regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,3})`)
// Returns season and episode strings.
func findSeasonEpisode(s string) (season, episode string, ok bool) {
	for i := range len(s) - 3 {
		if s[i] != 'S' && s[i] != 's' {
			continue
		}
		pos := i + 1
		// 1-2 digits for season
		dStart := pos
		for pos < len(s) && pos-dStart < 2 && isDigit(s[pos]) {
			pos++
		}
		if pos == dStart {
			continue
		}
		seasonStr := s[dStart:pos]
		// E or e
		if pos >= len(s) || (s[pos] != 'E' && s[pos] != 'e') {
			continue
		}
		pos++
		// 1-3 digits for episode
		eStart := pos
		for pos < len(s) && pos-eStart < 3 && isDigit(s[pos]) {
			pos++
		}
		if pos == eStart {
			continue
		}
		return seasonStr, s[eStart:pos], true
	}
	return "", "", false
}

// removeSeasonEpisode removes all S##E### patterns, replacing with space.
// Replaces: reSeasonEpisode.ReplaceAllString(title, " ")
func removeSeasonEpisode(s string) string {
	var b strings.Builder
	i := 0
	changed := false
	for i < len(s) {
		if (s[i] == 'S' || s[i] == 's') && i+3 < len(s) {
			pos := i + 1
			dStart := pos
			for pos < len(s) && pos-dStart < 2 && isDigit(s[pos]) {
				pos++
			}
			if pos > dStart && pos < len(s) && (s[pos] == 'E' || s[pos] == 'e') {
				pos++
				eStart := pos
				for pos < len(s) && pos-eStart < 3 && isDigit(s[pos]) {
					pos++
				}
				if pos > eStart {
					if !changed {
						b.Grow(len(s))
						_, _ = b.WriteString(s[:i])
						changed = true
					}
					_ = b.WriteByte(' ')
					i = pos
					continue
				}
			}
		}
		if changed {
			_ = b.WriteByte(s[i])
		}
		i++
	}
	if !changed {
		return s
	}
	return b.String()
}

// findUnbracketedYear finds a year (1970-2099) at word boundaries.
// Replaces: reYearScene = regexp.MustCompile(`\b(19[789]\d|20\d{2})\b`)
func findUnbracketedYear(s string) parseMatch {
	for i := 0; i <= len(s)-4; i++ {
		if !isWordBoundaryBefore(s, i) {
			continue
		}
		if !isYearValue(s[i : i+4]) {
			continue
		}
		if !isWordBoundaryAfter(s, i+4) {
			continue
		}
		return parseMatch{
			start: i, end: i + 4,
			cap1s: i, cap1e: i + 4,
			ok: true,
		}
	}
	return parseMatch{}
}
