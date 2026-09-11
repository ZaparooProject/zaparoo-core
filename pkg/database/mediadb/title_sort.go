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

package mediadb

import (
	"cmp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version collation names when comparison semantics change. SQLite persists
// collation order in indexes, so a new version must rebuild the browse index.
const (
	browseTitleCollationName     = "ZAPAROO_TITLE_V1"
	browseDirectoryCollationName = "ZAPAROO_DIRECTORY_V1"
)

type browseTitleTokenKind uint8

type browseTitleToken struct {
	value string
	next  int
	kind  browseTitleTokenKind
}

const (
	browseTitleTokenNumber browseTitleTokenKind = iota
	browseTitleTokenText
)

// compareBrowseTitles defines title order for media browsing. Punctuation is
// treated as a separator, digit runs compare numerically, and text compares
// case-insensitively. The raw title is the final tiebreaker, making the
// collation total and deterministic even when normalized forms match.
func compareBrowseTitles(left, right string) int {
	if left == right {
		return 0
	}

	// Keep first-character buckets contiguous so media.browse.index offsets and
	// seek cursors describe the exact media.browse order.
	if order := cmp.Compare(browseTitleBucketRank(left), browseTitleBucketRank(right)); order != 0 {
		return order
	}

	var leftAt, rightAt int
	for {
		leftToken, leftOK := nextBrowseTitleToken(left, leftAt)
		rightToken, rightOK := nextBrowseTitleToken(right, rightAt)
		switch {
		case !leftOK && !rightOK:
			return strings.Compare(left, right)
		case !leftOK:
			return -1
		case !rightOK:
			return 1
		}

		if leftToken.kind != rightToken.kind {
			return cmp.Compare(leftToken.kind, rightToken.kind)
		}

		var order int
		if leftToken.kind == browseTitleTokenNumber {
			order = compareBrowseTitleNumbers(leftToken.value, rightToken.value)
		} else {
			order = compareBrowseTitleText(leftToken.value, rightToken.value)
		}
		if order != 0 {
			return order
		}
		leftAt = leftToken.next
		rightAt = rightToken.next
	}
}

// browseTitleBucketRank mirrors browseBucketKeyExpr's ASCII buckets and their
// browse order: symbols, numbers, then A-Z. Non-ASCII initials stay in '#'.
func browseTitleBucketRank(title string) int {
	if title == "" {
		return 0
	}
	first := title[0]
	if first >= '0' && first <= '9' {
		return 1
	}
	if first >= 'a' && first <= 'z' {
		first -= 'a' - 'A'
	}
	if first >= 'A' && first <= 'Z' {
		return int(first-'A') + 2
	}
	return 0
}

func nextBrowseTitleToken(title string, at int) (browseTitleToken, bool) {
	for at < len(title) {
		r, size := utf8.DecodeRuneInString(title[at:])
		switch {
		case r >= '0' && r <= '9':
			start := at
			at += size
			for at < len(title) && title[at] >= '0' && title[at] <= '9' {
				at++
			}
			return browseTitleToken{value: title[start:at], next: at, kind: browseTitleTokenNumber}, true
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			start := at
			at += size
			for at < len(title) {
				r, size = utf8.DecodeRuneInString(title[at:])
				if (r >= '0' && r <= '9') || (!unicode.IsLetter(r) && !unicode.IsNumber(r)) {
					break
				}
				at += size
			}
			return browseTitleToken{value: title[start:at], next: at, kind: browseTitleTokenText}, true
		default:
			at += size
		}
	}
	return browseTitleToken{}, false
}

func compareBrowseDirectoryNames(left, right string) int {
	leftTitle := stripBrowseDirectoryMetadata(left)
	rightTitle := stripBrowseDirectoryMetadata(right)
	if order := compareBrowseTitles(leftTitle, rightTitle); order != 0 {
		return order
	}
	return strings.Compare(left, right)
}

func stripBrowseDirectoryMetadata(name string) string {
	var stripped strings.Builder
	stripped.Grow(len(name))
	for at := 0; at < len(name); {
		r, size := utf8.DecodeRuneInString(name[at:])
		closer, metadata := browseMetadataCloser(r)
		if !metadata {
			_, _ = stripped.WriteString(name[at : at+size])
			at += size
			continue
		}
		at += size
		for at < len(name) {
			r, size = utf8.DecodeRuneInString(name[at:])
			at += size
			if r == closer {
				break
			}
		}
	}
	return stripped.String()
}

func browseMetadataCloser(opener rune) (rune, bool) {
	switch opener {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '<':
		return '>', true
	default:
		return 0, false
	}
}

func compareBrowseTitleNumbers(left, right string) int {
	leftSignificant := strings.TrimLeft(left, "0")
	if leftSignificant == "" {
		leftSignificant = "0"
	}
	rightSignificant := strings.TrimLeft(right, "0")
	if rightSignificant == "" {
		rightSignificant = "0"
	}
	if order := cmp.Compare(len(leftSignificant), len(rightSignificant)); order != 0 {
		return order
	}
	if order := strings.Compare(leftSignificant, rightSignificant); order != 0 {
		return order
	}
	return cmp.Compare(len(left), len(right))
}

func compareBrowseTitleText(left, right string) int {
	for left != "" && right != "" {
		leftRune, leftSize := utf8.DecodeRuneInString(left)
		rightRune, rightSize := utf8.DecodeRuneInString(right)
		if order := cmp.Compare(unicode.ToLower(leftRune), unicode.ToLower(rightRune)); order != 0 {
			return order
		}
		left = left[leftSize:]
		right = right[rightSize:]
	}
	return cmp.Compare(len(left), len(right))
}
