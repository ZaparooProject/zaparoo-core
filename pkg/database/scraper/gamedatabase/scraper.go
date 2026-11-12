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

// Package gamedatabase fills in game metadata from the embedded copy of
// PigSaint's GameDataBase: genre, players, controls, save and add-on
// hardware, franchise, ports, compilation contents, release year, developer
// and publisher. Console titles are matched by title slug and arcade sets by
// MAME set name; nothing is matched by file hash.
package gamedatabase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// ID is the scraper's registered ID.
	ID          = "gamedatabase"
	scraperID   = ID
	scraperName = "GameDataBase"
)

// fingerprintVersion is bumped when a change to matching, to the vocabulary
// tables or to what is written means an unchanged system has to be scraped
// again.
const fingerprintVersion = 1

// SetNameCache is an optional fast path from an indexed arcade descriptor to
// its MAME set name, as the MiSTer platforms keep one. ok=false falls back to
// reading the descriptor. A nil cache is valid.
type SetNameCache func(path string) (setName string, ok bool)

// NewPlatformScraper returns the GameDataBase scraper. cache may be nil.
func NewPlatformScraper(cache SetNameCache) platforms.Scraper {
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: SupportedSystems(),
		SupportsFillMissing: true,
		// The data is keyed by title and set name, whichever launcher found
		// the file.
		AutoScrapeAllLaunchers: true,
		AutoScrapeOrder:        scraper.AutoOrderCatalog,
		Scrape: func(
			ctx context.Context,
			cfg *config.Instance,
			_ platforms.Platform,
			fs afero.Fs,
			db *database.Database,
			opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("gamedatabase: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("gamedatabase: media database is required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			indexed, err := db.MediaDB.IndexedSystems()
			if err != nil {
				return fmt.Errorf("gamedatabase: list indexed systems: %w", err)
			}
			impl := &scraperImpl{
				fs: fs, db: db.MediaDB, cache: cache, unmapped: &scraper.UnmappedValues{},
				regions: []string{string(tags.TagRegionUS), string(tags.TagRegionWorld)},
			}
			if cfg != nil {
				impl.regions = cfg.DefaultRegions()
			}
			go impl.scrapeLoop(ctx, opts, targetSystems(indexed, opts.SystemIDs()), ch)
			return nil
		},
	}
}

// targetSystems intersects the systems the data covers with the systems
// indexed and the systems the request asked for, in sorted order so progress
// steps are stable across runs.
func targetSystems(indexed, requested []string) []string {
	has := func(list []string, id string) bool {
		return slices.ContainsFunc(list, func(candidate string) bool {
			return strings.EqualFold(candidate, id)
		})
	}
	var targets []string
	for _, id := range SupportedSystems() {
		if !has(indexed, id) {
			continue
		}
		if len(requested) > 0 && !has(requested, id) {
			continue
		}
		targets = append(targets, id)
	}
	return targets
}

type scraperImpl struct {
	fs       afero.Fs
	db       database.MediaDBI
	cache    SetNameCache
	unmapped *scraper.UnmappedValues
	// arcade holds the arcade bundle's rows once an arcade system has read
	// them: a device can index a dozen arcade systems, and each uses the
	// whole bundle.
	arcade []*record
	// regions are the configured default regions, most wanted first.
	regions []string
}

type matchStats struct {
	Processed int
	Matched   int
	Skipped   int
}

func (s *scraperImpl) scrapeLoop(
	ctx context.Context, opts scraper.ScrapeOptions, targets []string, ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	defer s.unmapped.LogSummary(scraperID)
	defer debug.FreeOSMemory()
	bgpriority.Apply()

	if opts.Scope != nil {
		s.scrapeScope(ctx, opts, targets, ch)
		return
	}

	started := time.Now()
	var total matchStats
	for step, systemID := range targets {
		stats, err := s.scrapeSystem(ctx, opts, systemID, step, len(targets), ch)
		total.Processed += stats.Processed
		total.Matched += stats.Matched
		total.Skipped += stats.Skipped
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: err, Done: true, SystemID: systemID,
				Processed: total.Processed, Total: total.Processed,
				Matched: total.Matched, Skipped: total.Skipped,
				TotalSteps: len(targets), CurrentStep: step + 1,
			}
			return
		}
	}
	log.Debug().
		Int("systems", len(targets)).
		Int("matched", total.Matched).
		Int("skipped", total.Skipped).
		Dur("total", time.Since(started)).
		Msg("gamedatabase: scrape complete")
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: total.Processed, Total: total.Processed,
		Matched: total.Matched, Skipped: total.Skipped,
		TotalSteps: len(targets), CurrentStep: len(targets),
	}
}

