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

package misterarcade

import (
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// writeBuilder accumulates one media row's write, keeping each tag type's
// values distinct and preserving the order they were derived in. A value the
// vocabulary would refuse is dropped and reported rather than written.
type writeBuilder struct {
	write    *database.ScrapeWrite
	seen     map[string]struct{}
	unmapped *scraper.UnmappedValues
}

func newWriteBuilder(runID string, unmapped *scraper.UnmappedValues) *writeBuilder {
	b := &writeBuilder{
		write:    &database.ScrapeWrite{Sentinel: scraper.SentinelTagInfo(scraperID)},
		seen:     make(map[string]struct{}),
		unmapped: unmapped,
	}
	if runID != "" {
		b.write.MediaTags = append(b.write.MediaTags, scraper.RunTagInfo(scraperID, runID))
	}
	return b
}

// note reports a catalog value that has no tag mapping.
func (b *writeBuilder) note(tagType tags.TagType, raw string) {
	if b.unmapped != nil {
		b.unmapped.Note(scraperID, tagType, raw)
	}
}

func (b *writeBuilder) add(target *[]database.TagInfo, tagType tags.TagType, value tags.TagValue, label string) {
	if value == "" {
		return
	}
	if !tags.IsValidTagValue(tagType, string(value)) {
		b.note(tagType, string(value))
		return
	}
	key := string(tagType) + ":" + string(value)
	if _, dup := b.seen[key]; dup {
		return
	}
	b.seen[key] = struct{}{}
	*target = append(*target, database.TagInfo{Type: string(tagType), Tag: string(value), Label: label})
}

// title and media write a closed or format tag, which never carries a label.
func (b *writeBuilder) title(tagType tags.TagType, value tags.TagValue) {
	b.add(&b.write.TitleTags, tagType, value, "")
}

func (b *writeBuilder) media(tagType tags.TagType, value tags.TagValue) {
	b.add(&b.write.MediaTags, tagType, value, "")
}

// yearPattern accepts only a plain four-digit year.
var yearPattern = regexp.MustCompile(`^\d{4}$`) //nolint:gochecknoglobals // Compiled once.

// buildWrite maps one catalog entry onto the write that enriches a media row.
// Catalog values with no tag mapping are dropped and noted in unmapped, which
// may be nil.
//
// Shared facts about the game — when it came out, who made it, what it plays
// like, what it runs on — go to the title, so every regional variant of one
// game carries them. Facts about the individual romset go to the media row.
func buildWrite(entry *Entry, runID string, unmapped *scraper.UnmappedValues) *database.ScrapeWrite {
	b := newWriteBuilder(runID, unmapped)
	titleTags(b, entry)
	mediaTags(b, entry)
	if setName := field(entry.SetName); setName != "" {
		b.write.MediaProps = append(b.write.MediaProps, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyMAMESetName),
			Text:    strings.ToLower(setName),
		})
	}
	return b.write
}

