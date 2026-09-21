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

package misterarcade

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scraperID      = "mister-arcade"
	scraperName    = "MiSTer arcade catalog"
	writeBatchSize = 100
)

// NewPlatformScraper returns the arcade catalog scraper.
//
// systems are the indexed systems whose media this catalog describes: the
// platform states them because MiSTer classifies arcade descriptors into
// granular hardware systems while MiSTeX indexes them all as Arcade. catalog
// reads the platform's cached or embedded copy of the catalog. cache is an
// optional set-name fast path and may be nil.
func NewPlatformScraper(systems []string, catalog Catalog, cache SetNameCache) platforms.Scraper {
	supported := slices.Clone(systems)
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: supported,
		SupportsFillMissing: true,
		// Every arcade launcher contributes: a set the catalog describes is
		// indexed under Arcade and, where MiSTer classifies it, under its
		// hardware system too, and both rows deserve the metadata.
		AutoScrapeLaunchers: supported,
		Scrape: func(
			ctx context.Context,
			_ *config.Instance,
			pl platforms.Platform,
			fs afero.Fs,
			db *database.Database,
			opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("misterarcade: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("misterarcade: media database is required")
			}
			if catalog == nil {
				return errors.New("misterarcade: catalog reader is required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			entries, err := catalog(pl)
			if err != nil {
				return fmt.Errorf("misterarcade: %w", err)
			}
			indexed, err := db.MediaDB.IndexedSystems()
			if err != nil {
				return fmt.Errorf("misterarcade: list indexed systems: %w", err)
			}
			impl := &scraperImpl{fs: fs, db: db.MediaDB, entries: index(entries), cache: cache}
			go impl.scrapeLoop(ctx, opts, targetSystems(supported, indexed, opts.SystemIDs()), ch)
			return nil
		},
	}
}

// targetSystems intersects the systems this catalog describes with the systems
// actually indexed and the systems the request asked for, keeping the
// scraper's own order so progress steps are stable across runs.
func targetSystems(supported, indexed, requested []string) []string {
	has := func(list []string, id string) bool {
		return slices.ContainsFunc(list, func(candidate string) bool {
			return strings.EqualFold(candidate, id)
		})
	}
	targets := make([]string, 0, len(supported))
	for _, id := range supported {
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
	fs      afero.Fs
	db      database.MediaDBI
	entries map[string]*Entry
	cache   SetNameCache
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
	bgpriority.Apply()

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
		Msg("misterarcade: scrape complete")
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: total.Processed, Total: total.Processed,
		Matched: total.Matched, Skipped: total.Skipped,
		TotalSteps: len(targets), CurrentStep: len(targets),
	}
}

func (s *scraperImpl) scrapeSystem(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systemID string,
	step, steps int,
	ch chan<- scraper.ScrapeUpdate,
) (matchStats, error) {
	if err := waitForScrape(ctx, opts); err != nil {
		return matchStats{}, err
	}
	titles, err := s.db.GetTitlesBySystemID(systemID)
	if err != nil {
		return matchStats{}, fmt.Errorf("misterarcade: load titles for %s: %w", systemID, err)
	}
	if len(titles) == 0 {
		return matchStats{}, nil
	}
	media, err := s.db.GetMediaBySystemID(systemID)
	if err != nil {
		return matchStats{}, fmt.Errorf("misterarcade: load media for %s: %w", systemID, err)
	}
	completed, err := s.completedMedia(ctx, opts, titles[0].SystemDBID)
	if err != nil {
		return matchStats{}, err
	}

	// First fill of a shared title is deterministic, independent of query order.
	sort.Slice(media, func(i, j int) bool { return media[i].Path < media[j].Path })
	targets, stats := s.buildTargets(ctx, media, completed, opts)
	for i := range targets {
		targets[i].Write.FillMissing = opts.FillMissing
	}

	report := func(processed, matched int) {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemID, Processed: processed, Total: stats.Processed,
			Matched: matched, Skipped: stats.Skipped,
			TotalSteps: steps, CurrentStep: step + 1,
		}:
		case <-ctx.Done():
		}
	}
	if stats.Processed > 0 {
		report(stats.Skipped, 0)
	}

	written := 0
	if err := s.applyTargets(ctx, opts, targets, func(from, to int) {
		written += to - from
		if to < len(targets) {
			report(stats.Skipped+written, written)
		}
	}); err != nil {
		return matchStats{Processed: stats.Processed, Matched: written, Skipped: stats.Skipped}, err
	}
	report(stats.Processed, written)
	return matchStats{Processed: stats.Processed, Matched: written, Skipped: stats.Skipped}, nil
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
			return nil, fmt.Errorf("misterarcade: load run markers: %w", err)
		}
		return ids, nil
	case !opts.Force && !opts.FillMissing:
		ids, err := s.db.GetScrapedMediaIDs(ctx, scraperID, systemDBID)
		if err != nil {
			return nil, fmt.Errorf("misterarcade: load scraped media: %w", err)
		}
		return ids, nil
	default:
		return map[int64]struct{}{}, nil
	}
}

// buildTargets resolves every indexed descriptor to the write that enriches it.
// A descriptor with no readable set name, or one the catalog does not list,
// counts as skipped: the catalog omits hundreds of sets, most of them
// alternates, and those rows simply have no metadata to import.
func (s *scraperImpl) buildTargets(
	ctx context.Context,
	media []database.MediaWithFullPath,
	completed map[int64]struct{},
	opts scraper.ScrapeOptions,
) ([]database.ScrapeWriteTarget, matchStats) {
	targets := make([]database.ScrapeWriteTarget, 0, len(media))
	stats := matchStats{}
	for i := range media {
		if ctx.Err() != nil {
			return targets, stats
		}
		row := &media[i]
		if row.IsMissing || !strings.EqualFold(filepath.Ext(row.Path), mra.Ext) {
			continue
		}
		if _, already := completed[row.DBID]; already {
			continue
		}
		stats.Processed++
		entry, found := s.entries[s.setName(row.Path)]
		if !found {
			stats.Skipped++
			continue
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID,
			Write: buildWrite(entry, opts.RunID),
		})
		stats.Matched++
	}
	return targets, stats
}

// setName resolves one descriptor's set name, preferring the platform's cache
// so a scrape does not re-read thousands of descriptors from slow storage.
func (s *scraperImpl) setName(path string) string {
	if s.cache != nil {
		if cached, ok := s.cache(path); ok {
			return strings.ToLower(strings.TrimSpace(cached))
		}
	}
	return mra.ReadSetName(s.fs, path)
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
		if err := s.applyBatch(ctx, batcher, canBatch, targets[start:end]); err != nil {
			return err
		}
		if onBatch != nil {
			onBatch(start, end)
		}
	}
	return nil
}

// applyBatch commits one batch. A write failure is fatal: fill-missing work
// that committed only part of a batch must not be reported as complete, or the
// sentinel would keep the remaining rows from ever being filled.
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
		log.Warn().Err(batchErr).Int("targets", len(batch)).
			Msg("misterarcade: batch write failed, falling back to per-record writes")
	}
	for _, target := range batch {
		if err := s.db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("misterarcade: write media %d: %w", target.MediaDBID, err)
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
			return fmt.Errorf("misterarcade: wait while paused: %w", err)
		}
	}
	return nil
}
