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

// Package misterdocs imports metadata from MiSTer Downloader content installed
// under docs/<system> directories.
package misterdocs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scraperID   = "mister-docs"
	scraperName = "MiSTer docs databases"
)

// SetNameCache answers an arcade descriptor's set name without reading the
// descriptor. ok is false when the cache cannot answer for that file; an empty
// name with ok true means the descriptor has none.
type SetNameCache func(path string) (setName string, ok bool)

// NewPlatformScraper returns the MiSTer installed-docs scraper. setNames is an
// optional fast path for arcade set names and may be nil.
func NewPlatformScraper(setNames SetNameCache) platforms.Scraper {
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: []string{},
		SupportsFillMissing: true,
		// It only reads packs already on the card and skips a system that has
		// none, so every indexed system queues a fill-missing run.
		AutoScrapeAllLaunchers: true,
		Scrape: func(
			ctx context.Context,
			cfg *config.Instance,
			pl platforms.Platform,
			fs afero.Fs,
			db *database.Database,
			opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("misterdocs: fill-missing and force are mutually exclusive")
			}
			if pl == nil || db == nil || db.MediaDB == nil {
				return errors.New("misterdocs: platform and media database are required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			rootDirs := pl.RootDirs(cfg)
			sources, err := discoverSources(fs, rootDirs)
			if err != nil {
				return err
			}
			indexed, err := db.MediaDB.IndexedSystems()
			if err != nil {
				return fmt.Errorf("misterdocs: list indexed systems: %w", err)
			}
			targets := orderedTargetSystems(indexed, opts.SystemIDs())
			var langs []string
			if cfg != nil {
				langs = cfg.DefaultLangs()
			}
			impl := &scraperImpl{
				fs: fs, db: db.MediaDB, docsRoots: candidateDocsRoots(rootDirs),
				sources: sourcesBySystem(sources), langs: langs, setNames: setNames,
			}
			go impl.scrapeLoop(ctx, opts, targets, ch)
			return nil
		},
	}
}

type scraperImpl struct {
	sources map[string][]sourceDir
	fs      afero.Fs
	db      database.MediaDBI
	// setNames is an optional fast path for arcade set names.
	setNames SetNameCache
	// loaded holds the packs a later step will read again, keyed by folder,
	// and lastUse the last step that reads each folder. Game Boy Color falls
	// back to the Game Boy pack, for one, and parsing it once serves both.
	loaded  map[string]sourceRecords
	lastUse map[string]int
	// unmapped collects the pack values this run had no tag mapping for.
	unmapped  scraper.UnmappedValues
	docsRoots []string
	langs     []string
}

// cleanupBatchSize bounds how many rows' properties a forced run's cleanup
// holds at once.
const cleanupBatchSize = 500

// runTotals accumulates the counters of every finished step.
type runTotals struct {
	processed, matched, skipped int
}

func (t runTotals) done() scraper.ScrapeUpdate {
	return scraper.ScrapeUpdate{Done: true, Processed: t.processed, Matched: t.matched, Skipped: t.skipped}
}

func (s *scraperImpl) scrapeLoop(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	targetSystems []string,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	defer s.unmapped.LogSummary(scraperID)
	bgpriority.Apply()

	steps := s.eligibleTargets(targetSystems, opts.Force)
	if opts.Scope != nil {
		steps = opts.SystemIDs()
	}
	s.planSourceReuse(steps)
	var totals runTotals
	for step := range steps {
		if !s.scrapeStep(ctx, opts, steps, step, &totals, ch) {
			return
		}
		s.releaseSources(step)
	}
	final := totals.done()
	final.TotalSteps, final.CurrentStep = len(steps), len(steps)
	ch <- final
}

// planSourceReuse notes the last step that reads each pack folder, so a pack
// shared between systems is parsed once and dropped as soon as nothing later
// needs it.
func (s *scraperImpl) planSourceReuse(steps []string) {
	s.loaded = make(map[string]sourceRecords)
	s.lastUse = make(map[string]int)
	for step, targetID := range steps {
		for _, sourceID := range sourceIDsForTarget(targetID) {
			for _, source := range s.sources[sourceID] {
				s.lastUse[source.Path] = step
			}
		}
	}
}

// releaseSources drops the packs no step after this one reads.
func (s *scraperImpl) releaseSources(step int) {
	for path := range s.loaded {
		if s.lastUse[path] <= step {
			delete(s.loaded, path)
		}
	}
}

