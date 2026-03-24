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
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// benchGenerateUID builds a colon-separated hex UID from an integer,
// padded to 12 hex chars like a real NFC UID (e.g. "00:00:00:00:00:2A").
func benchGenerateUID(i int) string {
	hex := strings.ToUpper(strconv.FormatInt(int64(i), 16))
	for len(hex) < 12 {
		hex = "0" + hex
	}
	parts := make([]string, 0, len(hex)/2+1)
	for j := 0; j < len(hex); j += 2 {
		end := j + 2
		if end > len(hex) {
			end = len(hex)
		}
		parts = append(parts, hex[j:end])
	}
	return strings.Join(parts, ":")
}

func BenchmarkMatchMapping_Exact_50rules(b *testing.B) {
	b.ReportAllocs()
	mappings := make([]database.Mapping, 50)
	for i := range 50 {
		uid := benchGenerateUID(i)
		mappings[i] = database.Mapping{
			Type:    userdb.MappingTypeID,
			Match:   userdb.MatchTypeExact,
			Pattern: uid,
			Enabled: true,
		}
	}
	// Target is the last rule (worst case linear scan)
	token := tokens.Token{UID: benchGenerateUID(49)}
	b.ResetTimer()
	for b.Loop() {
		for i := range mappings {
			if checkMappingUID(&mappings[i], &token) {
				break
			}
		}
	}
}

func BenchmarkMatchMapping_Regex_50rules(b *testing.B) {
	b.ReportAllocs()
	mappings := make([]database.Mapping, 50)
	for i := range 50 {
		hex := strings.ToUpper(strconv.FormatInt(int64(i), 16))
		for len(hex) < 4 {
			hex = "0" + hex
		}
		mappings[i] = database.Mapping{
			Type:    userdb.MappingTypeID,
			Match:   userdb.MatchTypeRegex,
			Pattern: fmt.Sprintf("(?i)^[0-9a-f]*%s[0-9a-f]*$", hex),
			Enabled: true,
		}
	}
	token := tokens.Token{UID: "04:52:7C:A2:6B:5D:80"}
	b.ResetTimer()
	for b.Loop() {
		for i := range mappings {
			if checkMappingUID(&mappings[i], &token) {
				break
			}
		}
	}
}

func BenchmarkMatchMapping_Mixed_100rules(b *testing.B) {
	b.ReportAllocs()
	mappings := make([]database.Mapping, 100)
	for i := range 100 {
		hex := strings.ToUpper(strconv.FormatInt(int64(i), 16))
		for len(hex) < 8 {
			hex = "0" + hex
		}
		switch i % 3 {
		case 0:
			mappings[i] = database.Mapping{
				Type:    userdb.MappingTypeID,
				Match:   userdb.MatchTypeExact,
				Pattern: hex,
				Enabled: true,
			}
		case 1:
			mappings[i] = database.Mapping{
				Type:    userdb.MappingTypeID,
				Match:   userdb.MatchTypePartial,
				Pattern: hex[:4],
				Enabled: true,
			}
		default:
			mappings[i] = database.Mapping{
				Type:    userdb.MappingTypeID,
				Match:   userdb.MatchTypeRegex,
				Pattern: fmt.Sprintf("(?i)%s.*", hex[:4]),
				Enabled: true,
			}
		}
	}
	token := tokens.Token{UID: "04:52:7C:A2:6B:5D:80"}
	b.ResetTimer()
	for b.Loop() {
		for i := range mappings {
			if checkMappingUID(&mappings[i], &token) {
				break
			}
		}
	}
}
