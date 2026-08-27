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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
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
			impl := &scraperImpl{
				fs: fs, db: db.MediaDB, docsRoots: candidateDocsRoots(rootDirs),
				sources: sourcesBySystem(sources),
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

		var records []sourceRecords
		var sourceError error
		for _, sourceID := range sourceIDsForTarget(targetID) {
			for _, source := range s.sources[sourceID] {
				if err := waitForScrape(ctx, opts); err != nil {
					ch <- scraper.ScrapeUpdate{
						Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
					}
					return
				}
				loaded, loadErr := loadSourceRecords(ctx, s.fs, source)
				if loadErr != nil {
					sourceError = errors.Join(
						sourceError,
						fmt.Errorf("misterdocs: load %q: %w", source.Path, loadErr),
					)
					continue
				}
				records = append(records, loaded)
			}
		}

		idx := newSystemIndex(titles, media)
		writeTargets, stats, foundPaths := buildPendingWrites(idx, records, opts.RunID)
		if opts.Force && sourceError == nil {
			deleted, cleanupErr := s.deleteStaleProperties(ctx, opts, media, titles, foundPaths)
			if cleanupErr != nil {
				sourceError = cleanupErr
			} else if deleted > 0 {
				stats.Processed += deleted
				stats.Matched += deleted
			}
		}

		stepError := sourceError
		if err := s.applyTargets(ctx, opts, writeTargets); err != nil {
			stepError = errors.Join(stepError, err)
			stats.Skipped++
		}
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

func (s *scraperImpl) applyTargets(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	targets []database.ScrapeWriteTarget,
) error {
	batcher, canBatch := s.db.(database.ScrapeResultBatchApplier)
	for start := 0; start < len(targets); start += writeBatchSize {
		if err := waitForScrape(ctx, opts); err != nil {
			return err
		}
		end := min(start+writeBatchSize, len(targets))
		batch := targets[start:end]
		if canBatch {
			batchErr := batcher.ApplyScrapeResults(ctx, batch)
			if batchErr == nil {
				continue
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
			if !s.isStaleDocsProperty(prop, found) || prop.TypeTagDBID == 0 {
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
			if !s.isStaleDocsProperty(prop, found) || prop.TypeTagDBID == 0 {
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

func (s *scraperImpl) isStaleDocsProperty(prop *database.MediaProperty, found map[string]struct{}) bool {
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
	for _, root := range s.docsRoots {
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