// sourceState identifies the data a system is matched against: the upstream
// commit the embedded copy was built from and the bundles that serve the
// system. It is constant for one build of Core.
func sourceState(src source) string {
	return gdb.Version() + " " + strings.Join(src.primary, ",") + " " + strings.Join(src.secondary, ",")
}

// mediaRow is the part of an indexed media row the scrape reads.
type mediaRow struct {
	path      string
	dbid      int64
	titleDBID int64
}

func (s *scraperImpl) scrapeSystem(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systemID string,
	step, steps int,
	ch chan<- scraper.ScrapeUpdate,
) (stats matchStats, err error) {
	if waitErr := waitForScrape(ctx, opts); waitErr != nil {
		return matchStats{}, waitErr
	}
	src, covered := sourceFor(systemID)
	if !covered {
		return matchStats{}, nil
	}
	state := sourceState(src)
	// An index-triggered run ends here when the system's rows are as its last
	// completed run left them; every run over whole systems records the state
	// it finishes in.
	if opts.FillMissing && scraper.SystemUnchanged(ctx, s.db, scraperID, systemID, fingerprintVersion, state) {
		log.Debug().Str("system", systemID).Msg("gamedatabase: data and library unchanged, system skipped")
		ch <- scraper.ScrapeUpdate{SystemID: systemID, TotalSteps: steps, CurrentStep: step + 1}
		return matchStats{}, nil
	}
	defer func() {
		if err != nil {
			return
		}
		if rememberErr := scraper.RememberSystem(
			ctx, s.db, scraperID, systemID, fingerprintVersion, state,
		); rememberErr != nil {
			log.Debug().Err(rememberErr).Msg("gamedatabase: system fingerprint not stored")
		}
	}()
	// Hand the system's working set back to the OS before the next one, so a
	// run's peak is one system rather than the sum of them.
	defer debug.FreeOSMemory()

	started := time.Now()
	system, err := s.db.FindSystemBySystemID(systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return matchStats{}, nil
	}
	if err != nil {
		return matchStats{}, fmt.Errorf("gamedatabase: look up system %s: %w", systemID, err)
	}
	completed, err := s.completedMedia(ctx, opts, system.DBID)
	if err != nil {
		return matchStats{}, err
	}

	bySlug, err := s.loadTitles(ctx, systemID)
	if err != nil {
		return matchStats{}, err
	}
	cat, err := s.loadCatalog(ctx, systemID, src, bySlug)
	if err != nil {
		return matchStats{}, err
	}
	loaded := time.Now()

	arcade := arcadenames.IsArcadeSystem(systemID)
	var rows []mediaRow
	err = s.db.ForEachMediaBySystemID(ctx, systemID, func(row *database.MediaWithFullPath) error {
		if row.IsMissing {
			return nil
		}
		if _, done := completed[row.DBID]; done {
			return nil
		}
		stats.Processed++
		if _, matched := cat.byTitle[row.MediaTitleDBID]; !matched && (!arcade || !hasSetName(row.Path)) {
			stats.Skipped++
			return nil
		}
		rows = append(rows, mediaRow{path: row.Path, dbid: row.DBID, titleDBID: row.MediaTitleDBID})
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return matchStats{}, ctxErr
		}
		return matchStats{}, fmt.Errorf("gamedatabase: load media for %s: %w", systemID, err)
	}

	targets, unmatched, err := s.buildTargets(ctx, opts, cat, rows)
	if err != nil {
		return matchStats{}, err
	}
	stats.Skipped += unmatched
	for i := range targets {
		targets[i].Write.FillMissing = opts.FillMissing
	}
	matched := time.Now()

	report := func(processed, written int) {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemID, Processed: processed, Total: stats.Processed,
			Matched: written, Skipped: stats.Skipped,
			TotalSteps: steps, CurrentStep: step + 1,
		}:
		case <-ctx.Done():
		}
	}
	if stats.Processed > 0 {
		report(stats.Skipped, 0)
	}
	written := 0
	if err := scraper.ApplyTargets(ctx, s.db, opts, scraperID, targets, func(from, to int) {
		written += to - from
		if to < len(targets) {
			report(stats.Skipped+written, written)
		}
	}); err != nil {
		//nolint:wrapcheck // scraper.ApplyTargets names the scraper in its errors
		return matchStats{Processed: stats.Processed, Matched: written, Skipped: stats.Skipped}, err
	}
	report(stats.Processed, written)
	log.Debug().Str("system", systemID).
		Int("titles", len(bySlug)).Int("matchedTitles", len(cat.byTitle)).Int("sets", len(cat.bySet)).
		Int("media", stats.Processed).Int("matched", written).Int("skipped", stats.Skipped).
		Dur("load", loaded.Sub(started)).Dur("match", matched.Sub(loaded)).Dur("write", time.Since(matched)).
		Msg("gamedatabase: system complete")
	return matchStats{Processed: stats.Processed, Matched: written, Skipped: stats.Skipped}, nil
}

