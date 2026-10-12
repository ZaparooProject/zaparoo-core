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

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// GameDataBase's tags are the model for Core's own vocabulary, so most values
// carry over unchanged once "#type:sub>child" is rewritten as "sub:child".
// The tables here hold the exceptions: values Core names differently, mistakes
// in the upstream data that have one obvious reading, and mistakes that do
// not. The coverage test reads every embedded row against them, so a refresh
// of the data that brings a new value or a new mistake fails until it is
// listed, and an entry the data no longer needs fails too.

// typedValue is one Core tag.
type typedValue struct {
	Type  tags.TagType
	Value tags.TagValue
}

// Upstream tag types that are not a Core tag type of the same name and
// meaning.
const (
	typeCompilation = "compilation"
	typeMameROM     = "mamerom"
	typeMameParent  = "mameparent"
	typeDisc        = "disc"
)

// titleTypes are the upstream tag types whose values describe the game and
// are written to the title. Every other type describes one release or dump
// (its region, language, revision, disc, prototype or bootleg status, or the
// re-release it came from) and is left to the filename parser, which knows
// which file it is looking at.
var titleTypes = map[tags.TagType]struct{}{
	tags.TagTypeGenre:         {},
	tags.TagTypePlayers:       {},
	tags.TagTypeInput:         {},
	tags.TagTypeAddon:         {},
	tags.TagTypeEmbedded:      {},
	tags.TagTypeSave:          {},
	tags.TagTypeCompatibility: {},
	tags.TagTypeBased:         {},
	tags.TagTypeSearch:        {},
	tags.TagTypePort:          {},
	tags.TagTypeArcadeBoard:   {},
	tags.TagTypeMultigame:     {},
	tags.TagTypeCompilation:   {},
}

// variantTypes are the upstream tags that mark a row as something other than
// a game's original retail release: a prototype, an unlicensed copy, an
// alternate dump, a budget re-boxing or a later re-release. Such a row shares
// its title with the original, so it is passed over when an original row is
// there to describe the title.
var variantTypes = map[string]struct{}{
	"unfinished": {},
	"unlicensed": {},
	"alt":        {},
	"reboxed":    {},
	"rerelease":  {},
}

// tokenRecoveries rewrites the tags upstream wrote wrongly, keyed by the tag
// exactly as written. An empty replacement drops the tag: it is a stray
// fragment that says nothing.
var tokenRecoveries = map[string][]string{
	// The "#" was left off.
	"lang:en":                {"#lang:en"},
	"lang:es":                {"#lang:es"},
	"lang:en:fr:de:es:it:sv": {"#lang:en:fr:de:es:it:sv"},
	"port:arcade":            {"#port:arcade"},
	// The "#input:" prefix was lost.
	"joystick>8:buttons>2": {"#input:joystick>8:buttons>2"},
	// A stray character in an otherwise plain tag.
	"#players:2;coop": {"#players:2:coop"},
	"#genre:action>platformer:adventure:shooting>>rail": {"#genre:action>platformer:adventure:shooting>rail"},
	"@reboxed:banprestobest":                            {"#reboxed:banprestobest"},

	// A repeat of part of the genre tag beside it.
	"action:fighting>melee": {},
	// Notes that are not tags.
	"[b]":          {},
	"qsound":       {},
	"1lghost.zip":  {},
	"(1993-03-17)": {},
	"(1994-07-21)": {},
	"(1994-08-09)": {},
	"(1994-08-15)": {},
	"(1994-08-17)": {},
	"(1994-08-25)": {},
	"(1994-08-26)": {},
}

// valueRenames maps an upstream value to the Core value for the same thing.
var valueRenames = map[typedValue]typedValue{
	{tags.TagTypeAddon, "reader:barcodeboy"}:    {tags.TagTypeAddon, tags.TagAddonBarcodeBarcodeboy},
	{tags.TagTypeAddon, "reader:barcodereader"}: {tags.TagTypeAddon, tags.TagAddonBarcodeBarcodereader},
	{tags.TagTypeGenre, "sim:driving"}:          {tags.TagTypeGenre, tags.TagGameGenreRacingDriving},
	{tags.TagTypeEmbedded, "motion:accelerometer"}: {
		tags.TagTypeEmbedded, tags.TagEmbeddedAccelerometer,
	},
	{tags.TagTypeArcadeBoard, "nintendo:vssystem"}: {tags.TagTypeArcadeBoard, tags.TagArcadeBoardNintendoVS},
	{tags.TagTypeArcadeBoard, "nintendo:pc10"}: {
		tags.TagTypeArcadeBoard, tags.TagArcadeBoardNintendoPlaychoice10,
	},
}

// valueRecoveries repairs a value upstream wrote wrongly. One value can
// stand for several.
var valueRecoveries = map[typedValue][]typedValue{
	// "#players:2:coop>vs": two modes written as a mode and its child.
	{tags.TagTypePlayers, "coop:vs"}: {
		{tags.TagTypePlayers, tags.TagPlayersCoop}, {tags.TagTypePlayers, tags.TagPlayersVS},
	},
	// "#save:backp".
	{tags.TagTypeSave, "backp"}: {{tags.TagTypeSave, tags.TagSaveBackup}},
	// "#save:keyword>official": a search keyword filed under save.
	{tags.TagTypeSave, "keyword:official"}: {{tags.TagTypeSearch, tags.TagSearchKeywordOfficial}},
}

// bareValues are the Core values for an upstream tag written with no subtag.
var bareValues = map[string]typedValue{
	"based":         {tags.TagTypeBased, tags.TagBasedBased},
	"unlicensed":    {tags.TagTypeUnlicensed, tags.TagUnlicensed},
	"alt":           {tags.TagTypeAlt, tags.TagAlt},
	typeCompilation: {tags.TagTypeMultigame, tags.TagMultigameCompilation},
}

