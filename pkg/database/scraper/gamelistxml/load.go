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
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
)

// plannedLoadStats describes one system's planned load for the scrape log.
type plannedLoadStats struct {
	titleLoad, allTitleLoad, mediaLoad, scrapedIDsLoad time.Duration
	// systemMedia counts present, unscraped media rows in the system, which is
	// what the matchers would have held before the load was planned.
	systemMedia   int
	retainedMedia int
	passes        int
}

// retainedRow is a media row kept by a load pass, with its position in the
// system's stream so the indexes are built in the order a full load sees.
type retainedRow struct {
	key   string
	media database.Media
	seq   int
}

// arcadeCandidate is a present .mra row. Set names are read from disk only
// after the stream, never while the database read is open.
type arcadeCandidate struct {
	media    database.Media
	seq      int
	scraped  bool
	retained bool
}

// mediaPass is one stream over a system's media under a fixed set of keys.
type mediaPass struct {
	plan           *lookupPlan
	scrapedIDs     map[int64]struct{}
	neededTitles   map[int64]struct{}
	forcedKeys     map[string]struct{}
	forcedPrefixes map[string]struct{}
	containers     *container.WatchedIndex
	// underPrefix counts, per plan prefix, the unscraped rows below it that no
	// key kept. Only the first pass fills it.
	underPrefix   map[string]int
	retained      []retainedRow
	arcade        []arcadeCandidate
	systemMedia   int
	wantArcade    bool
	countPrefixes bool
}

func (p *mediaPass) add(m *database.MediaWithFullPath, seq int) {
	if m.IsMissing {
		return
	}
	row := database.Media{
		DBID:           m.DBID,
		MediaTitleDBID: m.MediaTitleDBID,
		Path:           m.Path,
		ParentDir:      m.ParentDir,
	}
	p.containers.Add(&row)
	_, scraped := p.scrapedIDs[m.DBID]
	if p.wantArcade && strings.EqualFold(filepath.Ext(m.Path), ".mra") {
		p.arcade = append(p.arcade, arcadeCandidate{media: row, seq: seq, scraped: scraped})
	}
	if scraped {
		return
	}
	p.systemMedia++

	key := pathFoldKey(m.Path)
	if !p.keeps(key, &row) {
		if p.countPrefixes {
			eachFoldAncestor(key, func(prefix string) bool {
				if _, ok := p.plan.prefixes[prefix]; ok {
					p.underPrefix[prefix]++
				}
				return true
			})
		}
		return
	}
	p.retained = append(p.retained, retainedRow{key: key, media: row, seq: seq})
	if p.wantArcade && len(p.arcade) > 0 && p.arcade[len(p.arcade)-1].seq == seq {
		p.arcade[len(p.arcade)-1].retained = true
	}
}

// keeps reports whether any lookup can reach an unscraped row. Every rule but
// the title one depends on the folded path alone, so rows that share a folded
// path, and with it a MediaByPathFold entry, are kept or dropped together
// wherever that entry can be looked up.
func (p *mediaPass) keeps(key string, row *database.Media) bool {
	if p.plan.reaches(key) {
		return true
	}
	if _, ok := p.forcedKeys[key]; ok {
		return true
	}
	if _, ok := p.neededTitles[row.MediaTitleDBID]; ok {
		return true
	}
	if len(p.plan.filenameKeys) > 0 {
		if _, ok := p.plan.filenameKeys[mediaFilenameKey(row.Path)]; ok {
			return true
		}
	}
	if p.plan.needCD {
		switch strings.ToLower(filepath.Ext(row.Path)) {
		case ".m3u", ".cue":
			return true
		}
	}
	return underPrefix(key, p.forcedPrefixes)
}

