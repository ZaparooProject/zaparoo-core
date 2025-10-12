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

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantParen   []string
		wantBracket []string
	}{
		{
			name:        "simple tags",
			filename:    "Game (USA)(En)[!].zip",
			wantParen:   []string{"USA", "En"},
			wantBracket: []string{"!"},
		},
		{
			name:        "multiple bracket tags",
			filename:    "Game (Japan)[h][cr][!].zip",
			wantParen:   []string{"Japan"},
			wantBracket: []string{"h", "cr", "!"},
		},
		{
			name:        "no tags",
			filename:    "PlainGame.zip",
			wantParen:   []string{},
			wantBracket: []string{},
		},
		{
			name:        "only parentheses",
			filename:    "Game (USA)(v1.2)(Beta).rom",
			wantParen:   []string{"USA", "v1.2", "Beta"},
			wantBracket: []string{},
		},
		{
			name:        "only brackets",
			filename:    "Game [!][f].bin",
			wantParen:   []string{},
			wantBracket: []string{"!", "f"},
		},
		{
			name:        "empty tags ignored",
			filename:    "Game ()(USA)[]En].zip",
			wantParen:   []string{"USA"},
			wantBracket: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParen, gotBracket := extractTags(tt.filename)
			assert.Equal(t, tt.wantParen, gotParen, "Parentheses tags mismatch")
			assert.Equal(t, tt.wantBracket, gotBracket, "Bracket tags mismatch")
		})
	}
}

func TestExtractTags_BracesAndAngles(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantParen   []string
		wantBracket []string
	}{
		{
			name:        "braces treated like parentheses",
			filename:    "Game {USA}.rom",
			wantParen:   []string{"USA"},
			wantBracket: []string{},
		},
		{
			name:        "angle brackets treated like parentheses",
			filename:    "Game <Europe>.bin",
			wantParen:   []string{"Europe"},
			wantBracket: []string{},
		},
		{
			name:        "mixed bracket types",
			filename:    "Game (USA){En}<Beta>[!].zip",
			wantParen:   []string{"USA", "En", "Beta"},
			wantBracket: []string{"!"},
		},
		{
			name:        "multiple braces",
			filename:    "Game {Japan}{v1.0}.sfc",
			wantParen:   []string{"Japan", "v1.0"},
			wantBracket: []string{},
		},
		{
			name:        "multiple angles",
			filename:    "Game <Proto><Alpha>.nes",
			wantParen:   []string{"Proto", "Alpha"},
			wantBracket: []string{},
		},
		{
			name:        "all four bracket types",
			filename:    "Game (USA)[!]{En}<Beta>.rom",
			wantParen:   []string{"USA", "En", "Beta"},
			wantBracket: []string{"!"},
		},
		{
			name:        "empty braces and angles ignored",
			filename:    "Game {}<>(USA).rom",
			wantParen:   []string{"USA"},
			wantBracket: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParen, gotBracket := extractTags(tt.filename)
			assert.Equal(t, tt.wantParen, gotParen, "Parentheses tags mismatch")
			assert.Equal(t, tt.wantBracket, gotBracket, "Bracket tags mismatch")
		})
	}
}