// scrapeScope scrapes the media one scoped request selected. A scope names a
// single system.
func (s *scraperImpl) scrapeScope(
	ctx context.Context, opts scraper.ScrapeOptions, targets []string, ch chan<- scraper.ScrapeUpdate,
) {
	selection, err := scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("gamedatabase: %w", err), Done: true}
		return
	}
	media, titles := selection.Pending()
	src, covered := sourceFor(opts.Scope.SystemID)
	if !covered || len(targets) == 0 || len(media) == 0 {
		scraper.ApplyScopedTargets(ctx, s.db, opts, selection, nil, ch)
		return
	}
	bySlug := make(map[string][]int64, len(titles))
	for i := range titles {
		if titles[i].Slug != "" {
			bySlug[titles[i].Slug] = append(bySlug[titles[i].Slug], titles[i].DBID)
		}
	}
	cat, err := s.loadCatalog(ctx, opts.Scope.SystemID, src, bySlug)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
		return
	}
	rows := make([]mediaRow, 0, len(media))
	for i := range media {
		rows = append(rows, mediaRow{path: media[i].Path, dbid: media[i].DBID, titleDBID: media[i].MediaTitleDBID})
	}
	writeTargets, _, err := s.buildTargets(ctx, opts, cat, rows)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
		return
	}
	for i := range writeTargets {
		writeTargets[i].Write.FillMissing = opts.FillMissing
	}
	scraper.ApplyScopedTargets(ctx, s.db, opts, selection, writeTargets, ch)
}

// completedMedia lists the media rows this run must leave alone. A forced or
// fill-missing run skips only rows its own run already committed, so a restart
// resumes; an ordinary run skips every row the scraper has ever written.
func (s *scraperImpl) completedMedia(
	ctx context.Context, opts scraper.ScrapeOptions, systemDBID int64,
) (map[int64]struct{}, error) {
	switch {
	case opts.RunID != "" && (opts.Force || opts.FillMissing):
		ids, err := s.db.GetScrapeRunMediaIDs(ctx, scraperID, opts.RunID, systemDBID)
		if err != nil {
			return nil, fmt.Errorf("gamedatabase: load run markers: %w", err)
		}
		return ids, nil
	case !opts.Force && !opts.FillMissing:
		ids, err := s.db.GetScrapedMediaIDs(ctx, scraperID, systemDBID)
		if err != nil {
			return nil, fmt.Errorf("gamedatabase: load scraped media: %w", err)
		}
		return ids, nil
	default:
		return map[int64]struct{}{}, nil
	}
}

// loadTitles streams a system's titles into a slug index. Two titles can
// share a slug, so each slug lists every title that has it.
func (s *scraperImpl) loadTitles(ctx context.Context, systemID string) (map[string][]int64, error) {
	bySlug := make(map[string][]int64)
	err := s.db.ForEachTitleBySystemID(ctx, systemID, func(row *database.TitleWithSystem) error {
		if row.Slug != "" {
			bySlug[row.Slug] = append(bySlug[row.Slug], row.DBID)
		}
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("gamedatabase: load titles for %s: %w", systemID, err)
	}
	return bySlug, nil
}

// loadCatalog reads the system's bundles and keeps the rows its library can
// use: those whose title slugs to a library title and, for an arcade system,
// those that name a set.
func (s *scraperImpl) loadCatalog(
	ctx context.Context, systemID string, src source, bySlug map[string][]int64,
) (*catalog, error) {
	cat := &catalog{byTitle: make(map[int64][]*record), bySet: make(map[string]*record)}
	mediaType := systemdefs.MediaTypeGame
	if system, err := systemdefs.GetSystem(systemID); err == nil {
		mediaType = system.GetMediaType()
	}
	slugger := newSlugger(mediaType)

	if arcadenames.IsArcadeSystem(systemID) {
		if s.arcade == nil {
			rows, err := loadBundle(ctx, arcadeBundle, 0, slugger, func(*record) bool { return true })
			if err != nil {
				return nil, err
			}
			s.arcade = rows
		}
		for _, rec := range s.arcade {
			cat.addSet(rec)
			addTitles(rec, bySlug, cat.byTitle, nil)
		}
		return cat, nil
	}

	order := 0
	for _, bundle := range src.primary {
		if _, err := loadBundle(ctx, bundle, order, slugger, func(rec *record) bool {
			return addTitles(rec, bySlug, cat.byTitle, nil)
		}); err != nil {
			return nil, err
		}
		order += bundleOrderStride
	}
	// A secondary bundle describes another system's games that this system
	// also runs. It only names titles the system's own bundles did not.
	fallback := make(map[int64][]*record)
	for _, bundle := range src.secondary {
		if _, err := loadBundle(ctx, bundle, order, slugger, func(rec *record) bool {
			return addTitles(rec, bySlug, fallback, cat.byTitle)
		}); err != nil {
			return nil, err
		}
		order += bundleOrderStride
	}
	for titleID, rows := range fallback {
		cat.byTitle[titleID] = rows
	}
	return cat, nil
}

// hasSetName reports whether an arcade media path can carry a MAME set name:
// a MiSTer descriptor, or an archive named after its set.
func hasSetName(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, mra.Ext) || arcadenames.SetArchive(ext)
}

