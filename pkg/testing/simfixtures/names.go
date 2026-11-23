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

package simfixtures

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/go-slugs"
	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/testdata"
)

// title is one game to write. Metadata fields are empty where the source has
// none; the gamelist fills them with fictional values.
type title struct {
	// name is the display title, as a gamelist names the game.
	name string
	// stem is the undecorated filename stem: the sanitised title, or the
	// set name for an arcade set.
	stem      string
	region    string
	date      string
	developer string
	publisher string
	genre     string
	players   string
	// clones are arcade clone sets of this parent set.
	clones []string
	// set marks an arcade set, whose stem is never decorated.
	set bool
	// decorated marks a stem that already carries dump tags (a DAT name).
	decorated bool
}

// titleSlug is the slug the indexer gives a filename stem and the
// gamedatabase scraper gives a dataset title; the scraper joins on it.
func titleSlug(s string) string {
	return slugs.Slugify(slugs.MediaTypeGame, tags.ParseTitleFromFilename(s, false))
}

// sanitizeStem makes a title safe as a file name on every platform, the way
// No-Intro writes colons as " - ".
func sanitizeStem(s string) string {
	s = strings.ReplaceAll(s, ": ", " - ")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
		case strings.ContainsRune(`?*"<>|`, r):
		case r == ':' || r == '/' || r == '\\':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	return strings.TrimRight(out, ". ")
}

// gameDBTitles picks n distinct titles from the bundles, deterministically
// for rng. It reads the bundles twice, once to count the usable rows and
// once to keep the chosen ones, so only the sample is held in memory. A row
// is usable when its sanitised filename slugs the same as its title, so the
// gamedatabase scraper will match the file to the row.
func gameDBTitles(bundles []string, n int, rng *rand.Rand) ([]title, error) {
	arcade := slices.Contains(bundles, "arcade")
	walk := func(fn func(i int, row *gdb.Row, set, parent string)) error {
		seen := make(map[string]bool)
		i := 0
		for _, bundle := range bundles {
			err := gdb.ForEach(bundle, func(row *gdb.Row) error {
				rk, ok := usableRow(row, arcade)
				if !ok {
					if arcade && rk.parent != "" {
						fn(-1, row, rk.set, rk.parent)
					}
					return nil
				}
				if seen[rk.key] {
					return nil
				}
				seen[rk.key] = true
				fn(i, row, rk.set, rk.parent)
				i++
				return nil
			})
			if err != nil {
				return fmt.Errorf("read gamedatabase bundle %s: %w", bundle, err)
			}
		}
		return nil
	}

	count := 0
	if err := walk(func(i int, _ *gdb.Row, _, _ string) {
		if i >= 0 {
			count++
		}
	}); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	chosen := make(map[int]bool, min(n, count))
	for _, i := range rng.Perm(count)[:min(n, count)] {
		chosen[i] = true
	}
	var out []title
	bySet := make(map[string]int)
	clones := make(map[string][]string)
	if err := walk(func(i int, row *gdb.Row, set, parent string) {
		if i < 0 {
			clones[parent] = append(clones[parent], set)
			return
		}
		if !chosen[i] {
			return
		}
		t := title{
			name:      row.Title,
			stem:      sanitizeStem(row.Title),
			region:    noIntroRegion(row.Region),
			date:      row.Date,
			developer: row.Developer,
			publisher: row.Publisher,
			genre:     tagValue(row.Tags, "genre"),
			players:   tagValue(row.Tags, "players"),
		}
		if arcade {
			t.stem, t.set = set, true
			bySet[set] = len(out)
		}
		out = append(out, t)
	}); err != nil {
		return nil, err
	}
	for parent, sets := range clones {
		if i, ok := bySet[parent]; ok {
			slices.Sort(sets)
			out[i].clones = slices.Compact(sets)
		}
	}
	return out, nil
}

// rowKey is what usableRow learns about a GameDataBase row: the key it is
// deduplicated by and, for arcade, its MAME set and parent set.
type rowKey struct {
	key    string
	set    string
	parent string
}

