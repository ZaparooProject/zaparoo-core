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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// record is one dataset row kept because a library title or set can use it.
type record struct {
	// id identifies the game the row is a release of. Rows upstream gave no
	// usable ID keep the cell as written, so two of them are still told apart.
	id      string
	date    string
	regions []tags.TagValue
	// keys are the distinct slugs a library title can match the row by; see
	// matchKeys.
	keys   []string
	row    gdb.Row
	parsed rowTags
	// order is the row's position in its bundle, the final tie-break.
	order   int
	variant bool
}

// slugger turns dataset titles into the slugs the indexer gives filenames, so
// the two can be joined. Rows repeat a title once per regional release, so
// results are kept for the life of one system's match.
type slugger struct {
	cache     map[string]string
	mediaType slugs.MediaType
}

func newSlugger(mediaType slugs.MediaType) *slugger {
	return &slugger{mediaType: mediaType, cache: make(map[string]string)}
}

func (s *slugger) slug(title string) string {
	if title == "" {
		return ""
	}
	if cached, ok := s.cache[title]; ok {
		return cached
	}
	slug := slugs.Slugify(s.mediaType, tags.ParseTitleFromFilenameForMedia(title, false, s.mediaType))
	s.cache[title] = slug
	return slug
}

func newRecord(row *gdb.Row, order int, slugger *slugger) *record {
	rec := &record{row: *row, order: order, parsed: readTags(row.Tags), date: sortDate(row.Date)}
	rec.variant = rec.parsed.variant
	rec.regions, _ = regionValues(row.Region)
	if id, ok := datasetID(row.ID); ok {
		rec.id = id
	} else {
		rec.id = row.ID
	}
	rec.keys = matchKeys(row, rec.id, slugger)
	return rec
}

// macrons rewrites the long vowels of Hepburn romanisation the way dump
// names spell them. Upstream titles a game "Bishōjo Senshi Sailor Moon"; its
// file is "Bishoujo Senshi Sailor Moon".
var macrons = strings.NewReplacer(
	"ā", "aa", "ī", "ii", "ū", "uu", "ē", "ee", "ō", "ou",
	"Ā", "Aa", "Ī", "Ii", "Ū", "Uu", "Ē", "Ee", "Ō", "Ou",
)

// matchKeys lists the slugs a library title can match a row by: its title and
// its title-screen name, each also with long vowels spelled out, and its ID.
// The ID is upstream's own spelled-out, punctuation-free form of the title,
// which is what a file's slug comes to when nothing in the name is rewritten;
// it catches the long vowels dump names write as "oo" where the usual spelling
// is "ou".
func matchKeys(row *gdb.Row, id string, slugger *slugger) []string {
	keys := make([]string, 0, 3)
	add := func(key string) {
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, title := range [...]string{row.Title, row.ScreenTitle()} {
		add(slugger.slug(title))
		if spelled := macrons.Replace(title); spelled != title {
			add(slugger.slug(spelled))
		}
	}
	if validDatasetID(id) {
		add(id)
	}
	return keys
}

// catalog is the part of the dataset one system's library can use.
type catalog struct {
	// byTitle lists, for a library title, the rows whose title slugs to it.
	byTitle map[int64][]*record
	// bySet finds an arcade row by the MAME set it describes.
	bySet map[string]*record
}

// addSet files an arcade row under its set name. A few sets appear on more
// than one row; a retail row wins over a variant, then the earlier row.
func (c *catalog) addSet(rec *record) {
	set := rec.parsed.setName
	if set == "" {
		return
	}
	if existing, ok := c.bySet[set]; ok && (!existing.variant || rec.variant) {
		return
	}
	c.bySet[set] = rec
}

// addTitles files a row under every library title its keys reach. target
// receives the row; skip names titles that already have rows from a bundle
// that takes precedence.
func addTitles(rec *record, bySlug map[string][]int64, target, skip map[int64][]*record) bool {
	added := false
	for _, slug := range rec.keys {
		for _, titleID := range bySlug[slug] {
			if _, taken := skip[titleID]; taken {
				continue
			}
			if rows := target[titleID]; len(rows) > 0 && rows[len(rows)-1] == rec {
				continue
			}
			target[titleID] = append(target[titleID], rec)
			added = true
		}
	}
	return added
}

// bundleOrderStride separates the row order of successive bundles, so rows
// keep the order of the bundle list first and their own bundle second.
const bundleOrderStride = 1 << 20

// loadBundle reads one bundle's rows, keeping those keep accepts. order is the
// position given to the bundle's first row.
func loadBundle(
	ctx context.Context, bundle string, order int, slugger *slugger, keep func(*record) bool,
) ([]*record, error) {
	var kept []*record
	err := gdb.ForEach(bundle, func(row *gdb.Row) error {
		if order%256 == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr //nolint:wrapcheck // callers match context errors directly
			}
		}
		rec := newRecord(row, order, slugger)
		order++
		if keep(rec) {
			kept = append(kept, rec)
		}
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr //nolint:wrapcheck // callers match context errors directly
		}
		return nil, fmt.Errorf("%s: %w", scraperID, err)
	}
	return kept, nil
}

// choose picks the row that describes a title out of the rows whose title
// matched it. mediaRegions are the regions of the title's own files and
// preferred the configured regions, most wanted first. ok is false when the
// rows left are releases of more than one game: a slug two games share does
// not say which one the library holds.
func choose(candidates []*record, mediaRegions map[tags.TagValue]struct{}, preferred []string) (*record, bool) {
	rows := candidates
	if retail := filter(rows, func(r *record) bool { return !r.variant }); len(retail) > 0 {
		rows = retail
	}

	inFiles := filter(rows, func(r *record) bool {
		return slices.ContainsFunc(r.regions, func(region tags.TagValue) bool {
			_, ok := mediaRegions[region]
			return ok
		})
	})
	if len(inFiles) > 0 {
		rows = inFiles
	}
	// One region across the title's files settles it. With none, or several,
	// the configured preference picks among what is left.
	if len(mediaRegions) != 1 || len(inFiles) == 0 {
		for _, want := range preferred {
			wanted := filter(rows, func(r *record) bool { return slices.Contains(r.regions, tags.TagValue(want)) })
			if len(wanted) > 0 {
				rows = wanted
				break
			}
		}
	}
	if len(rows) == 0 {
		return nil, false
	}
	for _, row := range rows[1:] {
		if row.id != rows[0].id {
			return nil, false
		}
	}
	return slices.MinFunc(rows, func(a, b *record) int {
		return cmp.Or(cmp.Compare(a.date, b.date), cmp.Compare(a.order, b.order))
	}), true
}

func filter(rows []*record, keep func(*record) bool) []*record {
	var out []*record
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
		}
	}
	return out
}
