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

package gamedatabase

import "strings"

// Bounds on one row's tags. Upstream's largest values today are a 615-byte
// token, 11 tokens in a row, 54 subtags (a compilation's contents) and 9
// children; anything far beyond that is not a tag cell.
const (
	maxTokens     = 64
	maxTokenBytes = 2048
	maxSubtags    = 128
	maxChildren   = 16
)

// tagMarker opens every tag in a row's tags cell.
const tagMarker = "#"

// subtag is one ":name" of a tag with its ">child" values.
type subtag struct {
	name     string
	children []string
}

// token is one parsed "#type:sub>child:sub" tag.
type token struct {
	tagType string
	subs    []subtag
}

// isName reports whether s is a tag, subtag or child name as upstream writes
// them: lower-case letters and digits, with the underscore MAME set names use.
func isName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// parseToken reads one whitespace-delimited tag. It accepts exactly the
// documented grammar and nothing else: a tag that needs repair is listed in
// tokenRecoveries, so every tolerated mistake is one the data really makes.
func parseToken(raw string) (token, bool) {
	body, marked := strings.CutPrefix(raw, tagMarker)
	if !marked || len(raw) > maxTokenBytes {
		return token{}, false
	}
	parts := strings.Split(body, ":")
	if !isName(parts[0]) || len(parts)-1 > maxSubtags {
		return token{}, false
	}
	tok := token{tagType: parts[0]}
	if len(parts) > 1 {
		tok.subs = make([]subtag, 0, len(parts)-1)
	}
	for _, part := range parts[1:] {
		names := strings.Split(part, ">")
		if len(names)-1 > maxChildren {
			return token{}, false
		}
		for _, name := range names {
			if !isName(name) {
				return token{}, false
			}
		}
		tok.subs = append(tok.subs, subtag{name: names[0], children: names[1:]})
	}
	return tok, true
}

// splitTokens splits a tags cell into its tags, applying the recoveries for
// the tags upstream wrote wrongly. unreadable lists the tags that are neither
// well formed nor recoverable.
func splitTokens(cell string) (tokens []token, unreadable []string) {
	fields := strings.Fields(cell)
	if len(fields) > maxTokens {
		fields = fields[:maxTokens]
	}
	for _, field := range fields {
		if fixed, listed := tokenRecoveries[field]; listed {
			for _, replacement := range fixed {
				if tok, ok := parseToken(replacement); ok {
					tokens = append(tokens, tok)
				}
			}
			continue
		}
		tok, ok := parseToken(field)
		if !ok {
			unreadable = append(unreadable, field)
			continue
		}
		tokens = append(tokens, tok)
	}
	return tokens, unreadable
}