// bareSkips are upstream tags that say nothing without a subtag.
var bareSkips = map[string]struct{}{
	"lang":      {},
	"port":      {},
	"reboxed":   {},
	"rerelease": {},
}

// idRecoveries repairs the IDs upstream wrote with stray capitals, spaces or
// punctuation, keyed by the ID exactly as written.
var idRecoveries = map[string]string{
	"gigawing (Phoenix Edition)":                "gigawing",
	"greatmahoudaisakusen (Phoenix Edition)":    "greatmahoudaisakusen",
	"space beam":                                "spacebeam",
	"swatspecialweaponsandtactics.":             "swatspecialweaponsandtactics",
	"tatakaeramenmanSakuretsuchoujinhyakunigei": "tatakaeramenmansakuretsuchoujinhyakunigei",
	"Porter":                "porter",
	"yomihon yumegoyomitk2": "yomihonyumegoyomitk2",
	"crayonshinchandensetsuwoyobuomakenomiyakoshockgahn!": "crayonshinchandensetsuwoyobuomakenomiyakoshockgahn",
	"Thunderbirdsgbc":                          "thunderbirdsgbc",
	"Majuuou":                                  "majuuou",
	"Sonicslastman2sfc":                        "sonicslastman2sfc",
	"takedanobuhironosuperlLeaguesoccer":       "takedanobuhironosuperlleaguesoccer",
	"macsBasicriflemarksmanship":               "macsbasicriflemarksmanship",
	"turfwind 96takeyutakakyousoubaikuseigame": "turfwind96takeyutakakyousoubaikuseigame",
}

// idSkips are IDs with no single obvious repair: an empty or placeholder
// cell, a romanisation with a macron where the rest of the data spells the
// long vowel out, and two IDs in one cell.
var idSkips = map[string]struct{}{
	"":                 {},
	"=":                {},
	"taisentsumeshōgi": {},
	"tanimurahitoshiryūuupachinkokouryakudaisakusendonquijotegaiku": {},
	"honoonotoukyūjidodgedanpeisfc":                                 {},
	"shichūsuimeigakunyuumonshintougenkyou":                         {},
	"kingoffightersr1/melonchannoseichounikki":                      {},
}

// dateSkips are release-date cells that do not begin with a readable year.
var dateSkips = map[string]struct{}{
	"19890-06":     {},
	"20003-12-12":  {},
	"20000":        {},
	"20001":        {},
	"19934":        {},
	"201-10-04":    {},
	"995-06-01":    {},
	"NAMCO":        {},
	"NA.P.S. Team": {},
	"()":           {},
}

// regionWords are upstream region names Core's shared region table does not
// spell the same way.
var regionWords = map[string]tags.TagValue{
	"South Korea": tags.TagRegionKR,
	"Ireland":     tags.TagRegionIE,
	"Belgium":     tags.TagRegionBE,
}

// regionSkips are upstream region names that are not one Core region.
var regionSkips = map[string]struct{}{
	"":         {},
	"Unknown":  {},
	"Hispanic": {},
	"Oceania":  {},
	"Benelux":  {},
}

// regionValues reads a region cell, which lists regions separated by "/".
// unknown lists the words that are neither mapped nor deliberately skipped.
func regionValues(cell string) (values []tags.TagValue, unknown []string) {
	for word := range strings.SplitSeq(cell, "/") {
		word = strings.TrimSpace(word)
		if _, skip := regionSkips[word]; skip {
			continue
		}
		value, ok := regionWords[word]
		if !ok {
			value, ok = tags.LookupRegionWord(word)
		}
		if !ok {
			unknown = append(unknown, word)
			continue
		}
		values = append(values, value)
	}
	return values, unknown
}

// datasetID returns a row's game ID in the form Core stores, repairing the
// IDs listed in idRecoveries. ok is false for a row with no usable ID.
func datasetID(raw string) (id string, ok bool) {
	if fixed, listed := idRecoveries[raw]; listed {
		raw = fixed
	}
	if !tags.IsValidTagValue(tags.TagTypeGameDatabase, raw) {
		return "", false
	}
	return raw, true
}

// releaseYear reads the year a release date opens with. The date may be a
// bare year or carry a month and day; what follows the year is not checked,
// since upstream writes that part loosely ("1989-1-31", "1990-06-").
func releaseYear(date string) (year string, ok bool) {
	date = strings.TrimSpace(date)
	if len(date) < 4 || (len(date) > 4 && date[4] != '-') {
		return "", false
	}
	year = date[:4]
	if !tags.IsValidTagValue(tags.TagTypeYear, year) {
		return "", false
	}
	return year, true
}

// sortDate is the form release dates are compared in to find a title's
// earliest release. Within a year, a date written in full comes before one
// that gives only the month, and that before one that gives only the year: the
// row upstream dated exactly is the release it knows best, where a bare year
// is often a later variant filed without a date. A date with no readable year
// sorts last.
func sortDate(date string) string {
	year, ok := releaseYear(date)
	if !ok {
		return "9999-99-99"
	}
	date = strings.TrimSpace(date)
	if !isFullDatePart(date, 4) {
		return year + "-99-99"
	}
	if !isFullDatePart(date, 7) {
		return date[:7] + "-99"
	}
	return date[:10]
}

// isFullDatePart reports whether date has "-NN" at offset at.
func isFullDatePart(date string, at int) bool {
	if len(date) < at+3 || date[at] != '-' {
		return false
	}
	return date[at+1] >= '0' && date[at+1] <= '9' && date[at+2] >= '0' && date[at+2] <= '9' &&
		(len(date) == at+3 || date[at+3] == '-')
}