func titleTags(b *writeBuilder, entry *Entry) {
	// Only a plain four-digit year is a release year: the catalog writes an
	// unknown one as "19xx", which is not a claim about the decade.
	if year := field(entry.Year); yearPattern.MatchString(year) {
		b.title(tags.TagTypeYear, tags.TagValue(year))
	} else if year != "" {
		b.note(tags.TagTypeYear, year)
	}
	if maker := field(entry.Manufacturer); maker != "" {
		// Company names are the one free-text type, so they keep the
		// catalog's spelling as a label.
		b.add(&b.write.TitleTags, tags.TagTypeDeveloper, tags.NormalizeCompanyName(maker), maker)
	}
	genres, known := genreTags(entry.Category)
	if !known {
		b.note(tags.TagTypeGenre, field(entry.Category))
	}
	for _, value := range genres {
		b.title(tags.TagTypeGenre, value)
	}
	franchiseTags(b, entry)
	if board := field(entry.Platform); board != "" {
		if value, ok := tags.LookupArcadeBoard(board); ok {
			b.title(tags.TagTypeArcadeBoard, value)
		} else if _, skip := boardSkips[skipKey(board)]; !skip {
			b.note(tags.TagTypeArcadeBoard, board)
		}
	}
	players, playersOK := playerTags(entry.Players)
	if !playersOK {
		b.note(tags.TagTypePlayers, field(entry.Players))
	}
	for _, value := range players {
		b.title(tags.TagTypePlayers, value)
	}
	controls, unknown := controlTags(entry.MoveInputs, entry.SpecialControls)
	for _, value := range controls {
		b.title(tags.TagTypeInput, value)
	}
	for _, phrase := range unknown {
		b.note(tags.TagTypeInput, phrase)
	}
	if value, ok := buttonTag(entry.NumButtons); ok {
		b.title(tags.TagTypeInput, value)
	} else if raw := field(entry.NumButtons); raw != "" && raw != "0" {
		b.note(tags.TagTypeInput, "buttons:"+raw)
	}
	if rate := field(entry.Resolution); rate != "" {
		if value := scanRateValue(rate); value != "" {
			b.title(tags.TagTypeVideo, value)
		} else {
			b.note(tags.TagTypeVideo, rate)
		}
	}
	b.title(tags.TagTypeSearch, tateValue(entry.Rotation))
	if isYes(entry.Flip) {
		b.title(tags.TagTypeSearch, tags.TagSearchKeywordFlip)
	}
	if isYes(entry.Homebrew) {
		b.title(tags.TagTypeRelease, tags.TagReleaseHomebrew)
	}
}

// franchiseTags writes the game's series. The catalog's curated series column
// is the source; a series it files that is not a franchise is dropped
// deliberately, any other unknown one is reported. The parent title is not a
// series, but where the catalog names no series and the parent title is
// itself a listed franchise ("Galaga"), that is used.
func franchiseTags(b *writeBuilder, entry *Entry) {
	if series := field(entry.Series); series != "" {
		if value, ok := tags.LookupFranchise(series); ok {
			b.title(tags.TagTypeSearch, value)
			return
		}
		if _, skip := notFranchises[skipKey(series)]; !skip {
			b.note(tags.TagTypeSearch, series)
		}
		return
	}
	if value, ok := tags.LookupFranchise(field(entry.ParentTitle)); ok {
		b.title(tags.TagTypeSearch, value)
	}
}

func mediaTags(b *writeBuilder, entry *Entry) {
	// The catalog files a handful of unlicensed sets under the region column
	// instead of the bootleg one. They state a provenance, not a territory.
	for _, word := range strings.Split(field(entry.Region), " - ") {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if strings.EqualFold(word, "bootleg") {
			b.media(tags.TagTypeUnlicensed, tags.TagUnlicensedBootleg)
			continue
		}
		if value, ok := tags.LookupRegionWord(word); ok {
			b.media(tags.TagTypeRegion, value)
		} else {
			b.note(tags.TagTypeRegion, word)
		}
	}
	if tagType, value, ok := versionTag(entry.Version); ok {
		b.media(tagType, value)
	}
	if isYes(entry.Alternative) {
		b.media(tags.TagTypeAlt, tags.TagAlt)
	}
	if isYes(entry.Bootleg) {
		b.media(tags.TagTypeUnlicensed, tags.TagUnlicensedBootleg)
	}
}

// scanRateValue reads the monitor scan rate. The catalog writes one of two
// values, with a stray space in one row.
func scanRateValue(resolution string) tags.TagValue {
	switch strings.ToLower(strings.ReplaceAll(field(resolution), " ", "")) {
	case "15khz":
		return tags.TagVideo15KHz
	case "31khz":
		return tags.TagVideo31KHz
	default:
		return ""
	}
}

