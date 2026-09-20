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

// Package misterarcade imports the MiSTer arcade catalog's metadata onto
// indexed .mra rows.
//
// MiSTer platforms already download, cache and classify that catalog; this
// scraper performs the join they stop short of, keying each indexed descriptor
// to its catalog row by the MAME set name declared inside it.
package misterarcade

import (
	"html"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// Entry is one catalog row. Field names follow the catalog's own columns rather
// than Core's tag vocabulary, so a mapping change never looks like a source
// change. The catalog's display name and layout spacer columns are not read:
// arcade titles come from the descriptor filename, which the indexer owns.
type Entry struct {
	SetName         string
	Region          string
	Version         string
	Alternative     string
	ParentTitle     string
	Platform        string
	Series          string
	Homebrew        string
	Bootleg         string
	Year            string
	Manufacturer    string
	Category        string
	Resolution      string
	Rotation        string
	Players         string
	MoveInputs      string
	SpecialControls string
	NumButtons      string
	Flip            string
}

// Catalog supplies the arcade metadata rows for one run. The catalog itself is
// platform-owned — MiSTer and MiSTeX cache and embed it — so the platform
// injects its reader rather than this package reaching into their build.
type Catalog func(platforms.Platform) ([]Entry, error)

// SetNameCache is an optional fast path for resolving an indexed descriptor to
// its set name. MiSTer already reads every set name while classifying granular
// arcade systems and keeps the result in a size/mtime-validated cache; reusing
// it means a scrape costs a map lookup per row instead of thousands of reads
// from SD storage. ok=false falls back to reading the descriptor. A nil cache
// is valid and means every row is read.
type SetNameCache func(path string) (setName string, ok bool)

// index maps a lower-cased set name to its catalog row. Later duplicates lose:
// the catalog is a single upstream file, so a repeated set name is a defect in
// it rather than an ambiguity Core can resolve.
func index(entries []Entry) map[string]*Entry {
	byName := make(map[string]*Entry, len(entries))
	for i := range entries {
		key := strings.ToLower(strings.TrimSpace(entries[i].SetName))
		if key == "" {
			continue
		}
		if _, exists := byName[key]; exists {
			continue
		}
		byName[key] = &entries[i]
	}
	return byName
}

// sentinels are catalog values that stand for "not applicable" rather than data.
// The catalog writes them in several cases and spells the empty case both ways.
var sentinels = map[string]struct{}{ //nolint:gochecknoglobals // Static lookup set.
	"":     {},
	"n-a":  {},
	"n/a":  {},
	"na":   {},
	"none": {},
}

// field cleans one catalog value for reading: catalog text is HTML-escaped in
// places (`Ball &amp; Paddle`) and carries stray whitespace. An empty result
// means the column said nothing.
func field(value string) string {
	value = html.UnescapeString(value)
	value = strings.Join(strings.Fields(value), " ")
	if _, sentinel := sentinels[strings.ToLower(value)]; sentinel {
		return ""
	}
	return value
}

// isYes reads the catalog's boolean columns. Upstream has at least one typo
// ("ys"), so a prefix match on the affirmative is used rather than equality;
// every negative spelling in the file starts with "n".
func isYes(value string) bool {
	value = strings.ToLower(field(value))
	return value == "yes" || value == "ys" || value == "y" || value == "true"
}
