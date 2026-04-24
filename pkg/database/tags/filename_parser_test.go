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

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
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
			name:     "No-Intro disc 1 without total",
			filename: "Final Fantasy VII (USA) (Disc 1).chd",
			wantTags: []string{"region:us", "lang:en", "media:disc", "disc:1"},
		},
		{
			name:     "No-Intro disc 2 without total",
			filename: "Final Fantasy VII (USA) (Disc 2).chd",
			wantTags: []string{"region:us", "lang:en", "media:disc", "disc:2"},
		},
		{
			name:     "No-Intro disc 4 without total",
			filename: "Final Fantasy IX (USA) (Disc 4) (Rev 1).chd",
			wantTags: []string{"rev:1", "region:us", "lang:en", "media:disc", "disc:4"},
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
		{
			name:     "single language in square brackets",
			filename: "Game [en].rom",
			wantTags: []string{"lang:en"},
		},
		{
			name:     "single language de in square brackets",
			filename: "Game [de].rom",
			wantTags: []string{"lang:de"},
		},
		{
			name:     "single language fr in square brackets",
			filename: "Game [fr].rom",
			wantTags: []string{"lang:fr"},
		},
		{
			name:     "language in square brackets with region in parentheses",
			filename: "Game (USA)[en].rom",
			wantTags: []string{"region:us", "lang:en"},
		},
		{
			name:     "language and dump tag in square brackets",
			filename: "Game (Japan)[ja][!].rom",
			wantTags: []string{"region:jp", "lang:ja", "dump:verified"},
		},
		// TOSEC positional publisher detection
		{
			name:     "TOSEC year-then-publisher produces publisher",
			filename: "Legend of TOSEC, The (1986)(DevStudio)[!].rom",
			wantTags: []string{"year:1986", "publisher:devstudio", "dump:verified"},
		},
		{
			name:     "TOSEC year-then-publisher with region flags produces publisher",
			filename: "Game, The (1993)(Konami)[!][US].rom",
			wantTags: []string{"year:1993", "publisher:konami", "dump:verified"},
		},
		{
			// Pre-1970 TOSEC files (early computing era) — year within 1950-2099 range
			name:     "TOSEC pre-1970 year produces publisher",
			filename: "ELIZA (1966)(MIT)[!].rom",
			wantTags: []string{"year:1966", "publisher:mit", "dump:verified"},
		},
		// Generic credit detection
		{
			name:     "Unknown company name in parens promotes to credit",
			filename: "Super Metroid (Nintendo R&D1) (USA).smc",
			wantTags: []string{"credit:nintendo-r-and-d1", "region:us"},
		},
		{
			name:     "Short company abbreviation promotes to credit",
			filename: "Game (CRL) (USA).rom",
			wantTags: []string{"credit:crl", "region:us"},
		},
		// Edition phrase detection
		{
			name:     "Special Edition produces edition:special",
			filename: "Game (Special Edition) (USA).rom",
			wantTags: []string{"edition:special", "region:us"},
		},
		{
			name:     "Collector's Edition produces edition:collectors",
			filename: "Game (Collector's Edition).rom",
			wantTags: []string{"edition:collectors"},
		},
		{
			name:     "VGA Remake produces edition:remake (unrecognized qualifier falls back to generic)",
			filename: "Game (VGA Remake).rom",
			wantTags: []string{"edition:remake"},
		},
		{
			name:     "Director's Cut produces edition:directors-cut",
			filename: "Game (The Director's Cut).rom",
			wantTags: []string{"edition:directors-cut"},
		},
		// Additional edition cases
		{
			name:     "Dreambor Edition falls back to edition:edition",
			filename: "Game (Dreambor Edition).rom",
			wantTags: []string{"edition:edition"},
		},
		{
			name:     "X-Mas Edition falls back to edition:edition",
			filename: "Game (X-Mas Edition).rom",
			wantTags: []string{"edition:edition"},
		},
		{
			name:     "GOTY Edition produces edition:goty",
			filename: "Game (GOTY Edition).rom",
			wantTags: []string{"edition:goty"},
		},
		{
			name:     "GOTY standalone produces edition:goty",
			filename: "Game (GOTY).rom",
			wantTags: []string{"edition:goty"},
		},
		{
			name:     "Game of the Year standalone produces edition:goty",
			filename: "Game (Game of the Year).rom",
			wantTags: []string{"edition:goty"},
		},
		{
			name:     "Deluxe Edition produces edition:deluxe",
			filename: "Game (Deluxe Edition).rom",
			wantTags: []string{"edition:deluxe"},
		},
		{
			name:     "Director's Cut without leading The produces edition:directors-cut",
			filename: "Game (Director's Cut).rom",
			wantTags: []string{"edition:directors-cut"},
		},
		// Release detection
		{
			name:     "PS1 Classics produces release:classics",
			filename: "Game (PS1 Classics).rom",
			wantTags: []string{"release:classics"},
		},
		{
			name:     "Homebrew mapped to release:homebrew",
			filename: "Game (Homebrew).rom",
			wantTags: []string{"release:homebrew"},
		},
		{
			name:     "Public Domain mapped to release:public-domain",
			filename: "Game (Public Domain).rom",
			wantTags: []string{"release:public-domain"},
		},
		// Language full-name aliases
		{
			name:     "German maps to lang:de",
			filename: "Game (German).rom",
			wantTags: []string{"lang:de"},
		},
		{
			name:     "French maps to lang:fr",
			filename: "Game (French).rom",
			wantTags: []string{"lang:fr"},
		},
		// Version phrase skip
		{
			name:     "Version phrase produces no tag",
			filename: "Game (Version 2) (USA).rom",
			wantTags: []string{"region:us"},
		},
		// Catalog number skip
		{
			name:     "Catalog number produces no tag",
			filename: "Game (KD02) (USA).rom",
			wantTags: []string{"region:us"},
		},
		// No-Intro (no dev/pub in filename)
		{
			name:     "No-Intro format produces no publisher or credit",
			filename: "Super Mario World (USA).sfc",
			wantTags: []string{"region:us", "lang:en"},
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

			// Negative assertions: no-Intro filenames should not produce credit:/publisher: tags
			if tt.name == "No-Intro format produces no publisher or credit" {
				for _, tagStr := range gotStrings {
					assert.False(t, strings.HasPrefix(tagStr, "credit:"),
						"No-Intro format should not produce credit: tag, got %q", tagStr)
					assert.False(t, strings.HasPrefix(tagStr, "publisher:"),
						"No-Intro format should not produce publisher: tag, got %q", tagStr)
				}
			}
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
			name:          "TFre without +/- should NOT match (regression: avoid false positives)",
			filename:      "Chrono Trigger TFre.smc",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Chrono Trigger TFre.smc",
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
		{
			name:          "FTL should not match translation tag (regression test)",
			filename:      "FTL: Faster Than Light",
			wantTags:      []CanonicalTag{},
			wantRemaining: "FTL: Faster Than Light",
		},
		{
			name:          "The Legend should not match translation tag",
			filename:      "The Legend of Zelda",
			wantTags:      []CanonicalTag{},
			wantRemaining: "The Legend of Zelda",
		},
		{
			name:          "Tony Hawk should not match translation tag",
			filename:      "Tony Hawk's Pro Skater",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Tony Hawk's Pro Skater",
		},
		{
			name:          "Team Fortress should not match translation tag",
			filename:      "Team Fortress 2",
			wantTags:      []CanonicalTag{},
			wantRemaining: "Team Fortress 2",
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

func TestParseFilenameToCanonicalTagsForMedia_GameSkipsTVButKeepsRomTranslationPatterns(t *testing.T) {
	t.Parallel()

	filename := "Game Title S01E02 (USA) [T+Eng].smc"

	defaultTags := ParseFilenameToCanonicalTags(filename)
	defaultStrings := make([]string, len(defaultTags))
	for i, tag := range defaultTags {
		defaultStrings[i] = tag.String()
	}
	assert.Contains(t, defaultStrings, "season:1")
	assert.Contains(t, defaultStrings, "episode:2")
	assert.Contains(t, defaultStrings, "unlicensed:translation")
	assert.Contains(t, defaultStrings, "lang:en")

	gameTags := ParseFilenameToCanonicalTagsForMedia(filename, slugs.MediaTypeGame)
	gameStrings := make([]string, len(gameTags))
	for i, tag := range gameTags {
		gameStrings[i] = tag.String()
	}
	assert.NotContains(t, gameStrings, "season:1")
	assert.NotContains(t, gameStrings, "episode:2")
	assert.Contains(t, gameStrings, "unlicensed:translation")
	assert.Contains(t, gameStrings, "lang:en")
	assert.Contains(t, gameStrings, "region:us")
}

func TestParseFilenameToCanonicalTagsForMedia_NonGameSkipsRomTranslationLanguageTags(t *testing.T) {
	t.Parallel()

	filename := "Game Title [T+Eng].smc"

	defaultTags := ParseFilenameToCanonicalTags(filename)
	defaultStrings := make([]string, len(defaultTags))
	for i, tag := range defaultTags {
		defaultStrings[i] = tag.String()
	}
	assert.Contains(t, defaultStrings, "unlicensed:translation")
	assert.Contains(t, defaultStrings, "lang:en")

	nonGameTags := ParseFilenameToCanonicalTagsForMedia(filename, slugs.MediaTypeMovie)
	nonGameStrings := make([]string, len(nonGameTags))
	for i, tag := range nonGameTags {
		nonGameStrings[i] = tag.String()
	}
	assert.NotContains(t, nonGameStrings, "unlicensed:translation")
	assert.NotContains(t, nonGameStrings, "lang:en")
}

func TestParseBracesAndAngles_FullPipeline(t *testing.T) {
	tests := []struct {
		name            string
		filename        string
		wantTags        []CanonicalTag
		wantContains    []string
		wantNotContains []string
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
		{
			name:         "TOSEC Final dev-status",
			filename:     "The Fall (Final)(2018)(Disk 2 of 2).adf",
			wantContains: []string{"unfinished:final", "year:2018", "media:disc", "disc:2", "disctotal:2"},
		},
		{
			name:         "TOSEC Final without year",
			filename:     "Game (Final)(1997).rom",
			wantContains: []string{"unfinished:final"},
		},
		{
			name:     "Disk N of M with Side A - publisher from TOSEC slot",
			filename: "Alter Ego (1985)(Activision)(Disk 1 of 3 Side A)[cr].do",
			wantContains: []string{
				"publisher:activision", "media:disc", "disc:1", "disctotal:3", "media:side-a", "dump:cracked",
			},
		},
		{
			name:         "Disk N of M with Side A no publisher",
			filename:     "Game (Disk 1 of 3 Side A).do",
			wantContains: []string{"disc:1", "disctotal:3", "media:side-a"},
		},
		{
			name:         "Disc N of M with Side B",
			filename:     "Game (Disc 2 of 2 Side B)(USA).iso",
			wantContains: []string{"media:disc", "disc:2", "disctotal:2", "media:side-b", "region:us"},
		},
		// Bug: (Side A) standalone was split into "side"→media:side + "a"→dump:alternate.
		{
			name:     "TOSEC standalone Side A - publisher from TOSEC slot",
			filename: "Barbarian (1987)(Palace Software)(Side A)[u].tap",
			wantContains: []string{
				"publisher:palace-software", "year:1987", "media:side-a", "dump:underdump",
			},
		},
		{
			name:         "TOSEC standalone Side B - publisher from TOSEC slot",
			filename:     "Batman - The Movie (1989)(Ocean)(Side B)[u].tap",
			wantContains: []string{"publisher:ocean", "year:1989", "media:side-b", "dump:underdump"},
		},
		// Bug: (Russia) was falling through to credit:russia.
		{
			name:         "Russia full-name region",
			filename:     "Adventures of the Gummi Bears (Russia) (Unl).md",
			wantContains: []string{"region:ru", "lang:ru", "unlicensed:unlicensed"},
		},
		// Bug: GoodTools (U)/(E)/(J)/(A) paren codes mapped to dump tags instead of regions.
		{
			name:         "GoodTools (U) USA",
			filename:     "Legend of Zelda, The - Ocarina of Time (U) (V1.2) [!].z64",
			wantContains: []string{"region:us", "lang:en", "rev:1-2", "dump:verified"},
		},
		{
			name:         "GoodTools (J) Japan",
			filename:     "Super Mario 64 (J) [!].z64",
			wantContains: []string{"region:jp", "lang:ja", "dump:verified"},
		},
		{
			name:         "GoodTools (E) Europe",
			filename:     "Some Game (E) [!].z64",
			wantContains: []string{"region:eu", "dump:verified"},
		},
		{
			name:         "GoodTools (A) Australia",
			filename:     "Some Game (A) [!].z64",
			wantContains: []string{"region:au", "lang:en", "dump:verified"},
		},
		// Regression: square-bracket [u] and [a] must still produce dump tags.
		{
			name:         "TOSEC [u] underdump in brackets",
			filename:     "Some File [u].z64",
			wantContains: []string{"dump:underdump"},
		},
		{
			name:         "TOSEC [a] alternate in brackets",
			filename:     "Some File [a].z64",
			wantContains: []string{"dump:alternate"},
		},
		// Bug: (GameCube) was tagged as credit:gamecube instead of distribution:gamecube.
		{
			name:     "GameCube distribution provenance",
			filename: "Legend of Zelda, The - Ocarina of Time - Master Quest (USA) (GameCube).z64",
			wantContains: []string{
				"region:us", "lang:en", "distribution:gamecube",
			},
		},
		// Bug: (Disk Writer) was falling through to credit:disk-writer.
		{
			name:         "FDS Disk Writer kiosk distribution",
			filename:     "Super Mario Bros. (Japan) (Disk Writer).fds",
			wantContains: []string{"region:jp", "distribution:disk-writer"},
		},
		// Bug: VC phrase variants were falling through to credit:*.
		{
			name:         "Wii Virtual Console phrase",
			filename:     "Zelda no Densetsu - The Hyrule Fantasy (Japan) (Wii Virtual Console).fds",
			wantContains: []string{"region:jp", "distribution:virtual-console"},
		},
		{
			name:         "Wii and Wii U Virtual Console phrase",
			filename:     "Metroid (World) (Wii and Wii U Virtual Console).fds",
			wantContains: []string{"distribution:virtual-console"},
		},
		{
			name:         "3DS Virtual Console phrase",
			filename:     "Some Game (World) (3DS Virtual Console).nes",
			wantContains: []string{"distribution:virtual-console"},
		},
		// Bug: (Switch Online) was falling through to credit:switch-online.
		{
			name:         "Switch Online distribution",
			filename:     "Sonic the Hedgehog (Japan) (Switch Online).md",
			wantContains: []string{"region:jp", "distribution:switch-online"},
		},
		// Bug: (Promo) was falling through to credit:promo.
		{
			name:         "Promo release",
			filename:     "Some Game (USA) (Promo).nes",
			wantContains: []string{"region:us", "release:promo"},
		},
		// Bug: full-name region countries (taiwan/argentina/mexico/scandinavia) were credit:*.
		{
			name:            "Taiwan full-name region",
			filename:        "Zaxxon (Taiwan) (En) (Unl).col",
			wantContains:    []string{"region:tw", "lang:zh"},
			wantNotContains: []string{"credit:taiwan"},
		},
		{
			name:            "Argentina full-name region",
			filename:        "Futbol Argentino (Argentina) (Pirate).md",
			wantContains:    []string{"region:ar", "lang:es"},
			wantNotContains: []string{"credit:argentina"},
		},
		{
			name:            "Mexico full-name region",
			filename:        "Chavez II (Mexico).md",
			wantContains:    []string{"region:mx", "lang:es"},
			wantNotContains: []string{"credit:mexico"},
		},
		{
			name:            "Scandinavia region",
			filename:        "Devil World (Scandinavia) (En).nes",
			wantContains:    []string{"region:scandinavia"},
			wantNotContains: []string{"credit:scandinavia"},
		},
		// Bug: (prototype) full word was credit:prototype instead of unfinished:proto.
		{
			name:            "Prototype full word",
			filename:        "Clockwork Aquario (prototype).mra",
			wantContains:    []string{"unfinished:proto"},
			wantNotContains: []string{"credit:prototype"},
		},
		// Bug: (Not For Resale) was credit:not-for-resale.
		{
			name:            "Not For Resale release",
			filename:        "Yasuda Fire & Marine - Safety Rally (Japan) (Not for Resale).nes",
			wantContains:    []string{"region:jp", "release:not-for-resale"},
			wantNotContains: []string{"credit:not-for-resale"},
		},
		// Bug: (Kiosk) was credit:kiosk.
		{
			name:            "Kiosk release",
			filename:        "DK - King of Swing (USA) (Demo) (Kiosk).gba",
			wantContains:    []string{"region:us", "unfinished:demo", "release:kiosk"},
			wantNotContains: []string{"credit:kiosk"},
		},
		// Bug: (Steam) was credit:steam.
		{
			name:            "Steam distribution",
			filename:        "Sonic & Knuckles + Sonic The Hedgehog 3 (Japan) (En) (Steam).md",
			wantContains:    []string{"region:jp", "distribution:steam"},
			wantNotContains: []string{"credit:steam"},
		},
		// Bug: (GBC) on non-GB files was credit:gbc instead of compatibility:gameboy:color.
		{
			name:            "GBC compatibility",
			filename:        "Some Music (GBC).nsf",
			wantContains:    []string{"compatibility:gameboy:color"},
			wantNotContains: []string{"credit:gbc"},
		},
		// Bug: (DSI) on GBA files was credit:dsi instead of compatibility:dsi.
		{
			name:            "DSI compatibility",
			filename:        "Polly Pocket! - Super Splash Island (Europe) (En,Fr,De,Es,It) (DSI).gba",
			wantContains:    []string{"region:eu", "compatibility:dsi"},
			wantNotContains: []string{"credit:dsi"},
		},
		// Bug: distribution service/mini console tags were credit:*.
		{
			name:            "Sega Channel distribution",
			filename:        "Game no Kanzume Otokuyou (Japan) (Sega Channel).md",
			wantContains:    []string{"region:jp", "distribution:sega-channel"},
			wantNotContains: []string{"credit:sega-channel"},
		},
		{
			name:            "Genesis Mini distribution",
			filename:        "Mega Man - The Wily Wars (USA) (Genesis Mini).md",
			wantContains:    []string{"region:us", "distribution:genesis-mini"},
			wantNotContains: []string{"credit:genesis-mini"},
		},
		{
			name:            "Mega Drive Mini distribution (regional alias)",
			filename:        "Castlevania - The New Generation (Europe) (Mega Drive Mini).md",
			wantContains:    []string{"region:eu", "distribution:genesis-mini"},
			wantNotContains: []string{"credit:mega-drive-mini"},
		},
		{
			name:            "Sega Ages distribution",
			filename:        "Thunder Force IV (World) (Sega Ages).md",
			wantContains:    []string{"distribution:sega-ages"},
			wantNotContains: []string{"credit:sega-ages"},
		},
		{
			name:            "Sega Smash Pack distribution",
			filename:        "Golden Axe (World) (Rev B) (Sega Smash Pack).md",
			wantContains:    []string{"distribution:sega-smash-pack"},
			wantNotContains: []string{"credit:sega-smash-pack"},
		},
		{
			name:            "Wii distribution",
			filename:        "Super Mario All-Stars (Europe) (Wii).sfc",
			wantContains:    []string{"region:eu", "distribution:wii"},
			wantNotContains: []string{"credit:wii"},
		},
		{
			name:            "Club Nintendo distribution",
			filename:        "Some Game (USA) (Beta) (Club Nintendo).sfc",
			wantContains:    []string{"distribution:club-nintendo"},
			wantNotContains: []string{"credit:club-nintendo"},
		},
		// Bug: (United Kingdom) was credit:united-kingdom instead of region:gb.
		{
			name:            "United Kingdom region",
			filename:        "TG Rally 2 (United Kingdom).gbc",
			wantContains:    []string{"region:gb", "lang:en"},
			wantNotContains: []string{"credit:united-kingdom"},
		},
		// Bug: Game Boy compatibility markers on GBC/GB files were credit:*.
		{
			name:            "GB Compatible on GBC file",
			filename:        "Metal Walker (USA) (GB Compatible).gbc",
			wantContains:    []string{"region:us", "compatibility:gameboy"},
			wantNotContains: []string{"credit:gb-compatible"},
		},
		{
			name:            "SGB Enhanced on GB file",
			filename:        "Battle Arena Toshinden (USA) (SGB Enhanced).gb",
			wantContains:    []string{"region:us", "compatibility:gameboy:sgb"},
			wantNotContains: []string{"credit:sgb-enhanced"},
		},
		{
			name:            "CGB+SGB Enhanced on GB file",
			filename:        "Pokemon - Yellow Version - Special Pikachu Edition (USA, Europe) (CGB+SGB Enhanced).gb",
			wantContains:    []string{"compatibility:gameboy:color", "compatibility:gameboy:sgb"},
			wantNotContains: []string{"credit:cgb-plus-sgb-enhanced"},
		},
		{
			name:            "SG Enhanced on PC Engine file",
			filename:        "Darius Plus (Japan) (SG Enhanced).pce",
			wantContains:    []string{"region:jp", "compatibility:pcengine:supergrafx"},
			wantNotContains: []string{"credit:sg-enhanced"},
		},
		{
			name:            "GBA e-Reader distribution",
			filename:        "Ice Climber (U) (GBA e-Reader).nes",
			wantContains:    []string{"distribution:gba-e-reader"},
			wantNotContains: []string{"credit:gba-e-reader"},
		},
		{
			name:            "Playable Demo is unfinished demo",
			filename:        "Kill Barney In Tokyo (1997 Daniel Bienvenu) (Playable Demo).col",
			wantContains:    []string{"unfinished:demo"},
			wantNotContains: []string{"credit:playable-demo"},
		},
		{
			name:            "Taikenban Sample ROM is unfinished demo",
			filename:        "Seiken Densetsu 3 (Japan) (Taikenban Sample ROM).sfc",
			wantContains:    []string{"region:jp", "unfinished:demo"},
			wantNotContains: []string{"credit:taikenban-sample-rom"},
		},
		{
			name:            "Bootleg is unlicensed:bootleg",
			filename:        "Tutankham (Bootleg).mra",
			wantContains:    []string{"unlicensed:bootleg"},
			wantNotContains: []string{"credit:bootleg"},
		},
		{
			name:            "Enhanced is edition:remaster",
			filename:        "King's Quest I (Enhanced) (OCS).adf",
			wantContains:    []string{"edition:remaster"},
			wantNotContains: []string{"credit:enhanced"},
		},
		{
			name:            "Engineering Sample is unfinished:proto",
			filename:        "Jump Shot (Engineering Sample).mra",
			wantContains:    []string{"unfinished:proto"},
			wantNotContains: []string{"credit:engineering-sample"},
		},
		{
			name:            "Location Test is unfinished:demo",
			filename:        "Battle Garegga (Location Test).mra",
			wantContains:    []string{"unfinished:demo"},
			wantNotContains: []string{"credit:location-test"},
		},
		{
			name:            "Reprint is release:reissue",
			filename:        "Armored Core (USA) (Reprint).chd",
			wantContains:    []string{"release:reissue"},
			wantNotContains: []string{"credit:reprint"},
		},
		{
			name:            "Nintendo Switch is distribution:switch-online",
			filename:        "Steins;Gate (USA) (Nintendo Switch).nes",
			wantContains:    []string{"distribution:switch-online"},
			wantNotContains: []string{"credit:nintendo-switch"},
		},
		{
			name:            "Named compilation collection is distribution:compilation",
			filename:        "Mega Man (World) (Mega Man Legacy Collection).nes",
			wantContains:    []string{"distribution:compilation"},
			wantNotContains: []string{"credit:mega-man-legacy-collection"},
		},
		{
			name:            "The Disney Afternoon Collection strips leading article for lookup",
			filename:        "Chip 'n Dale - Rescue Rangers (World) (The Disney Afternoon Collection).nes",
			wantContains:    []string{"distribution:compilation"},
			wantNotContains: []string{"credit:disney-afternoon-collection"},
		},
		{
			name:            "Castlevania Anniversary Collection is distribution:compilation",
			filename:        "Castlevania - The Adventure (USA) (Castlevania Anniversary Collection).gb",
			wantContains:    []string{"distribution:compilation"},
			wantNotContains: []string{"credit:castlevania-anniversary-collection"},
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
			for _, unexpected := range tt.wantNotContains {
				assert.NotContains(t, tagStrings, unexpected, "Unexpected tag %s found in %v", unexpected, tagStrings)
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
			name:     "Game with year in parentheses strips the year",
			input:    "Elden Ring (2022).exe",
			expected: "Elden Ring",
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

func TestParseCommaSeparatedTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tag      string
		wantTags []CanonicalTag
		wantNil  bool
	}{
		{
			name: "mixed region and revision (JP, Rev B)",
			tag:  "jp,-rev-b",
			wantTags: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionJP, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangJA, Source: TagSourceBracketed},
				{Type: TagTypeRev, Value: TagRevB, Source: TagSourceBracketed},
			},
		},
		{
			name: "multi-region USA and Europe",
			tag:  "usa,-europe",
			wantTags: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionUS, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeRegion, Value: TagRegionEU, Source: TagSourceBracketed},
			},
		},
		{
			name: "region and language (Europe, En)",
			tag:  "europe,-en",
			wantTags: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionEU, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
			},
		},
		{
			name: "dash-separated regions (EU-US)",
			tag:  "eu-us",
			wantTags: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionEU, Source: TagSourceBracketed},
				{Type: TagTypeRegion, Value: TagRegionUS, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
			},
		},
		{
			name: "revision with number (USA, Rev 1)",
			tag:  "usa,-rev-1",
			wantTags: []CanonicalTag{
				{Type: TagTypeRegion, Value: TagRegionUS, Source: TagSourceBracketed},
				{Type: TagTypeLang, Value: TagLangEN, Source: TagSourceBracketed},
				{Type: TagTypeRev, Value: TagRev1, Source: TagSourceBracketed},
			},
		},
		{
			name:    "single value returns nil",
			tag:     "usa",
			wantNil: true,
		},
		{
			name:    "empty tag returns nil",
			tag:     "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseCommaSeparatedTags(tt.tag)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tt.wantTags, got)
			}
		})
	}
}