// usableRow reports whether row can become a title and the key it is
// deduplicated by. For an arcade row the key is its MAME set; a clone set is
// never usable as a title but is returned with its parent so the caller can
// attach it.
func usableRow(row *gdb.Row, arcade bool) (rowKey, bool) {
	if arcade {
		set, parent := mameSet(row.Tags)
		if set == "" || parent != "" || !validSetName(set) {
			return rowKey{set: set, parent: parent}, false
		}
		return rowKey{key: set, set: set}, true
	}
	stem := sanitizeStem(row.Title)
	if stem == "" || stem[0] == '.' {
		return rowKey{}, false
	}
	slug := titleSlug(stem)
	if slug == "" || slug != titleSlug(row.Title) {
		return rowKey{}, false
	}
	return rowKey{key: strings.ToLower(stem)}, true
}

func validSetName(set string) bool {
	for _, r := range set {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return set != ""
}

// mameSet reads "#mamerom:set" or "#mamerom:parent>clone" from a tags cell.
func mameSet(cell string) (set, parent string) {
	value := tagCell(cell, "mamerom")
	if value == "" {
		return "", ""
	}
	if p, c, found := strings.Cut(value, ">"); found {
		return c, p
	}
	return value, ""
}

// tagCell returns the raw value of "#key:value" in a tags cell.
func tagCell(cell, key string) string {
	for _, tok := range strings.Fields(cell) {
		if v, ok := strings.CutPrefix(tok, "#"+key+":"); ok {
			return v
		}
	}
	return ""
}

// tagValue returns the first plain word of a tag's value: "action" for
// "#genre:action>platformer:racing", "2" for "#players:2:coop".
func tagValue(cell, key string) string {
	v := tagCell(cell, key)
	if i := strings.IndexAny(v, ":>"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return ""
	}
	return strings.ToUpper(v[:1]) + v[1:]
}

// noIntroRegion maps a GameDataBase region to the name No-Intro uses in a
// file name.
func noIntroRegion(region string) string {
	parts := strings.FieldsFunc(region, func(r rune) bool { return r == '/' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part = strings.TrimSpace(part); part {
		case "South Korea":
			out = append(out, "Korea")
		case "", "Unknown", "Unreleased":
		default:
			out = append(out, sanitizeStem(part))
		}
	}
	return strings.Join(out, ", ")
}

// Vocabulary for synthetic titles. Every combination is fictional.
var (
	synthPrefixes = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"Super", "Mega", "Ultra", "Final", "Grand", "Dark", "Crystal", "Shadow",
		"Iron", "Bright", "Neo", "Hyper", "Royal", "Star", "Turbo", "Silent",
		"Cosmic", "Wild", "Lost", "Hidden",
	}
	synthMiddles = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"Pixel", "Fighter", "Quest", "Fantasy", "Dragon", "Knight", "Warrior",
		"Battle", "Storm", "Legend", "World", "Racer", "Ninja", "Robot",
		"Galaxy", "Island", "Castle", "Circuit", "Puzzle", "Harbor",
	}
	synthSuffixes = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"Bros", "Adventure", "Saga", "Chronicles", "Wars", "Legacy", "Origins",
		"Legends", "Rising", "Revolution", "Arena", "Force", "Tales", "Rally",
		"Odyssey", "Academy", "Gauntlet", "Frontier", "Party", "Trilogy",
	}
	synthSequels = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"", "", "", "", " II", " III", " 2", " 3", " 64", " Advance",
	}
	synthRegions = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"USA", "USA", "USA", "Europe", "Europe", "Japan", "Japan", "World", "USA, Europe", "Germany",
		"France", "Spain", "Australia", "Korea", "Brazil",
	}
	synthCompanies = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"Fictional Soft", "Placeholder Games", "Sample Interactive", "Test Pattern Studios",
		"Dummy Data Co.", "Mock Arts", "Stub Entertainment", "Null Pointer Works",
	}
	synthGenres = []string{ //nolint:gochecknoglobals // Static vocabulary.
		"Action", "Adventure", "Platform", "Puzzle", "Racing", "Rpg", "Shmup", "Sports", "Fighting",
	}
)