// setName resolves an arcade media row's MAME set name. A descriptor states
// it inside the file, which the platform's cache usually already holds; an
// archive is named after it.
func (s *scraperImpl) setName(path string) string {
	if !strings.EqualFold(filepath.Ext(path), mra.Ext) {
		return strings.ToLower(mra.SetStem(path))
	}
	if s.cache != nil {
		if cached, ok := s.cache(path); ok {
			return strings.ToLower(strings.TrimSpace(cached))
		}
	}
	return mra.ReadSetName(s.fs, path)
}

// buildTargets resolves the kept media rows to their writes. unmatched counts
// the rows no dataset row describes.
func (s *scraperImpl) buildTargets(
	ctx context.Context, opts scraper.ScrapeOptions, cat *catalog, rows []mediaRow,
) (targets []database.ScrapeWriteTarget, unmatched int, err error) {
	// First fill of a shared title is deterministic, independent of query order.
	slices.SortFunc(rows, func(a, b mediaRow) int { return strings.Compare(a.path, b.path) })

	// Set names come first: a set match names one release outright, so its
	// title needs no choosing between regional rows.
	bySetRow := make([]*record, len(rows))
	if len(cat.bySet) > 0 {
		for i := range rows {
			if !hasSetName(rows[i].path) {
				continue
			}
			if waitErr := waitForScrape(ctx, opts); waitErr != nil {
				return nil, 0, waitErr
			}
			bySetRow[i] = cat.bySet[s.setName(rows[i].path)]
		}
	}

	regions, err := s.titleRegions(ctx, cat, rows, bySetRow)
	if err != nil {
		return nil, 0, err
	}
	type choice struct {
		rec  *record
		done bool
	}
	chosen := make(map[int64]choice)
	titleWritten := make(map[int64]struct{})
	for i := range rows {
		row := &rows[i]
		rec := bySetRow[i]
		if rec == nil {
			pick, seen := chosen[row.titleDBID]
			if !seen {
				if candidates := cat.byTitle[row.titleDBID]; len(candidates) > 0 {
					pick.rec, _ = choose(candidates, regions[row.titleDBID], s.regions)
				}
				pick.done = true
				chosen[row.titleDBID] = pick
			}
			rec = pick.rec
		}
		if rec == nil {
			unmatched++
			continue
		}
		b := newWriteBuilder(opts.RunID, s.unmapped)
		if _, written := titleWritten[row.titleDBID]; !written {
			titleWritten[row.titleDBID] = struct{}{}
			b.addTitle(&rec.row, &rec.parsed)
		}
		if bySetRow[i] != nil {
			b.addSet(&rec.parsed)
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: row.dbid, MediaTitleDBID: row.titleDBID, Write: b.write,
		})
	}
	return targets, unmatched, nil
}

// titleRegions reads the regions of the files behind every title that has
// more than one candidate row, which is all the choice between rows needs.
func (s *scraperImpl) titleRegions(
	ctx context.Context, cat *catalog, rows []mediaRow, bySetRow []*record,
) (map[int64]map[tags.TagValue]struct{}, error) {
	var mediaIDs []int64
	titleOf := make(map[int64]int64)
	for i := range rows {
		if bySetRow[i] != nil || len(cat.byTitle[rows[i].titleDBID]) < 2 {
			continue
		}
		mediaIDs = append(mediaIDs, rows[i].dbid)
		titleOf[rows[i].dbid] = rows[i].titleDBID
	}
	regions := make(map[int64]map[tags.TagValue]struct{})
	if len(mediaIDs) == 0 {
		return regions, nil
	}
	tagsByMedia, err := s.db.GetMediaTagsByMediaDBIDs(ctx, mediaIDs)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("gamedatabase: load media regions: %w", err)
	}
	for mediaID, infos := range tagsByMedia {
		titleID := titleOf[mediaID]
		for i := range infos {
			if infos[i].Type != string(tags.TagTypeRegion) {
				continue
			}
			if regions[titleID] == nil {
				regions[titleID] = make(map[tags.TagValue]struct{})
			}
			regions[titleID][tags.TagValue(infos[i].Tag)] = struct{}{}
		}
	}
	return regions, nil
}

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	//nolint:wrapcheck // scraper.Wait names the scraper in its error
	return scraper.Wait(ctx, opts, scraperID)
}
