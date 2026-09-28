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

package gamelistxml

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
)

// lookupPlan is every key the record and companion matchers can look up for
// one system's parsed gamelists. The full-system load keeps only the media and
// title rows these keys can reach, so its memory follows the gamelists rather
// than the size of the system. Each set mirrors one lookup:
//
//   - prefixes: MediaByPathFold by resolved path, for entries and companion
//     children, and matchMediaByResolvedPath's fallback over rows under one.
//     Keys are folded resolved paths, which are also the fallback's prefixes
//     without their slash. The value holds the resolved directories that fold
//     to the key; the fallback only runs for one the container index does not
//     know.
//   - sourceKeys: MediaByPathFold by indexed source path.
//   - watchDirs: the container index, for resolved entry and folder paths.
//   - slugs: TitlesBySlug and AllTitlesBySlug, which reach MediaByTitleDBID.
//   - filenameKeys: the companion filename fallback in MediaByFilename.
//   - needCD: matchCanonicalCDMedia, which reads every .m3u and .cue row.
type lookupPlan struct {
	prefixes     map[string]prefixDirs
	sourceKeys   map[string]struct{}
	slugs        map[string]struct{}
	filenameKeys map[string]struct{}
	watchDirs    []string
	// entries counts the gamelist entries that can produce a write. A system
	// with none needs no database rows at all.
	entries int
	needCD  bool
}

func newLookupPlan(entries int) *lookupPlan {
	return &lookupPlan{
		prefixes:     make(map[string]prefixDirs, entries),
		sourceKeys:   make(map[string]struct{}),
		slugs:        make(map[string]struct{}, entries),
		filenameKeys: make(map[string]struct{}),
		watchDirs:    make([]string, 0, entries),
	}
}

// buildLookupPlan collects the keys for parsed. Entries are resolved exactly as
// loadRecordsFromParsed and companionEntriesFromParsed resolve them; a key the
// matchers never end up using only costs a retained row.
func buildLookupPlan(
	cfg *config.Instance, system scraper.ScrapeSystem, parsed *parsedGamelistSystem,
) *lookupPlan {
	entries := 0
	for fi := range parsed.Files {
		entries += len(parsed.Files[fi].Games) + len(parsed.Files[fi].Folders)
	}
	plan := newLookupPlan(entries)
	for fi := range parsed.Files {
		file := &parsed.Files[fi]
		file.keys = make([]entryKeys, len(file.Games))
		for i := range file.Games {
			game := &file.Games[i]
			resolved, romRoot := resolveGamelistROMPath(game.Path, file.RootPath, system.ROMPaths)
			file.keys[i] = entryKeys{resolved: resolved, romRoot: romRoot}
			if resolved != "" {
				file.keys[i].key = pathFoldKey(resolved)
			}
			if isCompanionGame(game) {
				if game.ParentIDAttr == "" || game.Path == "" {
					continue
				}
				if resolved == "" {
					continue
				}
				plan.entries++
				if stem, ok := companionSlugStem(resolved); ok {
					plan.slugs[stem] = struct{}{}
					continue
				}
				plan.addResolved(resolved, file.keys[i].key)
				if key := mediaFilenameKey(resolved); key != "" {
					plan.filenameKeys[key] = struct{}{}
				}
				continue
			}

			// An entry without a resolvable path can still match a MiSTer
			// arcade set by name or an indexed source by its raw path.
			plan.entries++
			if resolved == "" {
				continue
			}
			plan.addResolved(resolved, file.keys[i].key)
			if isCDTrackLikePath(resolved) {
				plan.needCD = true
			}
			slug := entrySlug(cfg, file, i, &system, resolved)
			file.keys[i].slug = slug
			plan.slugs[slug] = struct{}{}
		}
		for i := range file.Folders {
			resolved, _ := resolveGamelistROMPath(file.Folders[i].Path, file.RootPath, system.ROMPaths)
			if resolved == "" {
				continue
			}
			plan.entries++
			plan.watchDirs = append(plan.watchDirs, filepath.ToSlash(resolved))
		}
	}
	return plan
}

// entryFoldKey returns pathFoldKey(resolved) for game i of file, resolved to
// resolved, from the plan's cache when there is one.
func entryFoldKey(file *parsedGamelistFile, i int, resolved string) string {
	if len(file.keys) > i && file.keys[i].key != "" {
		return file.keys[i].key
	}
	return pathFoldKey(resolved)
}

// entrySlug returns the title slug of game i of file, resolved to resolved,
// from the plan's cache when there is one.
func entrySlug(
	cfg *config.Instance, file *parsedGamelistFile, i int, system *scraper.ScrapeSystem, resolved string,
) string {
	if len(file.keys) > i && file.keys[i].slug != "" {
		return file.keys[i].slug
	}
	return mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
		Config:       cfg,
		Path:         resolved,
		SystemID:     system.ID,
		NoExt:        true,
		ProvidedName: file.Games[i].Name,
	}).Slug
}

// prefixDirs is the resolved directories that fold to one prefix. Almost
// every prefix has one, so the first is held inline.
type prefixDirs struct {
	first string
	more  []string
}

// reaches reports whether a MediaByPathFold lookup can be made with key.
func (p *lookupPlan) reaches(key string) bool {
	if _, ok := p.prefixes[key]; ok {
		return true
	}
	_, ok := p.sourceKeys[key]
	return ok
}

func (p *lookupPlan) addResolved(resolved, key string) {
	dir := filepath.ToSlash(resolved)
	p.watchDirs = append(p.watchDirs, dir)
	if dirs, ok := p.prefixes[key]; ok {
		if dir != dirs.first && !slices.Contains(dirs.more, dir) {
			dirs.more = append(dirs.more, dir)
			p.prefixes[key] = dirs
		}
		return
	}
	p.prefixes[key] = prefixDirs{first: dir}
}

// addSources registers the rows indexed sources claim by path.
func (p *lookupPlan) addSources(sources []database.MediaSource) {
	for i := range sources {
		p.sourceKeys[pathFoldKey(sources[i].MediaPath)] = struct{}{}
	}
}

// underPrefix reports whether foldKey sits below one of prefixes, which are
// folded directory paths without a trailing slash.
func underPrefix(foldKey string, prefixes map[string]struct{}) bool {
	if len(prefixes) == 0 {
		return false
	}
	found := false
	eachFoldAncestor(foldKey, func(prefix string) bool {
		_, found = prefixes[prefix]
		return !found
	})
	return found
}

// eachFoldAncestor calls fn with every directory above a folded path, deepest
// first and without a trailing slash, until fn returns false. They are the
// strings a folded key must start with, plus a slash, to sit below them.
func eachFoldAncestor(foldKey string, fn func(string) bool) {
	rest := foldKey
	for {
		idx := strings.LastIndex(rest, "/")
		if idx < 0 {
			return
		}
		rest = rest[:idx]
		if !fn(rest) {
			return
		}
	}
}
