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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// rowTags is one row's tags cell read into Core's terms.
type rowTags struct {
	// setName and setParent are the MAME set an arcade row describes and the
	// parent set it is a clone of. setParent is empty for a parent set.
	setName   string
	setParent string
	// values are the row's tags as Core values, of every type, in the order
	// upstream wrote them.
	values []typedValue
	// contains are the IDs of the games a compilation row contains.
	contains []string
	// unknown are the tags and values with no mapping.
	unknown []typedValue
	// variant is set for a row that is not its game's original retail
	// release; see variantTypes.
	variant bool
}

// readTags converts a row's tags cell.
func readTags(cell string) rowTags {
	var out rowTags
	tokens, unreadable := splitTokens(cell)
	for _, raw := range unreadable {
		out.unknown = append(out.unknown, typedValue{Type: tags.TagTypeUnknown, Value: tags.TagValue(raw)})
	}
	for i := range tokens {
		out.readToken(&tokens[i])
	}
	return out
}

func (t *rowTags) readToken(tok *token) {
	if _, ok := variantTypes[tok.tagType]; ok {
		t.variant = true
	}
	switch tok.tagType {
	case typeMameROM:
		t.readSet(tok)
		return
	case typeMameParent:
		// States that the row's set is a parent, which its mamerom tag already
		// shows by naming no clone.
		return
	case typeCompilation:
		t.readCompilation(tok)
		return
	case typeDisc:
		t.readDisc(tok)
		return
	}
	if len(tok.subs) == 0 {
		if value, ok := bareValues[tok.tagType]; ok {
			t.values = append(t.values, value)
		} else if _, skip := bareSkips[tok.tagType]; !skip {
			t.unknown = append(t.unknown, typedValue{Type: tags.TagType(tok.tagType)})
		}
		return
	}
	for _, value := range rawValues(tok) {
		t.add(value)
	}
}

// rawValues lists an ordinary tag's values as upstream means them, before any
// renaming: "sub" for a subtag alone and "sub:child" for each child.
func rawValues(tok *token) []typedValue {
	tagType := tags.TagType(tok.tagType)
	values := make([]typedValue, 0, len(tok.subs))
	for i := range tok.subs {
		sub := &tok.subs[i]
		if len(sub.children) == 0 {
			values = append(values, typedValue{Type: tagType, Value: tags.TagValue(sub.name)})
			continue
		}
		// A genre is filed under its family as well as its own name, as every
		// other scraper writes it.
		if tagType == tags.TagTypeGenre {
			values = append(values, typedValue{Type: tagType, Value: tags.TagValue(sub.name)})
		}
		for _, child := range sub.children {
			values = append(values, typedValue{Type: tagType, Value: tags.TagValue(sub.name + ":" + child)})
		}
	}
	return values
}

// add maps one upstream value onto the vocabulary.
func (t *rowTags) add(value typedValue) {
	if renamed, ok := valueRenames[value]; ok {
		value = renamed
	}
	if fixed, ok := valueRecoveries[value]; ok {
		t.values = append(t.values, fixed...)
		return
	}
	if !tags.IsValidTagValue(value.Type, string(value.Value)) {
		t.unknown = append(t.unknown, value)
		return
	}
	t.values = append(t.values, value)
}

// readSet reads "#mamerom:parent>clone", or "#mamerom:set" for a set that is
// its own parent.
func (t *rowTags) readSet(tok *token) {
	if len(tok.subs) != 1 || len(tok.subs[0].children) > 1 {
		t.unknown = append(t.unknown, typedValue{Type: tags.TagTypeMameParent})
		return
	}
	sub := &tok.subs[0]
	set, parent := sub.name, ""
	if len(sub.children) == 1 {
		set, parent = sub.children[0], sub.name
	}
	if !tags.IsValidTagValue(tags.TagTypeMameParent, set) {
		t.unknown = append(t.unknown, typedValue{Type: tags.TagTypeMameParent, Value: tags.TagValue(set)})
		return
	}
	t.setName, t.setParent = set, parent
}

// readCompilation reads the IDs of the games a compilation contains. Upstream
// separates them with ":" and, in a few rows, with ">".
func (t *rowTags) readCompilation(tok *token) {
	t.values = append(t.values, bareValues[typeCompilation])
	note := func(id string) {
		if !tags.IsValidTagValue(tags.TagTypeCompilation, id) {
			t.unknown = append(t.unknown, typedValue{Type: tags.TagTypeCompilation, Value: tags.TagValue(id)})
			return
		}
		t.contains = append(t.contains, id)
	}
	for i := range tok.subs {
		note(tok.subs[i].name)
		for _, child := range tok.subs[i].children {
			note(child)
		}
	}
}