func TestExtractSpecialPatterns(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantRemaining string
		wantTags      []CanonicalTag
	}{
		{
			name:     "disc X of Y",
			filename: "Final Fantasy VII (Disc 1 of 3)(USA).bin",
			wantTags: []CanonicalTag{
				{TagTypeMedia, TagMediaDisc},
				{TagTypeDisc, TagDisc1},
				{TagTypeDiscTotal, TagDiscTotal3},
			},
			wantRemaining: "Final Fantasy VII (USA).bin",
		},
		{
			name:     "disc X of Y case insensitive",
			filename: "Game (DISC 2 OF 2)(Europe).iso",
			wantTags: []CanonicalTag{
				{TagTypeMedia, TagMediaDisc},
				{TagTypeDisc, TagDisc2},
				{TagTypeDiscTotal, TagDiscTotal2},
			},
			wantRemaining: "Game (Europe).iso",
		},
		{
			name:     "revision tag",
			filename: "Sonic (Rev 1)(USA).md",
			wantTags: []CanonicalTag{
				{TagTypeRev, TagRev1},
			},
			wantRemaining: "Sonic (USA).md",
		},
		{
			name:     "revision tag with letter",
			filename: "Game (Rev A)(Japan).sfc",
			wantTags: []CanonicalTag{
				{TagTypeRev, TagRevA},
			},
			wantRemaining: "Game (Japan).sfc",
		},
		{
			name:     "version tag",
			filename: "Mario (v1.2)(USA).n64",
			wantTags: []CanonicalTag{
				{TagTypeRev, "1.2"},
			},
			wantRemaining: "Mario (USA).n64",
		},
		{
			name:     "version tag with multiple dots",
			filename: "Game (v1.2.3)(Europe).gba",
			wantTags: []CanonicalTag{
				{TagTypeRev, "1.2.3"},
			},
			wantRemaining: "Game (Europe).gba",
		},
		{
			name:          "no special patterns",
			filename:      "Plain Game (USA)(En).rom",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Plain Game (USA)(En).rom",
		},
		{
			name:     "multiple special patterns",
			filename: "FF7 (Disc 1 of 3)(v1.1)(USA).bin",
			wantTags: []CanonicalTag{
				{TagTypeMedia, TagMediaDisc},
				{TagTypeDisc, TagDisc1},
				{TagTypeDiscTotal, TagDiscTotal3},
				{TagTypeRev, "1.1"},
			},
			wantRemaining: "FF7 (USA).bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTags, gotRemaining := extractSpecialPatterns(tt.filename)
			assert.Equal(t, tt.wantTags, gotTags, "Special pattern tags mismatch")
			assert.Contains(t, gotRemaining, tt.wantRemaining[:len(tt.wantRemaining)-4],
				"Remaining filename should contain base name")
		})
	}
}

func TestParseMultiLanguageTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		wantTags []CanonicalTag
		wantNil  bool
	}{
		{
			name: "three languages",
			tag:  "En,Fr,De",
			wantTags: []CanonicalTag{
				{TagTypeLang, TagLangEN},
				{TagTypeLang, TagLangFR},
				{TagTypeLang, TagLangDE},
			},
		},
		{
			name: "two languages",
			tag:  "En,Fr", // Use Fr which is in standalone lang list
			wantTags: []CanonicalTag{
				{TagTypeLang, TagLangEN},
				{TagTypeLang, TagLangFR},
			},
		},
		{
			name:    "single language not multi",
			tag:     "En",
			wantNil: true,
		},
		{
			name:    "comma but not languages",
			tag:     "Test,Data",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMultiLanguageTag(tt.tag)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tt.wantTags, got)
			}
		})
	}
}

