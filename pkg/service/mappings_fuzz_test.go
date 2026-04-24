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

package service

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// FuzzCheckMappingUID tests UID mapping matching with arbitrary patterns and
// data, covering exact, partial, and regex match types.
func FuzzCheckMappingUID(f *testing.F) {
	f.Add("04:AB:CD:EF", "04:AB:CD:EF", "exact")
	f.Add("04AB", "04ABCDEF1234", "partial")
	f.Add("^04[A-F0-9]+$", "04ABCDEF", "regex")
	f.Add(".*", "anything", "regex")
	f.Add("[invalid", "test", "regex")
	f.Add("", "", "exact")
	f.Add("test", "TEST", "exact")
	f.Add("(a+)+$", "aaaaaaaaaaaaaaaaX", "regex")
	f.Add("^(a|a)*$", "aaaaaaa", "regex")

	f.Fuzz(func(t *testing.T, pattern, uid, matchType string) {
		// Normalize match type to valid values
		switch matchType {
		case userdb.MatchTypeExact, userdb.MatchTypePartial, userdb.MatchTypeRegex:
		default:
			return
		}

		m := &database.Mapping{
			Pattern: pattern,
			Match:   matchType,
		}
		token := &tokens.Token{UID: uid}

		result := checkMappingUID(m, token)

		// Determinism
		result2 := checkMappingUID(m, token)
		if result != result2 {
			t.Errorf("non-deterministic result for pattern=%q uid=%q match=%q", pattern, uid, matchType)
		}

		// Exact match with normalized values must be consistent
		if matchType == userdb.MatchTypeExact {
			normalizedUID := userdb.NormalizeID(uid)
			normalizedPattern := userdb.NormalizeID(pattern)
			expected := normalizedUID == normalizedPattern
			if result != expected {
				t.Errorf("exact match inconsistency: NormalizeID(%q)=%q NormalizeID(%q)=%q expected=%v got=%v",
					uid, normalizedUID, pattern, normalizedPattern, expected, result)
			}
		}
	})
}

// FuzzCheckMappingText tests text mapping matching with arbitrary patterns and data.
func FuzzCheckMappingText(f *testing.F) {
	f.Add("SNES/Super Metroid.sfc", "SNES/Super Metroid.sfc", "exact")
	f.Add("SNES", "SNES/Super Metroid.sfc", "partial")
	f.Add("^SNES/.*\\.sfc$", "SNES/Super Metroid.sfc", "regex")
	f.Add("[invalid", "test", "regex")
	f.Add("", "", "exact")
	f.Add("**launch", "**launch.system:nes", "partial")

	f.Fuzz(func(t *testing.T, pattern, text, matchType string) {
		switch matchType {
		case userdb.MatchTypeExact, userdb.MatchTypePartial, userdb.MatchTypeRegex:
		default:
			return
		}

		m := &database.Mapping{
			Pattern: pattern,
			Match:   matchType,
		}
		token := &tokens.Token{Text: text}

		result := checkMappingText(m, token)

		// Determinism
		result2 := checkMappingText(m, token)
		if result != result2 {
			t.Errorf("non-deterministic result for pattern=%q text=%q match=%q", pattern, text, matchType)
		}

		// Exact match must be string equality
		if matchType == userdb.MatchTypeExact {
			expected := text == pattern
			if result != expected {
				t.Errorf("exact match: expected %v got %v for pattern=%q text=%q", expected, result, pattern, text)
			}
		}
	})
}

// FuzzCheckMappingData tests data mapping matching with arbitrary patterns and data.
func FuzzCheckMappingData(f *testing.F) {
	f.Add("payload123", "payload123", "exact")
	f.Add("pay", "payload123", "partial")
	f.Add("^pay.*\\d+$", "payload123", "regex")
	f.Add("[invalid", "test", "regex")
	f.Add("", "", "exact")

	f.Fuzz(func(t *testing.T, pattern, data, matchType string) {
		switch matchType {
		case userdb.MatchTypeExact, userdb.MatchTypePartial, userdb.MatchTypeRegex:
		default:
			return
		}

		m := &database.Mapping{
			Pattern: pattern,
			Match:   matchType,
		}
		token := &tokens.Token{Data: data}

		result := checkMappingData(m, token)

		// Determinism
		result2 := checkMappingData(m, token)
		if result != result2 {
			t.Errorf("non-deterministic result for pattern=%q data=%q match=%q", pattern, data, matchType)
		}

		// Exact match must be string equality
		if matchType == userdb.MatchTypeExact {
			expected := data == pattern
			if result != expected {
				t.Errorf("exact match: expected %v got %v for pattern=%q data=%q", expected, result, pattern, data)
			}
		}
	})
}
