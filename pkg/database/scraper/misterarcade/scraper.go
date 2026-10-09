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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
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
	scraperID   = "mister-arcade"
	scraperName = "MiSTer arcade catalog"
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
			impl := &scraperImpl{
				fs: fs, db: db.MediaDB, entries: index(entries), cache: cache,
				unmapped: &scraper.UnmappedValues{}, catalogState: catalogState(entries),
			}
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
	// unmapped collects the catalog values this run dropped for want of a
	// tag mapping; scrapeLoop logs the summary once the run ends.
	unmapped *scraper.UnmappedValues
	// catalogState identifies the catalog this run reads, for the fingerprint
	// that lets an index-triggered run leave an unchanged system alone.
	catalogState string
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

// fingerprintVersion is bumped when a change to matching or to what is written
// means an unchanged system has to be scraped again.
const fingerprintVersion = 1

// catalogState digests the catalog, the whole of this scraper's source: a
// refreshed catalog is the only thing outside the library that can give a
// descriptor new metadata.
func catalogState(entries []Entry) string {
	digest := sha256.New()
	for i := range entries {
		_, _ = fmt.Fprintf(digest, "%+v\n", entries[i])
	}
	return hex.EncodeToString(digest.Sum(nil))
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
	// An index-triggered run ends here when the catalog and the system's rows
	// are as its last completed run left them; every run over whole systems
	// records the state it finishes in.
	if opts.Scope == nil {
		if opts.FillMissing &&
			scraper.SystemUnchanged(ctx, s.db, scraperID, systemID, fingerprintVersion, s.catalogState) {
			log.Debug().Str("system", systemID).Msg("misterarcade: catalog and library unchanged, system skipped")
			ch <- scraper.ScrapeUpdate{SystemID: systemID, TotalSteps: steps, CurrentStep: step + 1}
			return matchStats{}, nil
		}
		defer func() {
			if err != nil {
				return
			}
			if rememberErr := scraper.RememberSystem(
				ctx, s.db, scraperID, systemID, fingerprintVersion, s.catalogState,
			); rememberErr != nil {
				log.Debug().Err(rememberErr).Msg("misterarcade: system fingerprint not stored")
			}
		}()
	}
	candidates, err := s.loadDescriptors(ctx, systemID)
	if len(candidates) > 0 {
		// Hand the system's working set back to the OS before the next one,
		// so a run's peak is one system rather than the sum of them.
		defer debug.FreeOSMemory()
	}
	if err != nil {
		return matchStats{}, err
	}
	if len(candidates) == 0 {
		return matchStats{}, nil
	}
	system, err := s.db.FindSystemBySystemID(systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return matchStats{}, nil
	}
	if err != nil {
		return matchStats{}, fmt.Errorf("misterarcade: look up system %s: %w", systemID, err)
	}
	completed, err := s.completedMedia(ctx, opts, system.DBID)
	if err != nil {
		return matchStats{}, err
	}

	// First fill of a shared title is deterministic, independent of query order.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })
	targets, stats := s.buildTargets(ctx, candidates, completed, opts)
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

// descriptorRow is the part of an indexed descriptor row the scrape reads.
type descriptorRow struct {
	path      string
	dbid      int64
	titleDBID int64
}

// loadDescriptors streams a system's media and keeps only the present .mra
// rows, the only ones the catalog can describe. The stream holds a read open,
// so nothing here touches the descriptors themselves.
func (s *scraperImpl) loadDescriptors(ctx context.Context, systemID string) ([]descriptorRow, error) {
	var candidates []descriptorRow
	err := s.db.ForEachMediaBySystemID(ctx, systemID, func(row *database.MediaWithFullPath) error {
		if row.IsMissing || !strings.EqualFold(filepath.Ext(row.Path), mra.Ext) {
			return nil
		}
		candidates = append(candidates, descriptorRow{
			path: row.Path, dbid: row.DBID, titleDBID: row.MediaTitleDBID,
		})
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return candidates, ctxErr
		}
		return candidates, fmt.Errorf("misterarcade: load media for %s: %w", systemID, err)
	}
	return candidates, nil
}

// buildTargets resolves every indexed descriptor to the write that enriches it.
// A descriptor with no readable set name, or one the catalog does not list,
// counts as skipped: the catalog omits hundreds of sets, most of them
// alternates, and those rows simply have no metadata to import.
func (s *scraperImpl) buildTargets(
	ctx context.Context,
	candidates []descriptorRow,
	completed map[int64]struct{},
	opts scraper.ScrapeOptions,
) ([]database.ScrapeWriteTarget, matchStats) {
	var targets []database.ScrapeWriteTarget
	stats := matchStats{}
	for i := range candidates {
		if ctx.Err() != nil {
			return targets, stats
		}
		row := &candidates[i]
		if _, already := completed[row.dbid]; already {
			continue
		}
		stats.Processed++
		entry, found := s.entries[s.setName(row.path)]
		if !found {
			stats.Skipped++
			continue
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: row.dbid, MediaTitleDBID: row.titleDBID,
			Write: buildWrite(entry, opts.RunID, s.unmapped),
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

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	//nolint:wrapcheck // scraper.Wait names the scraper in its error
	return scraper.Wait(ctx, opts, scraperID)
}
