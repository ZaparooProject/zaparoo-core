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

package helpers

import (
	"errors"
	"strings"
)

var ErrUnclosedQuote = errors.New("unclosed quote in command string")

// SplitCommand splits a command string into a slice of arguments, respecting
// double quotes, single quotes, and backslash escaping. Quotes are stripped
// from the output. This is used instead of shell invocation (sh -c) to avoid
// shell injection vulnerabilities.
//
// Rules:
//   - Unquoted whitespace (space, tab) separates arguments
//   - Double-quoted strings preserve spaces; backslash escapes \" and \\
//   - Single-quoted strings preserve spaces; no escape sequences are recognized
//   - Backslash outside quotes escapes the next character
//   - Empty quoted strings produce an empty argument ("")
func SplitCommand(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	hasContent := false

	runes := []rune(s)
	i := 0

	for i < len(runes) {
		ch := runes[i]

		switch {
		case ch == '\\' && i+1 < len(runes):
			_, _ = current.WriteRune(runes[i+1])
			hasContent = true
			i += 2

		case ch == '"':
			hasContent = true
			i++
			closed := false
			for i < len(runes) && !closed {
				switch {
				case runes[i] == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
					_, _ = current.WriteRune(runes[i+1])
					i += 2
				case runes[i] == '"':
					i++
					closed = true
				default:
					_, _ = current.WriteRune(runes[i])
					i++
				}
			}
			if !closed {
				return nil, ErrUnclosedQuote
			}

		case ch == '\'':
			hasContent = true
			i++
			closed := false
			for i < len(runes) {
				if runes[i] == '\'' {
					i++
					closed = true
					break
				}
				_, _ = current.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, ErrUnclosedQuote
			}

		case ch == ' ' || ch == '\t':
			if hasContent {
				args = append(args, current.String())
				current.Reset()
				hasContent = false
			}
			i++

		default:
			_, _ = current.WriteRune(ch)
			hasContent = true
			i++
		}
	}

	if hasContent {
		args = append(args, current.String())
	}

	return args, nil
}