// loadPlannedIndexes builds the record indexes for one system holding only the
// rows plan can reach. The result answers every lookup the matchers make
// exactly as indexes over the whole system would, which the equivalence tests
// check against fullRecordIndexes.
//
// Media is streamed twice at most. The second pass runs only when the first
// finds rows the path fallback or a container can reach that no key named: a
// resolved path that differs in case from the indexed one, or a disc folder
// whose launch target was not itself named.
func (g *GamelistXMLScraper) loadPlannedIndexes(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	scraperID string,
	system scraper.ScrapeSystem,
	mdb database.MediaDBI,
	plan *lookupPlan,
	parsed *parsedGamelistSystem,
) (loadRecordIndexes, *plannedIndexes, error) {
	var stats plannedLoadStats
	indexes := loadRecordIndexes{
		TitlesBySlug:    make(map[string]database.MediaTitle, len(plan.slugs)),
		AllTitlesBySlug: make(map[string]database.MediaTitle, len(plan.slugs)),
	}

	if err := loadPlannedTitles(ctx, opts, scraperID, system, mdb, plan, &indexes, &stats); err != nil {
		return indexes, nil, err
	}
	neededTitles := make(map[int64]struct{}, len(indexes.TitlesBySlug)+len(indexes.AllTitlesBySlug))
	for _, title := range indexes.TitlesBySlug {
		neededTitles[title.DBID] = struct{}{}
	}
	for _, title := range indexes.AllTitlesBySlug {
		neededTitles[title.DBID] = struct{}{}
	}

	scrapedStart := time.Now()
	scrapedIDs, err := loadScrapedIDs(ctx, opts, scraperID, system, mdb)
	stats.scrapedIDsLoad = time.Since(scrapedStart)
	if err != nil {
		return indexes, nil, err
	}

	pass := &mediaPass{
		plan:          plan,
		scrapedIDs:    scrapedIDs,
		neededTitles:  neededTitles,
		wantArcade:    g.matchArcadeSets && len(arcadeWantedSets(parsed)) > 0,
		underPrefix:   make(map[string]int),
		countPrefixes: len(plan.prefixes) > 0,
	}
	mediaStart := time.Now()
	if err = runMediaPass(ctx, mdb, system.ID, pass); err != nil {
		stats.mediaLoad = time.Since(mediaStart)
		return indexes, nil, err
	}
	stats.passes = 1

	forcedPrefixes, forcedKeys := secondPassKeys(plan, pass)
	if len(forcedPrefixes) > 0 || len(forcedKeys) > 0 {
		pass = &mediaPass{
			plan:           plan,
			scrapedIDs:     scrapedIDs,
			neededTitles:   neededTitles,
			forcedKeys:     forcedKeys,
			forcedPrefixes: forcedPrefixes,
			wantArcade:     pass.wantArcade,
		}
		if err = runMediaPass(ctx, mdb, system.ID, pass); err != nil {
			stats.mediaLoad = time.Since(mediaStart)
			return indexes, nil, err
		}
		stats.passes = 2
	}
	stats.mediaLoad = time.Since(mediaStart)

	indexes.Containers = pass.containers
	retained := pass.retained
	if pass.wantArcade {
		var arcadeRows []retainedRow
		indexes.ArcadeBySetName, arcadeRows, err = g.indexArcadeCandidates(ctx, pass.arcade, parsed)
		if err != nil {
			return indexes, nil, err
		}
		if len(arcadeRows) > 0 {
			retained = append(retained, arcadeRows...)
			slices.SortFunc(retained, func(a, b retainedRow) int { return a.seq - b.seq })
		}
	} else {
		indexes.ArcadeBySetName = make(map[string][]database.Media)
	}

	indexes.MediaByPathFold = make(map[string]database.Media, len(retained))
	indexes.MediaByTitleDBID = make(map[int64][]database.Media, len(retained))
	indexes.MediaByFilename = make(map[string][]database.Media, len(retained))
	for i := range retained {
		media := retained[i].media
		indexes.MediaByPathFold[retained[i].key] = media
		indexes.MediaByTitleDBID[media.MediaTitleDBID] = append(indexes.MediaByTitleDBID[media.MediaTitleDBID], media)
		if key := mediaFilenameKey(media.Path); key != "" {
			indexes.MediaByFilename[key] = append(indexes.MediaByFilename[key], media)
		}
	}
	for slug, title := range indexes.TitlesBySlug {
		if len(indexes.MediaByTitleDBID[title.DBID]) == 0 {
			delete(indexes.TitlesBySlug, slug)
		}
	}
	stats.systemMedia = pass.systemMedia
	stats.retainedMedia = len(retained)
	return indexes, &plannedIndexes{UnretainedMedia: pass.systemMedia - len(retained), Stats: stats}, nil
}