// syntheticTitles returns n fictional titles whose slugs differ from each
// other and from every key in taken, which it extends. A drawn name that is
// already taken gets a number, so large counts stay linear.
func syntheticTitles(rng *rand.Rand, n int, taken map[string]bool) []title {
	out := make([]title, 0, n)
	counter := 0
	for len(out) < n {
		name := synthPrefixes[rng.IntN(len(synthPrefixes))] + " " +
			synthMiddles[rng.IntN(len(synthMiddles))] + " " +
			synthSuffixes[rng.IntN(len(synthSuffixes))] +
			synthSequels[rng.IntN(len(synthSequels))]
		key := titleSlug(name)
		for taken[key] {
			counter++
			numbered := name + " " + strconv.Itoa(counter)
			key = titleSlug(numbered)
			if !taken[key] {
				name = numbered
			}
		}
		taken[key] = true
		out = append(out, title{name: name, stem: name})
	}
	return out
}

// datIndex holds DAT game names by system ID.
type datIndex map[string][]datGame

type datGame struct {
	full  string
	clean string
}

// loadDATs reads every .dat and .xml file in dir, filing each DAT's games
// under the system its header (or, failing that, its file name) names.
func loadDATs(dir string) (datIndex, error) {
	if dir == "" {
		return nil, errors.New("simfixtures: source dats needs DatsDir")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("simfixtures: read DatsDir: %w", err)
	}
	idx := make(datIndex)
	parsed := 0
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.IsDir() || (ext != ".dat" && ext != ".xml") {
			continue
		}
		dat, err := testdata.ParseDATFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("simfixtures: DAT %s: %w", entry.Name(), err)
		}
		parsed++
		systemID, err := testdata.MatchSystemID(dat.Header.Name)
		if err != nil {
			systemID, err = testdata.MatchSystemID(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		}
		if err != nil {
			continue
		}
		for _, game := range dat.Games {
			full := game.Name
			if full == "" {
				full = game.Description
			}
			full = sanitizeStem(full)
			clean := sanitizeStem(slugs.StripMetadataBrackets(full))
			if clean == "" || full[0] == '.' {
				continue
			}
			idx[systemID] = append(idx[systemID], datGame{full: full, clean: clean})
		}
	}
	if parsed == 0 {
		return nil, fmt.Errorf("simfixtures: no .dat or .xml files in DatsDir %s", dir)
	}
	return idx, nil
}

// datTitles picks n distinct games from a system's DATs. With realistic the
// full dump name is kept; otherwise its tags are stripped.
func datTitles(games []datGame, n int, realistic bool, rng *rand.Rand) []title {
	seen := make(map[string]bool)
	var unique []datGame
	for _, g := range games {
		key := strings.ToLower(g.clean)
		if realistic {
			key = strings.ToLower(g.full)
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, g)
		}
	}
	perm := rng.Perm(len(unique))[:min(n, len(unique))]
	slices.Sort(perm)
	out := make([]title, 0, len(perm))
	for _, i := range perm {
		g := unique[i]
		t := title{name: g.clean, stem: g.clean}
		if realistic {
			t.stem, t.decorated = g.full, true
		}
		out = append(out, t)
	}
	return out
}

// pickTitles returns n titles for a system from the configured source,
// filling up with synthetic titles when the source runs out, and the source
// that supplied them.
func pickTitles(opts *Options, systemID string, dats datIndex, rng *rand.Rand) ([]title, string, error) {
	var picked []title
	source := SourceSynthetic
	switch opts.Source {
	case SourceGameDatabase:
		if bundles := gamedatabase.SystemBundles(systemID); len(bundles) > 0 {
			var err error
			if picked, err = gameDBTitles(bundles, opts.PerSystem, rng); err != nil {
				return nil, "", err
			}
			if len(picked) > 0 {
				source = SourceGameDatabase
			}
		}
	case SourceDATs:
		if games := dats[systemID]; len(games) > 0 {
			picked = datTitles(games, opts.PerSystem, opts.Realistic, rng)
			source = SourceDATs
		}
	}
	if len(picked) < opts.PerSystem {
		taken := make(map[string]bool, len(picked))
		for i := range picked {
			taken[titleSlug(picked[i].stem)] = true
		}
		picked = append(picked, syntheticTitles(rng, opts.PerSystem-len(picked), taken)...)
	}
	return picked, source, nil
}
