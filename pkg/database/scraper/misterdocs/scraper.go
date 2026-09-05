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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scraperID      = "mister-docs"
	scraperName    = "MiSTer docs databases"
	writeBatchSize = 100
)

// NewPlatformScraper returns the MiSTer installed-docs scraper.
func NewPlatformScraper() platforms.Scraper {
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: []string{},
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
			targets := orderedTargetSystems(indexed, opts.Systems)
			var langs []string
			if cfg != nil {
				langs = cfg.DefaultLangs()
			}
			impl := &scraperImpl{
				fs: fs, db: db.MediaDB, docsRoots: candidateDocsRoots(rootDirs),
				sources: sourcesBySystem(sources), langs: langs,
			}
			go impl.scrapeLoop(ctx, opts, targets, ch)
			return nil
		},
	}
}

type scraperImpl struct {
	sources   map[string][]sourceDir
	fs        afero.Fs
	db        database.MediaDBI
	docsRoots []string
	langs     []string
}

func (s *scraperImpl) scrapeLoop(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	targetSystems []string,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	bgpriority.Apply()

	steps := s.eligibleTargets(targetSystems, opts.Force)
	totalProcessed, totalMatched, totalSkipped := 0, 0, 0
	for step, targetID := range steps {
		if err := waitForScrape(ctx, opts); err != nil {
			ch <- scraper.ScrapeUpdate{
				Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
			}
			return
		}
		titles, err := s.db.GetTitlesBySystemID(targetID)
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: fmt.Errorf("misterdocs: load titles for %s: %w", targetID, err), Done: true,
			}
			return
		}
		media, err := s.db.GetMediaBySystemID(targetID)
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: fmt.Errorf("misterdocs: load media for %s: %w", targetID, err), Done: true,
			}
			return
		}

		stepStart := time.Now()
		report := func(processed, total, matched, skipped int) {
			select {
			case ch <- scraper.ScrapeUpdate{
				SystemID: targetID, Processed: processed, Total: total, Matched: matched, Skipped: skipped,
				TotalSteps: len(steps), CurrentStep: step + 1,
			}:
			case <-ctx.Done():
			}
		}

		var records []sourceRecords
		var sourceError error
		arcadeSource := false
		successfulRoots := make(map[string]struct{})
		for _, sourceID := range sourceIDsForTarget(targetID) {
			for _, source := range s.sources[sourceID] {
				if err := waitForScrape(ctx, opts); err != nil {
					ch <- scraper.ScrapeUpdate{
						Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
					}
					return
				}
				loaded, loadErr := loadSourceRecords(ctx, s.fs, source, s.langs)
				if loadErr != nil {
					sourceError = errors.Join(
						sourceError,
						fmt.Errorf("misterdocs: load %q: %w", source.Path, loadErr),
					)
					continue
				}
				records = append(records, loaded)
				if source.Kind == sourceArtwork && sourceID == systemdefs.SystemArcade {
					arcadeSource = true
				}
				if root := s.docsRootForSource(source.Path); root != "" {
					successfulRoots[root] = struct{}{}
				}
			}
		}

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

		cleanupRoots := make([]string, 0, len(successfulRoots))
		for _, root := range s.docsRoots {
			if _, ok := successfulRoots[root]; ok {
				cleanupRoots = append(cleanupRoots, root)
			}
		}
		scanStart := time.Now()
		idx := newSystemIndex(titles, media)
		if arcadeSource {
			if err := s.indexArcadeSetNames(ctx, opts, &idx, media); err != nil {
				ch <- scraper.ScrapeUpdate{
					Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				}
				return
			}
		}
		scanDuration := time.Since(scanStart)
		matchStart := time.Now()
		matched := buildPendingWrites(idx, records, opts.RunID)
		writeTargets, stats := matched.Targets, matched.Stats
		matchDuration := time.Since(matchStart)
		cleanupStart := time.Now()
		if opts.Force && sourceError == nil && len(cleanupRoots) > 0 {
			if _, cleanupErr := s.deleteStaleProperties(
				ctx, opts, media, titles, matched.Found, cleanupRoots,
			); cleanupErr != nil {
				sourceError = cleanupErr
			}
		}
		cleanupDuration := time.Since(cleanupStart)

		// Skipped records are finished once matching is; matched ones finish
		// as their rows commit, so progress advances with each write batch.
		if len(writeTargets) > 0 && stats.Skipped > 0 {
			report(stats.Skipped, stats.Processed, 0, stats.Skipped)
		}
		writeStart := time.Now()
		matchedWritten := 0
		stepError := sourceError
		if err := s.applyTargets(ctx, opts, writeTargets, func(from, to int) {
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
		totalProcessed += stats.Processed
		totalMatched += stats.Matched
		totalSkipped += stats.Skipped
		ch <- scraper.ScrapeUpdate{
			Err: stepError, SystemID: targetID, Processed: stats.Processed, Total: stats.Processed,
			Matched: stats.Matched, Skipped: stats.Skipped, TotalSteps: len(steps), CurrentStep: step + 1,
		}
	}
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
		TotalSteps: len(steps), CurrentStep: len(steps),
	}
}