func TestDisambiguateTag_BracketContext(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		wantTags []CanonicalTag
	}{
		{
			name: "tr in brackets is translated",
			tag:  "tr",
			wantTags: []CanonicalTag{
				{TagTypeDump, TagDumpTranslated},
			},
		},
		{
			name: "verified dump",
			tag:  "!",
			wantTags: []CanonicalTag{
				{TagTypeDump, TagDumpVerified},
			},
		},
		{
			name: "bad dump",
			tag:  "b",
			wantTags: []CanonicalTag{
				{TagTypeDump, TagDumpBad},
			},
		},
		{
			name: "hacked",
			tag:  "h",
			wantTags: []CanonicalTag{
				{TagTypeDump, TagDumpHacked},
			},
		},
		{
			name: "fixed",
			tag:  "f",
			wantTags: []CanonicalTag{
				{TagTypeDump, TagDumpFixed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ParseContext{
				CurrentTag:         tt.tag,
				CurrentBracketType: "bracket",
				ProcessedTags:      []CanonicalTag{},
			}
			got := disambiguateTag(ctx)
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

func TestDisambiguateTag_ParenthesesContext(t *testing.T) {
	tests := []struct {
		name          string
		tag           string
		processedTags []CanonicalTag
		wantTags      []CanonicalTag
		position      int
	}{
		{
			name:          "ch with German lang is Switzerland",
			tag:           "ch",
			processedTags: []CanonicalTag{{TagTypeLang, TagLangDE}},
			position:      1,
			wantTags:      []CanonicalTag{{TagTypeRegion, TagRegionCH}},
		},
		{
			name:          "ch without context is Chinese",
			tag:           "ch",
			processedTags: []CanonicalTag{},
			position:      2,
			wantTags:      []CanonicalTag{{TagTypeLang, TagLangZH}},
		},
		{
			name:          "ch early position is region",
			tag:           "ch",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{TagTypeRegion, TagRegionCH}},
		},
		{
			name:          "tr early is region",
			tag:           "tr",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{TagTypeRegion, TagRegionTR}},
		},
		{
			name:          "tr with region already is language",
			tag:           "tr",
			processedTags: []CanonicalTag{{TagTypeRegion, TagRegionUS}},
			position:      1,
			wantTags:      []CanonicalTag{{TagTypeLang, TagLangTR}},
		},
		{
			name:          "bs is always Bosnian language",
			tag:           "bs",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{TagTypeLang, TagLangBS}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ParseContext{
				CurrentTag:         tt.tag,
				CurrentBracketType: "paren",
				ProcessedTags:      tt.processedTags,
				CurrentIndex:       tt.position,
			}
			got := disambiguateTag(ctx)
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

func TestParseFilenameToCanonicalTags_Integration(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantTags []string // String representation for easy assertion
	}{
		{
			name:     "simple No-Intro format",
			filename: "Super Mario Bros (USA)(En)[!].nes",
			wantTags: []string{"region:us", "lang:en", "dump:verified"},
		},
		{
			name:     "ch disambiguation - Swiss with German",
			filename: "Game (Ch)(De)[!].rom",
			wantTags: []string{"region:ch", "lang:de", "dump:verified"},
		},
		{
			name:     "ch disambiguation - Chinese",
			filename: "Game (Japan)(Ch).rom",
			wantTags: []string{"region:jp", "lang:zh"}, // jp doesn't auto-add ja in new parser
		},
		{
			name:     "tr in brackets is translated",
			filename: "Game (USA)(En)[tr].rom",
			wantTags: []string{"region:us", "lang:en", "dump:translated"},
		},
		{
			name:     "disc X of Y special pattern",
			filename: "Final Fantasy VII (Disc 1 of 3)(USA)(En).bin",
			wantTags: []string{"media:disc", "disc:1", "disctotal:3", "region:us", "lang:en"},
		},
		{
			name:     "revision tag",
			filename: "Sonic (Rev 1)(USA).md",
			wantTags: []string{"rev:1", "region:us"}, // usa region, language not auto-added
		},
		{
			name:     "version tag",
			filename: "Mario Kart (v1.2)(Europe).n64",
			wantTags: []string{"rev:1.2", "region:eu"},
		},
		{
			name:     "multi-language tag",
			filename: "Game (En,Fr,De)(Europe)[!].gba",
			wantTags: []string{"lang:en", "lang:fr", "lang:de", "region:eu", "dump:verified"},
		},
		{
			name:     "beta with dump info",
			filename: "Prototype (Beta)(USA)[b].rom",
			wantTags: []string{"unfinished:beta", "region:us", "dump:bad"},
		},
		{
			name:     "multiple hacks",
			filename: "Game (Japan)[h][cr][t].sfc",
			wantTags: []string{"region:jp", "dump:hacked", "dump:cracked", "dump:trained"},
		},
		{
			name:     "complex real-world example",
			filename: "Legend of Zelda, The - Ocarina of Time (v1.2)(USA)(En)[!].z64",
			wantTags: []string{"rev:1.2", "region:us", "lang:en", "dump:verified"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFilenameToCanonicalTags(tt.filename)
			gotStrings := make([]string, len(got))
			for i, tag := range got {
				gotStrings[i] = tag.String()
			}

			// Check that all expected tags are present
			for _, want := range tt.wantTags {
				assert.Contains(t, gotStrings, want, "Expected tag %s not found in %v", want, gotStrings)
			}

			// Check we don't have unexpected extras (allow some flexibility)
			assert.LessOrEqual(t, len(gotStrings), len(tt.wantTags)+2, "Too many tags returned")
		})
	}
}

func TestParseFilenameToCanonicalTags_NoDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{
			name:     "tr should not create duplicates",
			filename: "Game (USA)(Tr)[tr].rom",
		},
		{
			name:     "ch should not create duplicates",
			filename: "Game (Ch)(De).rom",
		},
		{
			name:     "bs should not create duplicates",
			filename: "Game (BS)(Europe).sfc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFilenameToCanonicalTags(tt.filename)
			gotStrings := make([]string, len(got))
			for i, tag := range got {
				gotStrings[i] = tag.String()
			}

			// Check for duplicates
			seen := make(map[string]bool)
			for _, tag := range gotStrings {
				assert.False(t, seen[tag], "Duplicate tag found: %s in %v", tag, gotStrings)
				seen[tag] = true
			}
		})
	}
}

func TestExtractSpecialPatterns_BracketlessTranslation(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantRemaining string
		wantTags      []CanonicalTag
	}{
		{
			name:     "T+Eng newer translation",
			filename: "Final Fantasy V T+Eng.smc",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangEN},
			},
			wantRemaining: "Final Fantasy V smc",
		},
		{
			name:     "T-Ger older translation",
			filename: "Secret of Mana T-Ger.sfc",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslationOld},
				{TagTypeLang, TagLangDE},
			},
			wantRemaining: "Secret of Mana sfc",
		},
		{
			name:     "TFre generic translation",
			filename: "Chrono Trigger TFre.smc",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangFR},
			},
			wantRemaining: "Chrono Trigger smc",
		},
		{
			name:     "T+Eng v1.0 with version",
			filename: "Fire Emblem T+Eng v1.0.gba",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangEN},
				{TagTypeRev, "1.0"},
			},
			wantRemaining: "Fire Emblem gba",
		},
		{
			name:     "T+Spa v2.1.3 with multi-part version",
			filename: "Mother 3 T+Spa v2.1.3.gba",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangES},
				{TagTypeRev, "2.1.3"},
			},
			wantRemaining: "Mother 3 gba",
		},
		{
			name:     "T+Ita Italian translation",
			filename: "Pokemon Ruby T+Ita.gba",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangIT},
			},
			wantRemaining: "Pokemon Ruby gba",
		},
		{
			name:     "T+Rus Russian translation",
			filename: "Zelda T+Rus v1.5.nes",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangRU},
				{TagTypeRev, "1.5"},
			},
			wantRemaining: "Zelda nes",
		},
		{
			name:     "T+Por Portuguese translation",
			filename: "Final Fantasy VI T+Por.smc",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangPT},
			},
			wantRemaining: "Final Fantasy VI smc",
		},
		{
			name:     "translation with other tags",
			filename: "Game (USA) T+Eng v2.0 [!].rom",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangEN},
				{TagTypeRev, "2.0"},
			},
			wantRemaining: "Game (USA) [!].rom",
		},
		{
			name:          "no translation tag",
			filename:      "Regular Game (USA).rom",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Regular Game (USA).rom",
		},
		{
			name:          "C++ should not match T+",
			filename:      "C++ Tutorial (USA).pdf",
			wantTags:      []CanonicalTag{},
			wantRemaining: "C++ Tutorial (USA).pdf",
		},
		{
			name:          "ATP should not match T+",
			filename:      "ATP Tennis (USA).rom",
			wantTags:      []CanonicalTag{},
			wantRemaining: "ATP Tennis (USA).rom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTags, gotRemaining := extractSpecialPatterns(tt.filename)
			assert.Equal(t, tt.wantTags, gotTags, "Translation tags mismatch")
			assert.Equal(t, tt.wantRemaining, gotRemaining, "Remaining filename mismatch")
		})
	}
}