// loadSource returns a pack's records, parsing it unless an earlier step
// already did, and keeps them when a later step reads the same folder.
func (s *scraperImpl) loadSource(ctx context.Context, source sourceDir, step int) (sourceRecords, error) {
	if loaded, ok := s.loaded[source.Path]; ok {
		return loaded, nil
	}
	loaded, err := loadSourceRecords(ctx, s.fs, source, s.langs)
	if err != nil {
		return sourceRecords{}, err
	}
	if s.lastUse[source.Path] > step {
		s.loaded[source.Path] = loaded
	}
	return loaded, nil
}

// stepSources is what one step loaded from the installed packs.
type stepSources struct {
	err          error
	records      []sourceRecords
	cleanupRoots []string
	arcade       bool
}

// loadStepSources reads every pack serving targetID. It returns false when
// the scrape was stopped while loading.
func (s *scraperImpl) loadStepSources(
	ctx context.Context, opts scraper.ScrapeOptions, targetID string, step int,
) (stepSources, bool) {
	var result stepSources
	successfulRoots := make(map[string]struct{})
	for _, sourceID := range sourceIDsForTarget(targetID) {
		for _, source := range s.sources[sourceID] {
			if err := waitForScrape(ctx, opts); err != nil {
				return result, false
			}
			loaded, loadErr := s.loadSource(ctx, source, step)
			if loadErr != nil {
				result.err = errors.Join(
					result.err,
					fmt.Errorf("misterdocs: load %q: %w", source.Path, loadErr),
				)
				continue
			}
			result.records = append(result.records, loaded)
			if source.Kind == sourceArtwork && sourceID == systemdefs.SystemArcade {
				result.arcade = true
			}
			if root := s.docsRootForSource(source.Path); root != "" {
				successfulRoots[root] = struct{}{}
			}
		}
	}
	result.cleanupRoots = make([]string, 0, len(successfulRoots))
	for _, root := range s.docsRoots {
		if _, ok := successfulRoots[root]; ok {
			result.cleanupRoots = append(result.cleanupRoots, root)
		}
	}
	return result, true
}

