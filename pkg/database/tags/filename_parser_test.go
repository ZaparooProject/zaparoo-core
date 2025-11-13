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
				{Type: TagTypeMedia, Value: TagMediaDisc, Source: TagSourceBracketed},
				{Type: TagTypeDisc, Value: TagDisc1, Source: TagSourceBracketed},
				{Type: TagTypeDiscTotal, Value: TagDiscTotal3, Source: TagSourceBracketed},
			},
			wantRemaining: "Final Fantasy VII (USA).bin",
		},
		{
			name:     "disc X of Y case insensitive",
			filename: "Game (DISC 2 OF 2)(Europe).iso",
			wantTags: []CanonicalTag{
				{Type: TagTypeMedia, Value: TagMediaDisc, Source: TagSourceBracketed},
				{Type: TagTypeDisc, Value: TagDisc2, Source: TagSourceBracketed},
				{Type: TagTypeDiscTotal, Value: TagDiscTotal2, Source: TagSourceBracketed},
			},
			wantRemaining: "Game (Europe).iso",
		},
		{
			name:     "revision tag",
			filename: "Sonic (Rev 1)(USA).md",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: TagRev1, Source: TagSourceBracketed},
			},
			wantRemaining: "Sonic (USA).md",
		},
		{
			name:     "revision tag with letter",
			filename: "Game (Rev A)(Japan).sfc",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: TagRevA, Source: TagSourceBracketed},
			},
			wantRemaining: "Game (Japan).sfc",
		},
		{
			name:     "version tag",
			filename: "Mario (v1.2)(USA).n64",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: "1-2", Source: TagSourceBracketed},
			},
			wantRemaining: "Mario (USA).n64",
		},
		{
			name:     "version tag with multiple dots",
			filename: "Game (v1.2.3)(Europe).gba",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: "1-2-3", Source: TagSourceBracketed},
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
				{Type: TagTypeMedia, Value: TagMediaDisc, Source: TagSourceBracketed},
				{Type: TagTypeDisc, Value: TagDisc1, Source: TagSourceBracketed},
				{Type: TagTypeDiscTotal, Value: TagDiscTotal3, Source: TagSourceBracketed},
				{Type: TagTypeRev, Value: "1-1", Source: TagSourceBracketed},
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
			name: "comma-separated three languages (No-Intro)",
			tag:  "En,Fr,De",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
			},
		},
		{
			name: "comma-separated two languages",
			tag:  "En,Fr",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
			},
		},
		{
			name: "plus-separated two languages (TOSEC)",
			tag:  "En+Fr",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
			},
		},
		{
			name: "plus-separated three languages",
			tag:  "En+De+Es",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangES, Source: TagSourceBracketed},
			},
		},
		{
			name: "plus-separated four languages",
			tag:  "En+Fr+De+It",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangIT, Source: TagSourceBracketed},
			},
		},
		{
			name: "dash-separated three languages",
			tag:  "en-fr-de",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
			},
		},
		{
			name: "dash-separated two languages",
			tag:  "en-fr",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
			},
		},
		{
			name: "dash-separated four languages",
			tag:  "en-fr-de-it",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangIT, Source: TagSourceBracketed},
			},
		},
		{
			name: "dash-separated uppercase",
			tag:  "EN-FR-DE",
			wantTags: []CanonicalTag{
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed},
			},
		},
		{
			name:    "single language not multi",
			tag:     "En",
			wantNil: true,
		},
		{
			name:    "dash but not language codes (should let parseMultiRegionTag handle)",
			tag:     "USA-Europe",
			wantNil: true,
		},
		{
			name:    "dash with single language returns nil",
			tag:     "en",
			wantNil: true,
		},
		{
			name:    "comma but not languages",
			tag:     "Test,Data",
			wantNil: true,
		},
		{
			name:    "plus but not languages",
			tag:     "Test+Data",
			wantNil: true,
		},
		{
			name:    "single language with plus returns nil",
			tag:     "En+Invalid",
			wantNil: true, // Only one valid language, so returns nil
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
				{Type: TagTypeDump, Value: TagDumpTranslated, Source: TagSourceBracketed},
			},
		},
		{
			name: "verified dump",
			tag:  "!",
			wantTags: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpVerified, Source: TagSourceBracketed},
			},
		},
		{
			name: "bad dump",
			tag:  "b",
			wantTags: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpBad, Source: TagSourceBracketed},
			},
		},
		{
			name: "hacked",
			tag:  "h",
			wantTags: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpHacked, Source: TagSourceBracketed},
			},
		},
		{
			name: "fixed",
			tag:  "f",
			wantTags: []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpFixed, Source: TagSourceBracketed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ParseContext{
				CurrentTag:         tt.tag,
				CurrentBracketType: BracketTypeSquare,
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
			processedTags: []CanonicalTag{{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceBracketed}},
			position:      1,
			wantTags:      []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionCH, Source: TagSourceBracketed}},
		},
		{
			name:          "ch without context is Chinese",
			tag:           "ch",
			processedTags: []CanonicalTag{},
			position:      2,
			wantTags:      []CanonicalTag{{Type: TagTypeLang, Value: TagLangZH, Source: TagSourceBracketed}},
		},
		{
			name:          "ch early position is region",
			tag:           "ch",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionCH, Source: TagSourceBracketed}},
		},
		{
			name:          "tr early is region",
			tag:           "tr",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionTR, Source: TagSourceBracketed}},
		},
		{
			name:          "tr with region already is language",
			tag:           "tr",
			processedTags: []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionUS, Source: TagSourceBracketed}},
			position:      1,
			wantTags:      []CanonicalTag{{Type: TagTypeLang, Value: TagLangTR, Source: TagSourceBracketed}},
		},
		{
			name:          "bs is always Bosnian language",
			tag:           "bs",
			processedTags: []CanonicalTag{},
			position:      0,
			wantTags:      []CanonicalTag{{Type: TagTypeLang, Value: TagLangBS, Source: TagSourceBracketed}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ParseContext{
				CurrentTag:         tt.tag,
				CurrentBracketType: BracketTypeParen,
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
			wantTags: []string{"rev:1-2", "region:eu"},
		},
		{
			name:     "multi-language comma-separated (No-Intro)",
			filename: "Game (En,Fr,De)(Europe)[!].gba",
			wantTags: []string{"lang:en", "lang:fr", "lang:de", "region:eu", "dump:verified"},
		},
		{
			name:     "multi-language plus-separated (TOSEC)",
			filename: "Game (En+Fr)(Europe)[!].gba",
			wantTags: []string{"lang:en", "lang:fr", "region:eu", "dump:verified"},
		},
		{
			name:     "multi-language dash-separated in square brackets",
			filename: "Game (USA)[en-fr-de][!].gba",
			wantTags: []string{"region:us", "lang:en", "lang:fr", "lang:de", "dump:verified"},
		},
		{
			name:     "multi-language dash-separated in parentheses",
			filename: "Game (en-de-fr)(Europe).rom",
			wantTags: []string{"lang:en", "lang:de", "lang:fr", "region:eu"},
		},
		{
			name:     "multi-language dash-separated two languages",
			filename: "Game [en-fr](World).iso",
			wantTags: []string{"lang:en", "lang:fr", "region:world"},
		},
		{
			name:     "multi-language plus four languages",
			filename: "Game (En+Fr+De+It)(World).iso",
			wantTags: []string{"lang:en", "lang:fr", "lang:de", "lang:it", "region:world"},
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
			wantTags: []string{"rev:1-2", "region:us", "lang:en", "dump:verified"},
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
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceInferred},
			},
			wantRemaining: "Final Fantasy V smc",
		},
		{
			name:     "T-Ger older translation",
			filename: "Secret of Mana T-Ger.sfc",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslationOld, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangDE, Source: TagSourceInferred},
			},
			wantRemaining: "Secret of Mana sfc",
		},
		{
			name:     "TFre generic translation",
			filename: "Chrono Trigger TFre.smc",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangFR, Source: TagSourceInferred},
			},
			wantRemaining: "Chrono Trigger smc",
		},
		{
			name:     "T+Eng v1.0 with version",
			filename: "Fire Emblem T+Eng v1.0.gba",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceInferred},
				{Type: TagTypeRev, Value: "1-0", Source: TagSourceInferred},
			},
			wantRemaining: "Fire Emblem gba",
		},
		{
			name:     "T+Spa v2.1.3 with multi-part version",
			filename: "Mother 3 T+Spa v2.1.3.gba",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangES, Source: TagSourceInferred},
				{Type: TagTypeRev, Value: "2-1-3", Source: TagSourceInferred},
			},
			wantRemaining: "Mother 3 gba",
		},
		{
			name:     "T+Ita Italian translation",
			filename: "Pokemon Ruby T+Ita.gba",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangIT, Source: TagSourceInferred},
			},
			wantRemaining: "Pokemon Ruby gba",
		},
		{
			name:     "T+Rus Russian translation",
			filename: "Zelda T+Rus v1.5.nes",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangRU, Source: TagSourceInferred},
				{Type: TagTypeRev, Value: "1-5", Source: TagSourceInferred},
			},
			wantRemaining: "Zelda nes",
		},
		{
			name:     "T+Por Portuguese translation",
			filename: "Final Fantasy VI T+Por.smc",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangPT, Source: TagSourceInferred},
			},
			wantRemaining: "Final Fantasy VI smc",
		},
		{
			name:     "translation with other tags",
			filename: "Game (USA) T+Eng v2.0 [!].rom",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceInferred},
				{Type: TagTypeRev, Value: "2-0", Source: TagSourceInferred},
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
				{Type: TagTypeRev, Value: "1-0", Source: TagSourceInferred},
			},
			wantRemaining: "Game Name .rom",
		},
		{
			name:     "standalone v2.1.3",
			filename: "Another Game v2.1.3.bin",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: "2-1-3", Source: TagSourceInferred},
			},
			wantRemaining: "Another Game .bin",
		},
		{
			name:     "v1 single digit",
			filename: "Old Game v1.smc",
			wantTags: []CanonicalTag{
				{Type: TagTypeRev, Value: "1", Source: TagSourceInferred},
			},
			wantRemaining: "Old Game .smc",
		},
		{
			name:     "version not extracted if translation already has it",
			filename: "Game T+Eng v1.0.rom",
			wantTags: []CanonicalTag{
				{Type: TagTypeUnlicensed, Value: TagUnlicensedTranslation, Source: TagSourceInferred},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceInferred},
				{Type: TagTypeRev, Value: "1-0", Source: TagSourceInferred},
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
				"rev:1-1",
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
				"rev:2-0",
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

func TestParseBracketedTranslation_FullPipeline(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		wantContains []string
	}{
		{
			name:     "[T+Chi] Chinese translation with Big5 encoding - real world bug",
			filename: "Final Fantasy VI (J) [T+Chi(Big5)100_Kuyagi].smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:zh",
			},
		},
		{
			name:     "[T+Eng] English translation",
			filename: "Final Fantasy V (J) [T+Eng].smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:en",
			},
		},
		{
			name:     "[T-Ger] Older German translation",
			filename: "Secret of Mana (J) [T-Ger].sfc",
			wantContains: []string{
				"unlicensed:translation:old",
				"lang:de",
			},
		},
		{
			name:     "[TFre] French translation without prefix",
			filename: "Chrono Trigger (J) [TFre].smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:fr",
			},
		},
		{
			name:     "[T+Eng v1.0] Translation with version",
			filename: "Fire Emblem (J) [T+Eng v1.0].gba",
			wantContains: []string{
				"unlicensed:translation",
				"lang:en",
				"rev:1-0",
			},
		},
		{
			name:     "[T+Spa v2.1.3] Translation with multi-part version",
			filename: "Mother 3 (J) [T+Spa v2.1.3].gba",
			wantContains: []string{
				"unlicensed:translation",
				"lang:es",
				"rev:2-1-3",
			},
		},
		{
			name:     "[T+Rus_v1.5] Translation with underscore version",
			filename: "Zelda (J) [T+Rus_v1.5].nes",
			wantContains: []string{
				"unlicensed:translation",
				"lang:ru",
				"rev:1-5",
			},
		},
		{
			name:     "[T+Por] Portuguese translation",
			filename: "Final Fantasy VI (J) [T+Por].smc",
			wantContains: []string{
				"unlicensed:translation",
				"lang:pt",
			},
		},
		{
			name:     "Multiple bracket tags with translation",
			filename: "Game (USA) [T+Eng] [!].rom",
			wantContains: []string{
				"unlicensed:translation",
				"lang:en",
				"region:us",
				"dump:verified",
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
			wantContains: []string{"rev:1-0"},
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
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "English edition",
			filename: "Game Edition (USA).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		{
			name:     "version before parentheses",
			filename: "Deluxe Version (USA).bin",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "edition before brackets",
			filename: "Ultimate Edition [!].iso",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// German
		{
			name:     "German ausgabe",
			filename: "Spiel Ausgabe (Germany).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// Italian
		{
			name:     "Italian versione",
			filename: "Gioco Versione (Italy).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "Italian edizione",
			filename: "Gioco Edizione (Italy).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// Portuguese
		{
			name:     "Portuguese versao",
			filename: "Jogo Versao (Brazil).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "Portuguese edicao",
			filename: "Jogo Edicao (Brazil).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// Japanese
		{
			name:     "Japanese version (バージョン)",
			filename: "ゲーム バージョン (Japan).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "Japanese version (ヴァージョン)",
			filename: "ゲーム ヴァージョン (Japan).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "Japanese edition (エディション)",
			filename: "ゲーム エディション (Japan).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// Case insensitivity
		{
			name:     "VERSION uppercase",
			filename: "Game VERSION (USA).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "Edition mixed case",
			filename: "Game EdItIoN (USA).rom",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
		},
		// At end of filename
		{
			name:     "version at end",
			filename: "Special Version",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionVersion, Source: TagSourceInferred}},
		},
		{
			name:     "edition at end",
			filename: "Limited Edition",
			wantTags: []CanonicalTag{{Type: TagTypeEdition, Value: TagEditionEdition, Source: TagSourceInferred}},
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
			wantContains: []string{"edition:version", "rev:1-2", "region:us"},
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

// TestParseTitleFromFilename_SceneReleases tests scene release artifact stripping from movie and TV show filenames.
func TestParseTitleFromFilename_SceneReleases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Movie scene releases
		{
			name:     "Movie with full scene release tags",
			input:    "The.Dark.Knight.2008.1080p.BluRay.x264.DTS-GROUP.mkv",
			expected: "The Dark Knight 2008",
		},
		{
			name:     "Movie with WEB-DL and audio codec",
			input:    "Avatar.2009.2160p.WEB-DL.DD5.1.H264-TEAM.mkv",
			expected: "Avatar 2009",
		},
		{
			name:     "Movie with HDR and HEVC",
			input:    "Blade.Runner.1982.4K.UHD.HDR10.x265-GROUP.mkv",
			expected: "Blade Runner 1982",
		},
		{
			name:     "Movie with PROPER tag",
			input:    "Inception.2010.1080p.BluRay.PROPER.x264-SPARKS.mkv",
			expected: "Inception 2010",
		},
		{
			name:     "Movie with extended cut and codec",
			input:    "Lord.of.the.Rings.2001.EXTENDED.1080p.BluRay.x264.TrueHD-FGT.mkv",
			expected: "Lord of the Rings 2001",
		},
		{
			name:     "Movie with Dolby Vision and Atmos",
			input:    "Dune.2021.2160p.WEB-DL.DV.HDR10.Atmos.HEVC-CMRG.mkv",
			expected: "Dune 2021",
		},
		{
			name:     "Movie with underscores",
			input:    "The_Matrix_1999_1080p_BluRay_x264_AAC-YTS.mkv",
			expected: "The Matrix 1999",
		},

		// TV show scene releases
		{
			name:     "TV show with episode and quality",
			input:    "The.Office.US.S01E01.Pilot.1080p.WEB-DL.DD5.1.H264-GROUP.mkv",
			expected: "The Office US Pilot",
		},
		{
			name:     "TV show with season pack notation",
			input:    "Breaking.Bad.S05E16.1080p.BluRay.x264-ROVERS.mkv",
			expected: "Breaking Bad",
		},
		{
			name:     "TV show with HDTV source",
			input:    "Game.of.Thrones.S08E03.720p.HDTV.x264-AVS.mkv",
			expected: "Game of Thrones",
		},
		{
			name:     "TV show with PROPER and WEBRip",
			input:    "Stranger.Things.S01E01.PROPER.720p.WEBRip.x264-RARBG.mkv",
			expected: "Stranger Things",
		},

		// Edge cases
		{
			name:     "ROM filename should be unchanged",
			input:    "Super Mario Bros (USA) [!].sfc",
			expected: "Super Mario Bros",
		},
		{
			name:     "Game with year shouldn't strip it",
			input:    "Elden Ring (2022).exe",
			expected: "Elden Ring 2022",
		},
		{
			name:     "Filename with spaces (no scene tags)",
			input:    "My Movie Title.mkv",
			expected: "My Movie Title",
		},
		{
			name:     "Multiple quality indicators",
			input:    "Movie.2020.UHD.2160p.HDR.BluRay.x265-GRP.mkv",
			expected: "Movie 2020",
		},
		{
			name:     "Release group without hyphen",
			input:    "Movie.Title.2010.1080p.BluRay.x264.mkv",
			expected: "Movie Title 2010",
		},
		{
			name:     "Mixed case scene tags",
			input:    "Show.Name.s02e05.WEBDL.h264.AAC.mkv",
			expected: "Show Name",
		},

		// Critical edge cases from expert review
		{
			name:     "Movie titled 'Cam' should not have title stripped",
			input:    "Cam.2018.1080p.WEB-DL.x264-GROUP.mkv",
			expected: "Cam 2018",
		},
		{
			name:     "Movie titled 'TS' should not have title stripped",
			input:    "TS.2020.720p.BluRay.x264.mkv",
			expected: "TS 2020",
		},
		{
			name:     "Short title before year should be preserved",
			input:    "IT.2017.1080p.BluRay.x264-SPARKS.mkv",
			expected: "IT 2017",
		},
		{
			name:     "Game title with dash and number should be preserved",
			input:    "Mega-Man-X-4.sfc",
			expected: "Mega Man X 4",
		},
		{
			name:     "Game title ending in single letter-number should be preserved",
			input:    "F-Zero-X.n64",
			expected: "F Zero X",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseTitleFromFilename(tt.input, false)
			assert.Equal(t, tt.expected, got, "Title parsing mismatch")
		})
	}
}

// TestExtractSpecialPatterns_MediaMetadata tests extraction of TV show, comic, and music metadata.
func TestExtractSpecialPatterns_MediaMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filename     string
		wantContains []string
		wantNotFound []string
	}{
		// TV show patterns
		{
			name:         "TV show season and episode lowercase",
			filename:     "Breaking.Bad.s05e16.Felina.mkv",
			wantContains: []string{"season:5", "episode:16"},
		},
		{
			name:         "TV show season and episode uppercase",
			filename:     "Game.of.Thrones.S08E03.The.Long.Night.mkv",
			wantContains: []string{"season:8", "episode:3"},
		},
		{
			name:         "TV show with quality tags",
			filename:     "The.Office.S01E01.1080p.WEB-DL.mkv",
			wantContains: []string{"season:1", "episode:1"},
		},
		{
			name:         "TV show double-digit season",
			filename:     "Doctor.Who.S12E10.mkv",
			wantContains: []string{"season:12", "episode:10"},
		},
		{
			name:         "TV show triple-digit episode",
			filename:     "One.Piece.S01E125.mkv",
			wantContains: []string{"season:1", "episode:125"},
		},

		// Comic patterns
		{
			name:         "Comic with hash issue number",
			filename:     "Amazing Spider-Man #47.cbr",
			wantContains: []string{"issue:47"},
		},
		{
			name:         "Comic with Issue keyword",
			filename:     "Batman Issue 100.cbr",
			wantContains: []string{"issue:100"},
		},
		{
			name:         "Comic with No. format",
			filename:     "Superman No. 25.cbz",
			wantContains: []string{"issue:25"},
		},
		{
			name:         "Comic with leading zeros",
			filename:     "X-Men #001.cbr",
			wantContains: []string{"issue:1"},
		},

		// Music patterns
		{
			name:         "Music track with dash separator",
			filename:     "01 - Song Title.mp3",
			wantContains: []string{"track:1"},
		},
		{
			name:         "Music track with dot separator",
			filename:     "02. Artist - Song Name.flac",
			wantContains: []string{"track:2"},
		},
		{
			name:         "Music track with space separator",
			filename:     "03 Artist Name.mp3",
			wantContains: []string{"track:3"},
		},
		{
			name:         "Music track with Track keyword",
			filename:     "Track 05 - Album Name.wav",
			wantContains: []string{"track:5"},
		},
		{
			name:         "Music track triple digit",
			filename:     "125 - Song Title.mp3",
			wantContains: []string{"track:125"},
		},

		// Edge cases and combinations
		{
			name:         "TV show with year",
			filename:     "The Office (2005) S01E01.mkv",
			wantContains: []string{"year:2005", "season:1", "episode:1"},
		},
		{
			name:         "ROM with revision (should not extract as season)",
			filename:     "Game (USA) (Rev A).sfc",
			wantContains: []string{"region:us", "rev:a"},
			wantNotFound: []string{"season:", "episode:"},
		},
		{
			name:         "Year 1942 should not be track number",
			filename:     "1942 (USA).zip",
			wantContains: []string{"region:us"},
			wantNotFound: []string{"track:1942"},
		},

		// Critical edge cases from expert review
		{
			name:         "4-digit year in music filename should not be parsed as track",
			filename:     "1985.mp3",
			wantContains: []string{},
			wantNotFound: []string{"track:198", "track:1985"},
		},
		{
			name:         "Year at start should not be track",
			filename:     "2024 Song Title.flac",
			wantContains: []string{},
			wantNotFound: []string{"track:202"},
		},
		{
			name:         "Volume number with Vol. keyword",
			filename:     "Book Title (Vol. 2).epub",
			wantContains: []string{"volume:2"},
		},
		{
			name:         "Volume number with Volume keyword",
			filename:     "Comic Series (Volume 3).cbr",
			wantContains: []string{"volume:3"},
		},
		{
			name:         "Volume number with leading zeros",
			filename:     "Series Name (Vol. 001).cbz",
			wantContains: []string{"volume:1"},
		},
		{
			name:         "Bare v pattern should be version not volume",
			filename:     "Game (v01) (USA).sfc",
			wantContains: []string{"rev:01", "region:us"},
			wantNotFound: []string{"volume:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseFilenameToCanonicalTags(tt.filename)
			gotStrings := make([]string, len(got))
			for i, tag := range got {
				gotStrings[i] = tag.String()
			}

			// Check for expected tags
			for _, want := range tt.wantContains {
				assert.Contains(t, gotStrings, want, "Expected tag %s not found in %v", want, gotStrings)
			}

			// Check that unwanted tags are not present
			for _, notWant := range tt.wantNotFound {
				for _, gotTag := range gotStrings {
					assert.NotContains(t, gotTag, notWant,
						"Unexpected tag pattern %s found in tag %s", notWant, gotTag)
				}
			}
		})
	}
}

// TestStripSceneArtifacts tests the scene artifact stripping function directly.
func TestStripSceneArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Strip resolution",
			input:    "Movie Title 1080p mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip source type",
			input:    "Movie Title BluRay mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip video codec",
			input:    "Movie Title x264 mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip audio codec",
			input:    "Movie Title DTS mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip HDR format",
			input:    "Movie Title HDR10 mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip scene status",
			input:    "Movie Title PROPER mkv",
			expected: "Movie Title   mkv",
		},
		{
			name:     "Strip release group",
			input:    "Movie Title-GROUP",
			expected: "Movie Title",
		},
		{
			name:     "Strip all artifacts",
			input:    "Movie 2020 1080p BluRay x264 DTS HDR10-GROUP",
			expected: "Movie 2020          ",
		},
		{
			name:     "Leave non-scene text alone",
			input:    "Regular Movie Title mkv",
			expected: "Regular Movie Title mkv",
		},
		{
			name:     "Case insensitive matching",
			input:    "Movie webdl H265 aac mkv",
			expected: "Movie       mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripSceneArtifacts(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
