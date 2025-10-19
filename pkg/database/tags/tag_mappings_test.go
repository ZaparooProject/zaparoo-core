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

package tags

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapFilenameTagToCanonical(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []CanonicalTag
	}{
		{
			name:  "Region USA maps to region and language",
			input: "usa",
			expected: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionUS, Source: TagSourceUnknown},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Region Europe",
			input: "europe",
			expected: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionEU, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Region Japan with language",
			input: "japan",
			expected: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionJP, Source: TagSourceUnknown},
				{Type: TagTypeLang, Value: TagLangJA, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Language code",
			input: "en",
			expected: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Beta version",
			input: "beta",
			expected: []CanonicalTag{
				{Type: TagTypeUnfinished, Value: TagUnfinishedBeta, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Proto version",
			input: "proto",
			expected: []CanonicalTag{
				{Type: TagTypeUnfinished, Value: TagUnfinishedProto, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Alpha version",
			input: "alpha",
			expected: []CanonicalTag{
				{Type: TagTypeUnfinished, Value: TagUnfinishedAlpha, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Revision",
			input: "rev-a",
			expected: []CanonicalTag{
				{Type: TagTypeRev, Value: TagRevA, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Version number",
			input: "v1",
			expected: []CanonicalTag{
				{Type: TagTypeRev, Value: TagRev1, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Wildcard year (specific year unknown within 1980s)",
			input: "198x",
			expected: []CanonicalTag{
				{Type: TagTypeYear, Value: TagYear198X, Source: TagSourceUnknown},
			},
		},
		{
			name:  "NTSC video format",
			input: "ntsc",
			expected: []CanonicalTag{
				{Type: TagTypeVideo, Value: TagVideoNTSC, Source: TagSourceUnknown},
			},
		},
		{
			name:  "PAL video format",
			input: "pal",
			expected: []CanonicalTag{
				{Type: TagTypeVideo, Value: TagVideoPAL, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Public Domain copyright",
			input: "pd",
			expected: []CanonicalTag{
				{Type: TagTypeCopyright, Value: TagCopyrightPD, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Cracked dump",
			input: "cr",
			expected: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpCracked, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Verified dump",
			input: "!",
			expected: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpVerified, Source: TagSourceUnknown},
			},
		},
		{
			name:  "TOSEC system Amiga 500",
			input: "a500",
			expected: []CanonicalTag{
				{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500, Source: TagSourceUnknown},
			},
		},
		{
			name:     "Unknown tag returns nil",
			input:    "foobar",
			expected: nil,
		},
		{
			name:  "Demo",
			input: "demo",
			expected: []CanonicalTag{
				{Type: TagTypeUnfinished, Value: TagUnfinishedDemo, Source: TagSourceUnknown},
			},
		},
		{
			name:  "Demo kiosk",
			input: "demo-kiosk",
			expected: []CanonicalTag{
				{Type: TagTypeUnfinished, Value: TagUnfinishedDemoKiosk, Source: TagSourceUnknown},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapFilenameTagToCanonical(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetTagsFromFileName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected []string
	}{
		{
			name:     "Simple filename with region and language",
			filename: "Super Mario Bros (USA)",
			expected: []string{"region:us", "lang:en"},
		},
		{
			name:     "Multiple tags",
			filename: "Sonic (Europe) (Rev A)",
			expected: []string{"region:eu", "rev:a"}, // "Rev A" normalizes to "rev-a" and now matches
		},
		{
			name:     "Beta version",
			filename: "Game (Beta)",
			expected: []string{"unfinished:beta"},
		},
		{
			name:     "Multiple tags with regions",
			filename: "Game (USA)(Japan)", // Fixed: proper No-Intro format uses separate parentheses
			expected: []string{"region:us", "lang:en", "region:jp", "lang:ja"},
		},
		{
			name:     "TOSEC-style tags",
			filename: "Game (1990)(NTSC)(PD)", // Removed Publisher (not a valid tag)
			expected: []string{"year:1990", "video:ntsc", "copyright:pd"},
		},
		{
			name:     "Brackets are dump info",
			filename: "Game (USA)[!][h]", // Fixed: brackets=dump info, parens=metadata
			expected: []string{"region:us", "lang:en", "dump:verified", "dump:hacked"},
		},
		{
			name:     "Mixed brackets and parentheses",
			filename: "Game (USA)[!]", // Fixed: Rev should be in parens, not brackets
			expected: []string{"region:us", "lang:en", "dump:verified"},
		},
		{
			name:     "Unknown tags create unknown type",
			filename: "Game (Unknown Tag)",
			expected: []string{"unknown:unknown-tag"}, // New parser tracks unknown tags
		},
		{
			name:     "No tags",
			filename: "Game.rom",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonicalStructs := ParseFilenameToCanonicalTags(tt.filename)
			result := make([]string, 0, len(canonicalStructs))
			for _, ct := range canonicalStructs {
				result = append(result, ct.String())
			}
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}