// scrapeStep scrapes one target system. It returns false when the run has
// ended, having already sent the final update.
//
//nolint:gocognit,gocyclo,cyclop,funlen // one step's load, match, cleanup and write sequence
func (s *scraperImpl) scrapeStep(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	steps []string,
	step int,
	totals *runTotals,
	ch chan<- scraper.ScrapeUpdate,
) bool {
	targetID := steps[step]
	if err := waitForScrape(ctx, opts); err != nil {
		ch <- totals.done()
		return false
	}
	loadedData := false
	defer func() {
		// Hand the step's working set back to the OS before the next one, so
		// a run's peak is one system rather than the sum of them.
		if loadedData {
			debug.FreeOSMemory()
		}
	}()

	var selection scraper.ScopedSelection
	var titles []database.TitleWithSystem
	var media []database.MediaWithFullPath
	if opts.Scope != nil {
		var err error
		selection, err = scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: fmt.Errorf("misterdocs: load titles for %s: %w", targetID, err), Done: true,
			}
			return false
		}
		loadedData = true
		media, titles = selection.Pending()
		if len(media) == 0 {
			scraper.ApplyScopedTargets(ctx, s.db, opts, selection, nil, ch)
			return false
		}
	}

	stepStart := time.Now()
	report := func(processed, total, matched, skipped int) {
		if opts.Scope != nil {
			processed, total, matched, skipped = 0, len(selection.Media), 0, 0
		}
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: targetID, Processed: processed, Total: total, Matched: matched, Skipped: skipped,
			TotalSteps: len(steps), CurrentStep: step + 1,
		}:
		case <-ctx.Done():
		}
	}

	// A run over whole systems fingerprints each one. An index-triggered
	// run ends the step here when the packs and the library are as its last
	// completed run left them; every unscoped run records the state it
	// finishes in.
	sourceState, fingerprinted := "", false
	if opts.Scope == nil {
		state, stateErr := s.sourceState(ctx, opts, targetID)
		switch {
		case stateErr == nil:
			sourceState, fingerprinted = state, true
		case ctx.Err() != nil:
			ch <- totals.done()
			return false
		default:
			log.Debug().Err(stateErr).Str("system", targetID).Msg("misterdocs: could not fingerprint packs")
		}
	}
	if fingerprinted && opts.FillMissing &&
		scraper.SystemUnchanged(ctx, s.db, scraperID, targetID, fingerprintVersion, sourceState) {
		log.Debug().Str("system", targetID).Dur("total", time.Since(stepStart)).
			Msg("misterdocs: packs and library unchanged, step skipped")
		ch <- scraper.ScrapeUpdate{SystemID: targetID, TotalSteps: len(steps), CurrentStep: step + 1}
		return true
	}

	sources, ok := s.loadStepSources(ctx, opts, targetID, step)
	if !ok {
		ch <- totals.done()
		return false
	}
	records := sources.records
	sourceError := sources.err
	loadDuration := time.Since(stepStart)
	totalRecords := 0
	for i := range records {
		totalRecords += len(records[i].Artwork) + len(records[i].Manuals) + records[i].RowErrors
	}
	// A large pack takes minutes to match and write, so tell the API how
	// big the step is before any of that starts.
	if totalRecords > 0 {
		report(0, totalRecords, 0, 0)
	}

	cleanup := opts.Force && sourceError == nil && len(sources.cleanupRoots) > 0
	keys := newMatchKeys(records)
	scanStart := time.Now()
	var load systemLoad
	var loadErr error
	switch {
	case opts.Scope != nil:
		load, loadErr = s.scopedSystemLoad(ctx, opts, titles, media, keys, sources.arcade, cleanup)
	case keys.any || cleanup:
		loadedData = true
		load, loadErr = s.loadSystem(ctx, opts, targetID, keys, sources.arcade, cleanup)
	default:
		// No record can match anything and nothing needs cleaning, so the
		// system's rows are never read.
		load.idx = newSystemIndex(nil, nil)
	}
	if loadErr != nil {
		if ctx.Err() != nil {
			ch <- totals.done()
		} else {
			ch <- scraper.ScrapeUpdate{FatalErr: loadErr, Done: true}
		}
		return false
	}
	scanDuration := time.Since(scanStart)
	matchStart := time.Now()
	matched := buildPendingWrites(load.idx, records, opts, &s.unmapped)
	load.idx = systemIndex{}
	writeTargets, stats := matched.Targets, matched.Stats
	matchDuration := time.Since(matchStart)
	cleanupStart := time.Now()
	if cleanup {
		if _, cleanupErr := s.deleteStaleProperties(
			ctx, opts, load.mediaIDs, load.titleIDs, matched.Found, sources.cleanupRoots,
		); cleanupErr != nil {
			sourceError = cleanupErr
		}
	}
	cleanupDuration := time.Since(cleanupStart)
	if opts.Scope != nil {
		if sourceError != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: sourceError, Done: true}
			return false
		}
		scraper.ApplyScopedTargets(ctx, s.db, opts, selection, writeTargets, ch)
		return false
	}

	// Skipped records are finished once matching is; matched ones finish
	// as their rows commit, so progress advances with each write batch.
	if len(writeTargets) > 0 && stats.Skipped > 0 {
		report(stats.Skipped, stats.Processed, 0, stats.Skipped)
	}
	writeStart := time.Now()
	matchedWritten := 0
	stepError := sourceError
	if err := scraper.ApplyTargets(ctx, s.db, opts, scraperID, writeTargets, func(from, to int) {
		for i := from; i < to; i++ {
			matchedWritten += matched.RecordsPerTarget[i]
		}
		if to < len(writeTargets) {
			report(stats.Skipped+matchedWritten, stats.Processed, matchedWritten, stats.Skipped)
		}
	}); err != nil {
		stepError = errors.Join(stepError, err)
		stats.Skipped++
	}
	writeDuration := time.Since(writeStart)
	if fingerprinted && stepError == nil {
		if rememberErr := scraper.RememberSystem(
			ctx, s.db, scraperID, targetID, fingerprintVersion, sourceState,
		); rememberErr != nil {
			log.Debug().Err(rememberErr).Msg("misterdocs: step fingerprint not stored")
		}
	}
	log.Debug().
		Str("system", targetID).
		Int("records", stats.Processed).
		Int("matched", stats.Matched).
		Int("skipped", stats.Skipped).
		Int("targets", len(writeTargets)).
		Dur("load", loadDuration).
		Dur("scan", scanDuration).
		Dur("match", matchDuration).
		Dur("cleanup", cleanupDuration).
		Dur("write", writeDuration).
		Dur("total", time.Since(stepStart)).
		Msg("misterdocs: step complete")
	totals.processed += stats.Processed
	totals.matched += stats.Matched
	totals.skipped += stats.Skipped
	ch <- scraper.ScrapeUpdate{
		Err: stepError, SystemID: targetID, Processed: stats.Processed, Total: stats.Processed,
		Matched: stats.Matched, Skipped: stats.Skipped, TotalSteps: len(steps), CurrentStep: step + 1,
	}
	return true
}