func runMediaPass(ctx context.Context, mdb database.MediaDBI, systemID string, pass *mediaPass) error {
	pass.containers = container.NewWatchedIndex(pass.plan.watchDirs)
	seq := 0
	err := mdb.ForEachMediaBySystemID(ctx, systemID, func(m *database.MediaWithFullPath) error {
		pass.add(m, seq)
		seq++
		return nil
	})
	if err != nil {
		return fmt.Errorf("stream media for %s: %w", systemID, err)
	}
	return nil
}

// secondPassKeys returns what the first pass could not keep from keys alone.
//
// A prefix needs its rows when the path fallback can scan it, which is when
// one of its resolved directories is unknown to the container index, and rows
// sit below it that no key kept. A container's launch target needs every row
// sharing its folded path when that path was not a key already.
func secondPassKeys(plan *lookupPlan, pass *mediaPass) (prefixes, keys map[string]struct{}) {
	prefixes = make(map[string]struct{})
	for prefix, count := range pass.underPrefix {
		if count == 0 {
			continue
		}
		dirs := plan.prefixes[prefix]
		if !pass.containers.HasMedia(dirs.first) || slices.ContainsFunc(dirs.more, func(dir string) bool {
			return !pass.containers.HasMedia(dir)
		}) {
			prefixes[prefix] = struct{}{}
		}
	}
	keys = make(map[string]struct{})
	for _, dir := range plan.watchDirs {
		// A scraped target still counts: its folded path can hold an unscraped
		// row of another case, which the lookup would then return.
		target := pass.containers.Resolve(dir)
		if target == nil {
			continue
		}
		key := pathFoldKey(target.Path)
		if plan.reaches(key) {
			continue
		}
		keys[key] = struct{}{}
	}
	return prefixes, keys
}

// loadPlannedTitles fills the title maps with the titles plan's slugs name,
// from the same queries and in the same order as a full load.
func loadPlannedTitles(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	scraperID string,
	system scraper.ScrapeSystem,
	mdb database.MediaDBI,
	plan *lookupPlan,
	indexes *loadRecordIndexes,
	stats *plannedLoadStats,
) error {
	if len(plan.slugs) == 0 {
		return nil
	}
	keepAll := func(t *database.TitleWithSystem) error {
		if _, ok := plan.slugs[t.Slug]; !ok {
			return nil
		}
		title := database.MediaTitle{DBID: t.DBID, SystemDBID: t.SystemDBID, Slug: t.Slug, Name: t.Name}
		if opts.Force {
			indexes.TitlesBySlug[t.Slug] = title
		}
		indexes.AllTitlesBySlug[t.Slug] = title
		return nil
	}
	if !opts.Force {
		sentinel := scraper.SentinelTagInfo(scraperID)
		start := time.Now()
		err := mdb.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, sentinel.Type+":"+sentinel.Tag,
			func(t *database.MediaTitle) error {
				if _, ok := plan.slugs[t.Slug]; ok {
					indexes.TitlesBySlug[t.Slug] = *t
				}
				return nil
			})
		stats.titleLoad = time.Since(start)
		if err != nil {
			return fmt.Errorf("stream unscraped titles for %s: %w", system.ID, err)
		}
	}
	start := time.Now()
	err := mdb.ForEachTitleBySystemID(ctx, system.ID, keepAll)
	if opts.Force {
		stats.titleLoad = time.Since(start)
	} else {
		stats.allTitleLoad = time.Since(start)
	}
	if err != nil {
		return fmt.Errorf("stream titles for %s: %w", system.ID, err)
	}
	return nil
}

func loadScrapedIDs(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	scraperID string,
	system scraper.ScrapeSystem,
	mdb database.MediaDBI,
) (map[int64]struct{}, error) {
	var (
		ids map[int64]struct{}
		err error
	)
	switch {
	case opts.Force && shouldUseRunMarker(opts):
		ids, err = mdb.GetScrapeRunMediaIDs(ctx, scraperID, opts.RunID, system.DBID)
	case opts.Force:
		return map[int64]struct{}{}, nil
	default:
		ids, err = mdb.GetScrapedMediaIDs(ctx, scraperID, system.DBID)
	}
	if err != nil {
		return nil, fmt.Errorf("load scraped media for %s: %w", system.ID, err)
	}
	if ids == nil {
		ids = map[int64]struct{}{}
	}
	return ids, nil
}
