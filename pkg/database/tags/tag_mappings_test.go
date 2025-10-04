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
				{TagTypeRegion, TagRegionUS},
				{TagTypeLang, TagLangEN},
			},
		},
		{
			name:  "Region Europe",
			input: "europe",
			expected: []CanonicalTag{
				{TagTypeRegion, TagRegionEU},
			},
		},
		{
			name:  "Region Japan with language",
			input: "japan",
			expected: []CanonicalTag{
				{TagTypeRegion, TagRegionJP},
				{TagTypeLang, TagLangJA},
			},
		},
		{
			name:  "Language code",
			input: "en",
			expected: []CanonicalTag{
				{TagTypeLang, TagLangEN},
			},
		},
		{
			name:  "Beta version",
			input: "beta",
			expected: []CanonicalTag{
				{TagTypeUnfinished, TagUnfinishedBeta},
			},
		},
		{
			name:  "Proto version",
			input: "proto",
			expected: []CanonicalTag{
				{TagTypeUnfinished, TagUnfinishedProto},
			},
		},
		{
			name:  "Alpha version",
			input: "alpha",
			expected: []CanonicalTag{
				{TagTypeUnfinished, TagUnfinishedAlpha},
			},
		},
		{
			name:  "Revision",
			input: "rev-a",
			expected: []CanonicalTag{
				{TagTypeRev, TagRevA},
			},
		},
		{
			name:  "Version number",
			input: "v1",
			expected: []CanonicalTag{
				{TagTypeRev, TagRev1},
			},
		},
		{
			name:  "Year",
			input: "1990",
			expected: []CanonicalTag{
				{TagTypeYear, TagYear1990},
			},
		},
		{
			name:  "NTSC video format",
			input: "ntsc",
			expected: []CanonicalTag{
				{TagTypeVideo, TagVideoNTSC},
			},
		},
		{
			name:  "PAL video format",
			input: "pal",
			expected: []CanonicalTag{
				{TagTypeVideo, TagVideoPAL},
			},
		},
		{
			name:  "Public Domain copyright",
			input: "pd",
			expected: []CanonicalTag{
				{TagTypeCopyright, TagCopyrightPD},
			},
		},
		{
			name:  "Cracked dump",
			input: "cr",
			expected: []CanonicalTag{
				{TagTypeDump, TagDumpCracked},
			},
		},
		{
			name:  "Verified dump",
			input: "!",
			expected: []CanonicalTag{
				{TagTypeDump, TagDumpVerified},
			},
		},
		{
			name:  "TOSEC system Amiga 500",
			input: "a500",
			expected: []CanonicalTag{
				{TagTypeCompatibility, TagCompatibilityAmigaA500},
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
				{TagTypeUnfinished, TagUnfinishedDemo},
			},
		},
		{
			name:  "Demo kiosk",
			input: "demo-kiosk",
			expected: []CanonicalTag{
				{TagTypeUnfinished, TagUnfinishedDemoKiosk},
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