// indexArcadeSetNames resolves installed MRAs to the setname they declare.
// Arcade artwork is filed under the MAME parent setname, which lives inside the
// MRA and never in its filename, so without this pass an arcade pack matches
// nothing. It runs only for systems that actually have an arcade source.
func (s *scraperImpl) indexArcadeSetNames(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	idx *systemIndex,
	media []database.MediaWithFullPath,
) error {
	started := time.Now()
	scanned, resolved := 0, 0
	for i := range media {
		if !strings.EqualFold(filepath.Ext(media[i].Path), mraExt) {
			continue
		}
		if err := waitForScrape(ctx, opts); err != nil {
			return err
		}
		scanned++
		setName, ok := readMRASetName(s.fs, media[i].Path)
		if !ok {
			continue
		}
		resolved++
		key := strings.ToLower(setName)
		idx.mediaBySetName[key] = append(idx.mediaBySetName[key], media[i])
	}
	if scanned > 0 {
		log.Debug().
			Int("mraFiles", scanned).
			Int("resolved", resolved).
			Dur("elapsed", time.Since(started)).
			Msg("misterdocs: resolved arcade setnames")
	}
	return nil
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

// applyTargets writes targets in batches, calling onBatch with the half-open
// index range of each batch once it has committed.
func (s *scraperImpl) applyTargets(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	targets []database.ScrapeWriteTarget,
	onBatch func(from, to int),
) error {
	batcher, canBatch := s.db.(database.ScrapeResultBatchApplier)
	for start := 0; start < len(targets); start += writeBatchSize {
		if err := waitForScrape(ctx, opts); err != nil {
			return err
		}
		end := min(start+writeBatchSize, len(targets))
		batch := targets[start:end]
		if err := s.applyBatch(ctx, batcher, canBatch, batch); err != nil {
			return err
		}
		if onBatch != nil {
			onBatch(start, end)
		}
	}
	return nil
}

func (s *scraperImpl) applyBatch(
	ctx context.Context,
	batcher database.ScrapeResultBatchApplier,
	canBatch bool,
	batch []database.ScrapeWriteTarget,
) error {
	if canBatch {
		batchErr := batcher.ApplyScrapeResults(ctx, batch)
		if batchErr == nil {
			return nil
		}
		log.Warn().Err(batchErr).
			Int("targets", len(batch)).
			Msg("misterdocs: batch write failed, falling back to per-record writes")
	}
	for _, target := range batch {
		if err := s.db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("misterdocs: write media %d: %w", target.MediaDBID, err)
		}
	}
	return nil
}

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.Pauser != nil {
		if err := opts.Pauser.Wait(ctx); err != nil {
			return fmt.Errorf("misterdocs: wait while paused: %w", err)
		}
	}
	return nil
}

func (s *scraperImpl) deleteStaleProperties(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	media []database.MediaWithFullPath,
	titles []database.TitleWithSystem,
	found map[string]struct{},
	cleanupRoots []string,
) (int, error) {
	mediaProps := make(map[int64][]database.MediaProperty, len(media))
	if len(media) > 0 {
		if err := waitForScrape(ctx, opts); err != nil {
			return 0, err
		}
		mediaIDs := make([]int64, len(media))
		for i := range media {
			mediaIDs[i] = media[i].DBID
		}
		var err error
		mediaProps, err = s.db.GetMediaPropertyMetadataByMediaDBIDs(ctx, mediaIDs)
		if err != nil {
			return 0, fmt.Errorf("misterdocs: load media properties for cleanup: %w", err)
		}
	}

	titleProps := make(map[int64][]database.MediaProperty, len(titles))
	if len(titles) > 0 {
		if err := waitForScrape(ctx, opts); err != nil {
			return 0, err
		}
		titleIDs := make([]int64, len(titles))
		for i := range titles {
			titleIDs[i] = titles[i].DBID
		}
		var err error
		titleProps, err = s.db.GetMediaTitlePropertyMetadataByMediaTitleDBIDs(ctx, titleIDs)
		if err != nil {
			return 0, fmt.Errorf("misterdocs: load title properties for cleanup: %w", err)
		}
	}

	deleted := 0
	for i := range media {
		if err := waitForScrape(ctx, opts); err != nil {
			return deleted, err
		}
		props := mediaProps[media[i].DBID]
		for propIdx := range props {
			prop := &props[propIdx]
			if !isStaleDocsProperty(prop, found, cleanupRoots) || prop.TypeTagDBID == 0 {
				continue
			}
			if err := s.db.DeleteMediaProperty(ctx, media[i].DBID, prop.TypeTagDBID); err != nil {
				return deleted, fmt.Errorf("misterdocs: delete stale media property: %w", err)
			}
			deleted++
		}
	}
	for i := range titles {
		if err := waitForScrape(ctx, opts); err != nil {
			return deleted, err
		}
		props := titleProps[titles[i].DBID]
		for propIdx := range props {
			prop := &props[propIdx]
			if !isStaleDocsProperty(prop, found, cleanupRoots) || prop.TypeTagDBID == 0 {
				continue
			}
			if err := s.db.DeleteMediaTitleProperty(ctx, titles[i].DBID, prop.TypeTagDBID); err != nil {
				return deleted, fmt.Errorf("misterdocs: delete stale title property: %w", err)
			}
			deleted++
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
	if prop.TypeTag != tags.PropertyTypeTag(tags.TagPropertyImageBoxart) &&
		prop.TypeTag != tags.PropertyTypeTag(tags.TagPropertyManual) {
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
	if prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyImageBoxart) {
		return strings.EqualFold(parent, artworkDirName)
	}
	return strings.Contains(parent, "manual")
}