func TestExtractSpecialPatterns_BracketlessVersion(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantRemaining string
		wantTags      []CanonicalTag
	}{
		{
			name:     "standalone v1.0",
			filename: "Game Name v1.0.rom",
			wantTags: []CanonicalTag{
				{TagTypeRev, "1.0"},
			},
			wantRemaining: "Game Name .rom",
		},
		{
			name:     "standalone v2.1.3",
			filename: "Another Game v2.1.3.bin",
			wantTags: []CanonicalTag{
				{TagTypeRev, "2.1.3"},
			},
			wantRemaining: "Another Game .bin",
		},
		{
			name:     "v1 single digit",
			filename: "Old Game v1.smc",
			wantTags: []CanonicalTag{
				{TagTypeRev, "1"},
			},
			wantRemaining: "Old Game .smc",
		},
		{
			name:     "version not extracted if translation already has it",
			filename: "Game T+Eng v1.0.rom",
			wantTags: []CanonicalTag{
				{TagTypeUnlicensed, TagUnlicensedTranslation},
				{TagTypeLang, TagLangEN},
				{TagTypeRev, "1.0"},
			},
			wantRemaining: "Game rom",
		},
		{
			name:          "no version tag",
			filename:      "Plain Game.rom",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Plain Game.rom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTags, gotRemaining := extractSpecialPatterns(tt.filename)
			assert.Equal(t, tt.wantTags, gotTags, "Version tags mismatch")
			assert.Equal(t, tt.wantRemaining, gotRemaining, "Remaining filename mismatch")
		})
	}
}