func TestParseFilenameToCanonicalTags_MixedTagTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filename     string
		wantContains []string
	}{
		{
			name:         "1943 Midway Kaisen with JP and Rev B",
			filename:     "1943- Midway Kaisen (JP, Rev B).mra",
			wantContains: []string{"region:jp", "rev:b"},
		},
		{
			name:         "Game with USA and Rev 1",
			filename:     "Game (USA, Rev 1).rom",
			wantContains: []string{"region:us", "rev:1"},
		},
		{
			name:         "Game with Europe and English",
			filename:     "Game (Europe, En).rom",
			wantContains: []string{"region:eu", "lang:en"},
		},
		{
			name:         "Multi-region still works",
			filename:     "Game (USA, Europe).rom",
			wantContains: []string{"region:us", "region:eu"},
		},
		{
			name:         "Dash-separated regions still work",
			filename:     "Game (EU-US).rom",
			wantContains: []string{"region:eu", "region:us"},
		},
		{
			name:         "Japan with revision letter",
			filename:     "Sonic (Japan, Rev A).md",
			wantContains: []string{"region:jp", "rev:a"},
		},
		{
			name:         "Single region still works",
			filename:     "Mario (USA).nes",
			wantContains: []string{"region:us"},
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

			for _, want := range tt.wantContains {
				assert.Contains(t, gotStrings, want, "Expected tag %s not found in %v", want, gotStrings)
			}
		})
	}
}