// matchKeys are the values a step's records can look installed rows up by.
// Matching only ever reads the index under these keys, so rows that answer to
// none of them can be left out of it without changing a single match.
type matchKeys struct {
	// names are record names, which match a media filename or MRA setname.
	names map[string]struct{}
	// tags are record keys, which match the trailing tag of a media filename.
	tags map[string]struct{}
	// slugs are the title slugs slug-unique records and manuals fall back to.
	slugs map[string]struct{}
	any   bool
}

func newMatchKeys(records []sourceRecords) matchKeys {
	keys := matchKeys{
		names: make(map[string]struct{}),
		tags:  make(map[string]struct{}),
		slugs: make(map[string]struct{}),
	}
	for _, source := range records {
		for _, record := range source.Artwork {
			keys.any = true
			keys.names[strings.ToLower(strings.TrimSpace(record.Name))] = struct{}{}
			keys.tags[strings.ToLower(strings.TrimSpace(record.Key))] = struct{}{}
			if record.SlugUnique {
				keys.slugs[slugs.Slugify(slugs.MediaTypeGame, record.Name)] = struct{}{}
			}
		}
		for _, manualPath := range source.Manuals {
			keys.any = true
			name := strings.TrimSuffix(filepath.Base(manualPath), filepath.Ext(manualPath))
			if slug := slugs.Slugify(slugs.MediaTypeGame, name); slug != "" {
				keys.slugs[slug] = struct{}{}
			}
		}
	}
	return keys
}

// wantsMedia reports whether matching can read row: by its filename, by its
// filename's trailing tag, or as a media row of a title a slug can reach.
func (k *matchKeys) wantsMedia(row *database.MediaWithFullPath, slugTitles map[int64]struct{}) bool {
	if base := normalizedMediaBase(row.Path); base != "" {
		if _, ok := k.names[base]; ok {
			return true
		}
		if tag := trailingParenTag(base); tag != "" {
			if _, ok := k.tags[tag]; ok {
				return true
			}
		}
	}
	_, ok := slugTitles[row.MediaTitleDBID]
	return ok
}

// systemLoad is what a step read from the database: the index its records
// match against and, for a forced run's cleanup, the IDs of every row.
type systemLoad struct {
	idx      systemIndex
	mediaIDs []int64
	titleIDs []int64
}