func TestParseBracketlessTranslation_FullPipeline(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		wantContains []string
	}{
		{
			name:     "T+Eng full parsing",
			filename: "Final Fantasy V T+Eng v1.1 (USA)[!].smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:en",
				"rev:1.1",
				"region:us",
				"dump:verified",
			},
		},
		{
			name:     "T-Ger full parsing",
			filename: "Secret of Mana T-Ger (Europe).sfc",
			wantContains: []string{
				"unlicensed:translation:old",
				"lang:de",
				"region:eu",
			},
		},
		{
			name:     "TFre without version",
			filename: "Chrono Trigger TFre (France).smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:fr",
				"region:fr",
			},
		},
		{
			name:     "translation with region language",
			filename: "Pokemon Ruby T+Ita v2.0 (Italy)(It).gba",
			wantContains: []string{
				"unlicensed:translation",
				"lang:it",
				"rev:2.0",
				"region:it",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFilenameToCanonicalTags(tt.filename)
			gotStrings := make([]string, len(got))
			for i, tag := range got {
				gotStrings[i] = tag.String()
			}

			for _, want := range tt.wantContains {
				assert.Contains(t, gotStrings, want, "Expected tag %s not found in %v", want, gotStrings)
			}
		})
	}
}

func TestParseBracesAndAngles_FullPipeline(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		wantTags     []CanonicalTag
		wantContains []string
	}{
		{
			name:         "region in braces",
			filename:     "Super Mario Bros {USA}.nes",
			wantContains: []string{"region:us"},
		},
		{
			name:         "language in angle brackets",
			filename:     "Final Fantasy <En>.sfc",
			wantContains: []string{"lang:en"},
		},
		{
			name:         "beta in braces",
			filename:     "Sonic {Beta}.md",
			wantContains: []string{"unfinished:beta"},
		},
		{
			name:         "mixed brackets",
			filename:     "Game (USA){En}<Proto>[!].zip",
			wantContains: []string{"region:us", "lang:en", "unfinished:proto", "dump:verified"},
		},
		{
			name:         "version in braces",
			filename:     "Zelda {v1.0}.rom",
			wantContains: []string{"rev:1.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := ParseFilenameToCanonicalTags(tt.filename)

			tagStrings := make([]string, 0, len(tags))
			for _, tag := range tags {
				tagStrings = append(tagStrings, string(tag.Type)+":"+string(tag.Value))
			}

			for _, expected := range tt.wantContains {
				assert.Contains(t, tagStrings, expected, "Expected tag %s not found in %v", expected, tagStrings)
			}
		})
	}
}

