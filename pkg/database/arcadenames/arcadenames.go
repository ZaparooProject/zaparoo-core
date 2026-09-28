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

// Package arcadenames names arcade romsets. A set archive is named after its
// MAME set ("dkong.zip"), which is neither a readable title nor what
// libretro and ES-style scrapers key their artwork by. The embedded catalog
// maps set names to MAME's own description, year and manufacturer. It is
// generated from a MAME release's machine list by scripts/arcadenames; see
// LICENSE in this directory for its terms.
package arcadenames

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

//go:embed names.tsv.gz
var embedded []byte

// Entry is one set's catalog record.
type Entry struct {
	Title        string
	Year         string
	Manufacturer string
}

var (
	loadOnce sync.Once
	entries  map[string]Entry
	version  string
)

func load() {
	entries = make(map[string]Entry)
	zr, err := gzip.NewReader(bytes.NewReader(embedded))
	if err != nil {
		log.Error().Err(err).Msg("arcade names catalog unreadable")
		return
	}
	defer func() { _ = zr.Close() }()
	scanner := bufio.NewScanner(zr)
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, "# MAME "); ok {
			version = rest
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[0] == "" || fields[1] == "" {
			continue
		}
		entries[fields[0]] = Entry{Title: fields[1], Year: fields[2], Manufacturer: fields[3]}
	}
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("arcade names catalog truncated")
	}
}

// Lookup finds a set by name, ignoring case. The first call loads the
// catalog (about 18,000 sets).
func Lookup(set string) (Entry, bool) {
	loadOnce.Do(load)
	entry, ok := entries[strings.ToLower(set)]
	return entry, ok
}

// Version is the MAME release the catalog was generated from.
func Version() string {
	loadOnce.Do(load)
	return version
}

// arcadeHardware are systems whose romsets are MAME sets but which do not
// fall back to the Arcade system.
var arcadeHardware = []string{
	systemdefs.SystemAtomiswave, systemdefs.SystemNAOMI, systemdefs.SystemNAOMI2,
	systemdefs.SystemNeoGeo, systemdefs.SystemNeoGeoAES, systemdefs.SystemNeoGeoMVS,
	systemdefs.SystemModel1, systemdefs.SystemModel2, systemdefs.SystemModel3,
}

// IsArcadeSystem reports whether a system's files are named after MAME sets:
// Arcade itself, the arcade boards that fall back to it (pinball excluded,
// whose tables are not MAME sets), and other arcade hardware MAME emulates.
func IsArcadeSystem(systemID string) bool {
	if systemID == systemdefs.SystemArcade || slices.Contains(arcadeHardware, systemID) {
		return true
	}
	if systemID == systemdefs.SystemPinball {
		return false
	}
	system, err := systemdefs.GetSystem(systemID)
	return err == nil && slices.Contains(system.Fallbacks, systemdefs.SystemArcade)
}

// SetArchive reports whether a file extension is one a set is packed in.
func SetArchive(ext string) bool {
	switch strings.ToLower(ext) {
	case ".zip", ".7z":
		return true
	}
	return false
}

// regions maps MAME's region words to No-Intro's spelling.
var regions = map[string]string{
	"us": "USA", "usa": "USA", "world": "World", "japan": "Japan", "euro": "Europe",
	"europe": "Europe", "asia": "Asia", "korea": "Korea", "taiwan": "Taiwan",
	"hong kong": "Hong Kong", "brazil": "Brazil", "spain": "Spain", "italy": "Italy",
	"germany": "Germany", "france": "France", "uk": "UK", "australia": "Australia",
	"china": "China", "canada": "Canada",
}

var (
	noteGroup = regexp.MustCompile(`\(([^()]*)\)`)
	setNote   = regexp.MustCompile(`(?i)\bset (\d+)\b`)
	revNote   = regexp.MustCompile(`(?i)\brev(?:ision)?\.? ?([0-9a-z][0-9a-z.]*)\b`)
)

// VariantNotes rewrites the notes in a MAME description, such as "US set 1"
// or "Namco rev. B", as No-Intro-style brackets ("(USA)", "(Set 1)",
// "(Rev B)") that filename tag parsing understands. Notes it cannot name,
// such as licensees and build dates, are left out.
func VariantNotes(title string) string {
	var out []string
	for _, group := range noteGroup.FindAllStringSubmatch(title, -1) {
		for part := range strings.SplitSeq(group[1], ",") {
			part = strings.TrimSpace(part)
			lower := strings.ToLower(part)
			for word, region := range regions {
				if lower == word || strings.HasPrefix(lower, word+" ") {
					out = append(out, "("+region+")")
					break
				}
			}
			if m := setNote.FindStringSubmatch(part); m != nil {
				out = append(out, "(Set "+m[1]+")")
			}
			if m := revNote.FindStringSubmatch(part); m != nil {
				out = append(out, "(Rev "+strings.ToUpper(strings.TrimSuffix(m[1], "."))+")")
			}
		}
	}
	return strings.Join(out, " ")
}