func BenchmarkParseFilenameToCanonicalTags(b *testing.B) {
	b.Run("Simple", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ParseFilenameToCanonicalTags("Game (USA) [!].zip")
		}
	})

	b.Run("Complex", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ParseFilenameToCanonicalTags("Game Title (USA, Europe) (En,Fr,De) (Rev A) (v1.2) [!] [h1].zip")
		}
	})

	b.Run("Scene_release", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ParseFilenameToCanonicalTags("The.Dark.Knight.2008.1080p.BluRay.x264-GROUP.mkv")
		}
	})
}

func benchGenerateNumberedFilenames(n int) []string {
	filenames := make([]string, n)
	for i := range n {
		num := i + 1
		if num < 10 {
			filenames[i] = "0" + strconv.Itoa(num) + " - Game.zip"
		} else {
			filenames[i] = strconv.Itoa(num) + " - Game.zip"
		}
	}
	return filenames
}

func BenchmarkDetectNumberingPattern(b *testing.B) {
	for _, scale := range []int{100, 1000, 10000} {
		filenames := benchGenerateNumberedFilenames(scale)
		b.Run(strconv.Itoa(scale), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, fn := range filenames {
					ParseFilenameToCanonicalTags(fn)
				}
			}
		})
	}
}

func BenchmarkFilenameParser_ExtractSpecialPatterns(b *testing.B) {
	filename := "Game Title (Disc 1 of 3) (Rev A) (v1.2) (1998) S02E05.zip"
	b.ReportAllocs()
	for b.Loop() {
		extractSpecialPatterns(filename)
	}
}