// loadSystem builds a system's match index from streamed rows, keeping only
// the rows matchKeys can reach. Titles are read before media, so a title a
// slug matches keeps every one of its media rows; the titles of the other
// kept media rows are then fetched by ID.
//
//nolint:gocognit // three streamed passes, each with its own filter
func (s *scraperImpl) loadSystem(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systemID string,
	keys matchKeys,
	arcade, cleanup bool,
) (systemLoad, error) {
	var load systemLoad
	var titles []database.TitleWithSystem
	heldTitles := make(map[int64]struct{})
	if len(keys.slugs) > 0 || cleanup {
		err := s.db.ForEachTitleBySystemID(ctx, systemID, func(title *database.TitleWithSystem) error {
			if cleanup {
				load.titleIDs = append(load.titleIDs, title.DBID)
			}
			if _, ok := keys.slugs[title.Slug]; ok {
				titles = append(titles, *title)
				heldTitles[title.DBID] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return load, fmt.Errorf("misterdocs: load titles for %s: %w", systemID, err)
		}
	}

	var media, mraRows []database.MediaWithFullPath
	err := s.db.ForEachMediaBySystemID(ctx, systemID, func(row *database.MediaWithFullPath) error {
		if cleanup {
			load.mediaIDs = append(load.mediaIDs, row.DBID)
		}
		isMRA := arcade && strings.EqualFold(filepath.Ext(row.Path), mraExt)
		wanted := keys.wantsMedia(row, heldTitles)
		if !isMRA && !wanted {
			return nil
		}
		kept := *row
		// Matching never reads these, so the index does not hold them.
		kept.TitleSlug, kept.SortName = "", ""
		if isMRA {
			mraRows = append(mraRows, kept)
		}
		if wanted {
			media = append(media, kept)
		}
		return nil
	})
	if err != nil {
		return load, fmt.Errorf("misterdocs: load media for %s: %w", systemID, err)
	}

	var bySetName map[string][]database.MediaWithFullPath
	if arcade {
		bySetName, err = s.resolveArcadeSetNames(ctx, opts, mraRows, keys.names)
		if err != nil {
			return load, err
		}
	}

	needed := make(map[int64]struct{})
	noteTitle := func(id int64) {
		if _, held := heldTitles[id]; !held {
			needed[id] = struct{}{}
		}
	}
	for i := range media {
		noteTitle(media[i].MediaTitleDBID)
	}
	for _, rows := range bySetName {
		for i := range rows {
			noteTitle(rows[i].MediaTitleDBID)
		}
	}
	if len(needed) > 0 {
		ids := make([]int64, 0, len(needed))
		for id := range needed {
			ids = append(ids, id)
		}
		extra, err := s.db.GetTitlesByDBIDs(ctx, ids)
		if err != nil {
			return load, fmt.Errorf("misterdocs: load titles for %s: %w", systemID, err)
		}
		for i := range extra {
			extra[i].SystemID = systemID
		}
		titles = append(titles, extra...)
	}

	load.idx = newSystemIndex(titles, media)
	if bySetName != nil {
		load.idx.mediaBySetName = bySetName
	}
	return load, nil
}

// scopedSystemLoad indexes a scoped run's selection, which is already bounded
// by its scope.
func (s *scraperImpl) scopedSystemLoad(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	titles []database.TitleWithSystem,
	media []database.MediaWithFullPath,
	keys matchKeys,
	arcade, cleanup bool,
) (systemLoad, error) {
	load := systemLoad{idx: newSystemIndex(titles, media)}
	if arcade {
		bySetName, err := s.resolveArcadeSetNames(ctx, opts, media, keys.names)
		if err != nil {
			return load, err
		}
		load.idx.mediaBySetName = bySetName
	}
	if cleanup {
		load.mediaIDs = make([]int64, len(media))
		for i := range media {
			load.mediaIDs[i] = media[i].DBID
		}
		load.titleIDs = make([]int64, len(titles))
		for i := range titles {
			load.titleIDs[i] = titles[i].DBID
		}
	}
	return load, nil
}

// resolveArcadeSetNames resolves installed MRAs to the setname they declare,
// keeping those a record name can look up. Arcade artwork is filed under the
// MAME parent setname, which lives inside the MRA and never in its filename,
// so without this pass an arcade pack matches nothing. It runs only for
// systems that actually have an arcade source.
func (s *scraperImpl) resolveArcadeSetNames(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	media []database.MediaWithFullPath,
	names map[string]struct{},
) (map[string][]database.MediaWithFullPath, error) {
	started := time.Now()
	bySetName := make(map[string][]database.MediaWithFullPath)
	scanned, resolved := 0, 0
	for i := range media {
		if !strings.EqualFold(filepath.Ext(media[i].Path), mraExt) {
			continue
		}
		if err := waitForScrape(ctx, opts); err != nil {
			return nil, err
		}
		scanned++
		setName, ok := s.arcadeSetName(media[i].Path)
		if !ok {
			continue
		}
		resolved++
		key := strings.ToLower(setName)
		if _, wanted := names[key]; !wanted {
			continue
		}
		bySetName[key] = append(bySetName[key], media[i])
	}
	if scanned > 0 {
		log.Debug().
			Int("mraFiles", scanned).
			Int("resolved", resolved).
			Dur("elapsed", time.Since(started)).
			Msg("misterdocs: resolved arcade setnames")
	}
	return bySetName, nil
}

// arcadeSetName returns a descriptor's set name from the platform's cache
// when it can answer, and from the descriptor itself otherwise.
func (s *scraperImpl) arcadeSetName(path string) (string, bool) {
	if s.setNames != nil {
		if setName, ok := s.setNames(path); ok {
			return setName, setName != ""
		}
	}
	return readMRASetName(s.fs, path)
}

func (s *scraperImpl) eligibleTargets(targets []string, force bool) []string {
	result := make([]string, 0, len(targets))
	for _, target := range targets {
		hasSource := false
		for _, sourceID := range sourceIDsForTarget(target) {
			if len(s.sources[sourceID]) > 0 {
				hasSource = true
				break
			}
		}
		if hasSource || force {
			result = append(result, target)
		}
	}
	return result
}

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	//nolint:wrapcheck // scraper.Wait names the scraper in its error
	return scraper.Wait(ctx, opts, scraperID)
}

// deleteStaleProperties drops the docs image and manual properties a forced
// run no longer finds, reading the rows' properties a batch at a time.
func (s *scraperImpl) deleteStaleProperties(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	mediaIDs []int64,
	titleIDs []int64,
	found map[string]struct{},
	cleanupRoots []string,
) (int, error) {
	deleted := 0
	for start := 0; start < len(mediaIDs); start += cleanupBatchSize {
		batch := mediaIDs[start:min(start+cleanupBatchSize, len(mediaIDs))]
		if err := waitForScrape(ctx, opts); err != nil {
			return deleted, err
		}
		mediaProps, err := s.db.GetMediaPropertyMetadataByMediaDBIDs(ctx, batch)
		if err != nil {
			return deleted, fmt.Errorf("misterdocs: load media properties for cleanup: %w", err)
		}
		for _, mediaID := range batch {
			if err := waitForScrape(ctx, opts); err != nil {
				return deleted, err
			}
			props := mediaProps[mediaID]
			for propIdx := range props {
				prop := &props[propIdx]
				if !isStaleDocsProperty(prop, found, cleanupRoots) || prop.TypeTagDBID == 0 {
					continue
				}
				if err := s.db.DeleteMediaProperty(ctx, mediaID, prop.TypeTagDBID); err != nil {
					return deleted, fmt.Errorf("misterdocs: delete stale media property: %w", err)
				}
				deleted++
			}
		}
	}
	for start := 0; start < len(titleIDs); start += cleanupBatchSize {
		batch := titleIDs[start:min(start+cleanupBatchSize, len(titleIDs))]
		if err := waitForScrape(ctx, opts); err != nil {
			return deleted, err
		}
		titleProps, err := s.db.GetMediaTitlePropertyMetadataByMediaTitleDBIDs(ctx, batch)
		if err != nil {
			return deleted, fmt.Errorf("misterdocs: load title properties for cleanup: %w", err)
		}
		for _, titleID := range batch {
			if err := waitForScrape(ctx, opts); err != nil {
				return deleted, err
			}
			props := titleProps[titleID]
			for propIdx := range props {
				prop := &props[propIdx]
				if !isStaleDocsProperty(prop, found, cleanupRoots) || prop.TypeTagDBID == 0 {
					continue
				}
				if err := s.db.DeleteMediaTitleProperty(ctx, titleID, prop.TypeTagDBID); err != nil {
					return deleted, fmt.Errorf("misterdocs: delete stale title property: %w", err)
				}
				deleted++
			}
		}
	}
	return deleted, nil
}

func (s *scraperImpl) docsRootForSource(path string) string {
	for _, root := range s.docsRoots {
		if pathWithin(path, root) {
			return root
		}
	}
	return ""
}

func isStaleDocsProperty(
	prop *database.MediaProperty,
	found map[string]struct{},
	cleanupRoots []string,
) bool {
	if prop.Text == "" {
		return false
	}
	isManual := prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyManual)
	imageDirName := ""
	for i := range imageDirs {
		if prop.TypeTag == tags.PropertyTypeTag(imageDirs[i].property) {
			imageDirName = imageDirs[i].name
			break
		}
	}
	if !isManual && imageDirName == "" {
		return false
	}
	path := filepath.Clean(filepath.FromSlash(prop.Text))
	if _, ok := found[path]; ok {
		return false
	}
	withinDocs := false
	for _, root := range cleanupRoots {
		if pathWithin(path, root) {
			withinDocs = true
			break
		}
	}
	if !withinDocs {
		return false
	}
	parent := strings.ToLower(filepath.Base(filepath.Dir(path)))
	if isManual {
		return strings.Contains(parent, "manual")
	}
	return strings.EqualFold(parent, imageDirName)
}