func TestExtractSpecialPatterns_EditionWords(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantTags []CanonicalTag
	}{
		// English
		{
			name:     "English version",
			filename: "Game Version (USA).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "English edition",
			filename: "Game Edition (USA).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		{
			name:     "version before parentheses",
			filename: "Deluxe Version (USA).bin",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "edition before brackets",
			filename: "Ultimate Edition [!].iso",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// German
		{
			name:     "German ausgabe",
			filename: "Spiel Ausgabe (Germany).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// Italian
		{
			name:     "Italian versione",
			filename: "Gioco Versione (Italy).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "Italian edizione",
			filename: "Gioco Edizione (Italy).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// Portuguese
		{
			name:     "Portuguese versao",
			filename: "Jogo Versao (Brazil).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "Portuguese edicao",
			filename: "Jogo Edicao (Brazil).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// Japanese
		{
			name:     "Japanese version (バージョン)",
			filename: "ゲーム バージョン (Japan).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "Japanese version (ヴァージョン)",
			filename: "ゲーム ヴァージョン (Japan).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "Japanese edition (エディション)",
			filename: "ゲーム エディション (Japan).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// Case insensitivity
		{
			name:     "VERSION uppercase",
			filename: "Game VERSION (USA).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "Edition mixed case",
			filename: "Game EdItIoN (USA).rom",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// At end of filename
		{
			name:     "version at end",
			filename: "Special Version",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionVersion}},
		},
		{
			name:     "edition at end",
			filename: "Limited Edition",
			wantTags: []CanonicalTag{{TagTypeEdition, TagEditionEdition}},
		},
		// No match
		{
			name:     "version in middle of word should not match",
			filename: "Perversion Game (USA).rom",
			wantTags: []CanonicalTag{},
		},
		{
			name:     "no edition words",
			filename: "Regular Game (USA).rom",
			wantTags: []CanonicalTag{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTags, _ := extractSpecialPatterns(tt.filename)
			assert.Equal(t, tt.wantTags, gotTags, "Edition word tags mismatch")
			// Note: Edition words are NOT removed from filename - they're just tagged
			// The words will be stripped later by slugification
		})
	}
}

func TestParseFilenameToCanonicalTags_EditionWords(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		wantContains []string
	}{
		{
			name:         "English version with region",
			filename:     "Deluxe Version (USA)(En)[!].rom",
			wantContains: []string{"edition:version", "region:us", "lang:en", "dump:verified"},
		},
		{
			name:         "English edition with region",
			filename:     "Ultimate Edition (Europe)(En,Fr,De).iso",
			wantContains: []string{"edition:edition", "region:eu", "lang:en", "lang:fr", "lang:de"},
		},
		{
			name:         "German ausgabe",
			filename:     "Spiel Ausgabe (Germany)(De).rom",
			wantContains: []string{"edition:edition", "region:de", "lang:de"},
		},
		{
			name:         "Italian versione",
			filename:     "Gioco Versione (Italy)(It)[!].rom",
			wantContains: []string{"edition:version", "region:it", "lang:it", "dump:verified"},
		},
		{
			name:         "Portuguese edicao",
			filename:     "Jogo Edicao (Brazil)(Pt).rom",
			wantContains: []string{"edition:edition", "region:br", "lang:pt"},
		},
		{
			name:         "Japanese バージョン",
			filename:     "ゲーム バージョン (Japan)(Ja).rom",
			wantContains: []string{"edition:version", "region:jp", "lang:ja"},
		},
		{
			name:         "Japanese エディション",
			filename:     "ゲーム エディション (Japan)(Ja)[!].rom",
			wantContains: []string{"edition:edition", "region:jp", "lang:ja", "dump:verified"},
		},
		{
			name:         "version with revision number",
			filename:     "Game Version (v1.2)(USA).rom",
			wantContains: []string{"edition:version", "rev:1.2", "region:us"},
		},
		{
			name:         "edition with disc info",
			filename:     "RPG Edition (Disc 1 of 2)(USA).iso",
			wantContains: []string{"edition:edition", "media:disc", "disc:1", "disctotal:2", "region:us"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFilenameToCanonicalTags(tt.filename)
			gotStrings := make([]string, len(got))
			for i, tag := range got {
				gotStrings[i] = tag.String()
			}

			for _, want := range tt.wantContains {
				assert.Contains(t, gotStrings, want, "Expected tag %s not found in %v", want, gotStrings)
			}
		})
	}
}