// tateValue reads the cabinet monitor rotation. A horizontal monitor is the
// default and writes nothing, so a tate tag always means a rotated screen.
func tateValue(rotation string) tags.TagValue {
	switch strings.ToLower(field(rotation)) {
	case "vertical (cw)":
		return tags.TagSearchTateCW
	case "vertical (ccw)":
		return tags.TagSearchTateCCW
	default:
		return ""
	}
}

// versionPrefixes routes a version phrase that names a numbered variant.
var versionPrefixes = []struct { //nolint:gochecknoglobals // Static routing table.
	prefix  string
	tagType tags.TagType
}{
	{prefix: "rev", tagType: tags.TagTypeRev},
	{prefix: "set", tagType: tags.TagTypeSet},
}

// versionWords routes a version phrase that names a status outright.
var versionWords = map[string]struct { //nolint:gochecknoglobals // Static routing table.
	tagType tags.TagType
	value   tags.TagValue
}{
	"prototype":     {tagType: tags.TagTypeUnfinished, value: tags.TagUnfinishedProto},
	"proto":         {tagType: tags.TagTypeUnfinished, value: tags.TagUnfinishedProto},
	"bootleg":       {tagType: tags.TagTypeUnlicensed, value: tags.TagUnlicensedBootleg},
	"hack":          {tagType: tags.TagTypeUnlicensed, value: tags.TagUnlicensedHack},
	"no protection": {tagType: tags.TagTypeProtection, value: tags.TagProtectionNone},
	"not protected": {tagType: tags.TagTypeProtection, value: tags.TagProtectionNone},
	"decrypted":     {tagType: tags.TagTypeProtection, value: tags.TagProtectionDecrypted},
	"encrypted":     {tagType: tags.TagTypeProtection, value: tags.TagProtectionEncrypted},
	"unprotected":   {tagType: tags.TagTypeProtection, value: tags.TagProtectionNone},
}

// protectionChips routes a version phrase that opens with a protection chip
// family, such as "FD1089B 317-xxxx".
var protectionChips = []struct { //nolint:gochecknoglobals // Static routing table.
	prefix string
	value  tags.TagValue
}{
	{prefix: "fd1094", value: tags.TagProtectionFD1094},
	{prefix: "fd1089", value: tags.TagProtectionFD1089},
	{prefix: "mc-8123", value: tags.TagProtectionMC8123},
	{prefix: "mc8123", value: tags.TagProtectionMC8123},
	{prefix: "8751", value: tags.TagProtection8751},
}

// versionTag routes the catalog's version column, which is one free-text cell
// carrying several unrelated kinds of fact. Only the forms that name something
// the tag vocabulary already models are routed; the rest of the column is prose
// about a particular dump and is left alone.
func versionTag(version string) (tagType tags.TagType, value tags.TagValue, ok bool) {
	raw := field(version)
	if raw == "" {
		return "", "", false
	}
	lower := strings.ToLower(raw)
	if date, dateOK := tags.ParseBuildDate(raw); dateOK && isAllDigits(raw) {
		return tags.TagTypeBuildDate, tags.TagValue(date), true
	}
	if word, found := versionWords[lower]; found {
		return word.tagType, word.value, true
	}
	for _, chip := range protectionChips {
		if strings.HasPrefix(lower, chip.prefix) {
			return tags.TagTypeProtection, chip.value, true
		}
	}
	for _, route := range versionPrefixes {
		suffix, matched := matchVersionPrefix(lower, route.prefix)
		if matched {
			return route.tagType, tags.TagValue(suffix), true
		}
	}
	return "", "", false
}

// matchVersionPrefix accepts "Rev 1", "Rev. A" and "Set 2", returning the
// single character that identifies the variant. Anything longer is a phrase
// about the dump rather than a variant marker.
func matchVersionPrefix(lower, prefix string) (suffix string, ok bool) {
	rest, found := strings.CutPrefix(lower, prefix)
	if !found {
		return "", false
	}
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "."))
	if len(rest) != 1 {
		return "", false
	}
	c := rest[0]
	if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') {
		return rest, true
	}
	return "", false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