// readDisc reads "#disc:total>number". Neither value is written: a disc
// number belongs to one file, which the filename parser reads.
func (t *rowTags) readDisc(tok *token) {
	for i := range tok.subs {
		sub := &tok.subs[i]
		t.add(typedValue{Type: tags.TagTypeDiscTotal, Value: tags.TagValue(sub.name)})
		for _, child := range sub.children {
			t.add(typedValue{Type: tags.TagTypeDisc, Value: tags.TagValue(child)})
		}
	}
}

// writeBuilder accumulates one media row's write, keeping each tag distinct
// and in the order it was derived. A value the vocabulary would refuse is
// dropped and reported rather than written.
type writeBuilder struct {
	write    *database.ScrapeWrite
	seen     map[typedValue]struct{}
	unmapped *scraper.UnmappedValues
}

func newWriteBuilder(runID string, unmapped *scraper.UnmappedValues) *writeBuilder {
	b := &writeBuilder{
		write:    &database.ScrapeWrite{Sentinel: scraper.SentinelTagInfo(scraperID)},
		seen:     make(map[typedValue]struct{}),
		unmapped: unmapped,
	}
	if runID != "" {
		b.write.MediaTags = append(b.write.MediaTags, scraper.RunTagInfo(scraperID, runID))
	}
	return b
}

func (b *writeBuilder) note(tagType tags.TagType, raw string) {
	if b.unmapped != nil {
		b.unmapped.Note(scraperID, tagType, raw)
	}
}

func (b *writeBuilder) add(target *[]database.TagInfo, value typedValue, label string) {
	if value.Value == "" {
		return
	}
	if !tags.IsValidTagValue(value.Type, string(value.Value)) {
		b.note(value.Type, string(value.Value))
		return
	}
	if _, dup := b.seen[value]; dup {
		return
	}
	b.seen[value] = struct{}{}
	*target = append(*target, database.TagInfo{Type: string(value.Type), Tag: string(value.Value), Label: label})
}

func (b *writeBuilder) title(tagType tags.TagType, value tags.TagValue) {
	b.add(&b.write.TitleTags, typedValue{Type: tagType, Value: value}, "")
}

// addTitle writes what a row says about the game to the title.
func (b *writeBuilder) addTitle(row *gdb.Row, parsed *rowTags) {
	if id, ok := datasetID(row.ID); ok {
		b.title(tags.TagTypeGameDatabase, tags.TagValue(id))
	}
	if year, ok := releaseYear(row.Date); ok {
		b.title(tags.TagTypeYear, tags.TagValue(year))
	} else if _, skip := dateSkips[strings.TrimSpace(row.Date)]; !skip && strings.TrimSpace(row.Date) != "" {
		b.note(tags.TagTypeYear, row.Date)
	}
	b.addCompanies(tags.TagTypeDeveloper, row.Developer)
	b.addCompanies(tags.TagTypePublisher, row.Publisher)

	board := false
	for i := range parsed.values {
		value := parsed.values[i]
		if _, written := titleTypes[value.Type]; !written {
			continue
		}
		// A title holds one board. Upstream names the manufacturer and its
		// board in one tag, so the first value is the most specific it gives.
		if value.Type == tags.TagTypeArcadeBoard {
			if board {
				continue
			}
			board = true
		}
		b.title(value.Type, value.Value)
	}
	for _, id := range parsed.contains {
		b.title(tags.TagTypeCompilation, tags.TagValue(id))
	}
	for i := range parsed.unknown {
		b.note(parsed.unknown[i].Type, string(parsed.unknown[i].Value))
	}
}

// addCompanies writes a developer or publisher cell. Upstream joins several
// companies with "/"; a title holds one developer and one publisher, so the
// first named takes the role and the rest are kept as credits.
func (b *writeBuilder) addCompanies(role tags.TagType, cell string) {
	var holder tags.TagValue
	for name := range strings.SplitSeq(cell, "/") {
		name = strings.TrimSpace(name)
		value := tags.NormalizeCompanyName(name)
		if value == "" || value == holder {
			continue
		}
		tagType := tags.TagTypeCredit
		if holder == "" {
			tagType, holder = role, value
		}
		// Company names are the one free-text type, so they keep upstream's
		// spelling as a label.
		b.add(&b.write.TitleTags, typedValue{Type: tagType, Value: value}, name)
	}
}

// addSet writes an arcade row's set identity to the media row it matched.
func (b *writeBuilder) addSet(parsed *rowTags) {
	if parsed.setName == "" {
		return
	}
	b.write.MediaProps = append(b.write.MediaProps, database.MediaProperty{
		TypeTag: tags.PropertyTypeTag(tags.TagPropertyMAMESetName),
		Text:    parsed.setName,
	})
	if parsed.setParent != "" && parsed.setParent != parsed.setName {
		b.add(&b.write.MediaTags, typedValue{Type: tags.TagTypeMameParent, Value: tags.TagValue(parsed.setParent)}, "")
	}
}
