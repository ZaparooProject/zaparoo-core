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
// reads the platform's cached or embedded copy of the catalog. cache and
// identities are optional fast paths for reading a descriptor and may be nil.
func NewPlatformScraper(
	systems []string, catalog Catalog, cache SetNameCache, identities IdentityCache,
) platforms.Scraper {
	supported := slices.Clone(systems)
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: supported,
		SupportsFillMissing: true,
		AutoScrapeOrder:     scraper.AutoOrderDevice,
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
				fs: fs, db: db.MediaDB, entries: index(entries), cache: cache, identities: identities,
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
	// identities answers a descriptor's parent set where the platform cached it.
	identities IdentityCache
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
	if slugErr := s.fillTitleSlugs(ctx, candidates, opts); slugErr != nil {
		return matchStats{}, slugErr
	}
	built, err := s.buildTargets(ctx, candidates, indexMains(candidates), completed, opts)
	targets, keyTargets, stats := built.matched, built.keysOnly, built.stats
	if err != nil {
		return stats, err
	}
	for i := range targets {
		targets[i].Write.FillMissing = opts.FillMissing
	}
	// Sets the catalog does not list still say which game they are a version
	// of. They are written apart so they do not count as matched.
	if err := scraper.ApplyTargets(ctx, s.db, opts, scraperID, keyTargets, nil); err != nil {
		//nolint:wrapcheck // scraper.ApplyTargets names the scraper in its errors
		return stats, err
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

// scrapeScope scrapes only the media one scoped request selected, such as a
// single game's "Update metadata", rather than its whole system.
func (s *scraperImpl) scrapeScope(
	ctx context.Context, opts scraper.ScrapeOptions, targets []string, ch chan<- scraper.ScrapeUpdate,
) {
	selection, err := scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("misterarcade: %w", err), Done: true}
		return
	}
	if len(targets) == 0 {
		scraper.ApplyScopedTargets(ctx, s.db, opts, selection, nil, ch)
		return
	}
	media, _ := selection.Pending()
	candidates := make([]descriptorRow, 0, len(media))
	for i := range media {
		if strings.EqualFold(filepath.Ext(media[i].Path), mra.Ext) {
			candidates = append(candidates, descriptorRow{
				path: media[i].Path, dbid: media[i].DBID, titleDBID: media[i].MediaTitleDBID,
				titleSlug: media[i].TitleSlug,
			})
		}
	}
	// Whether an alternate is a game with a main descriptor of its own is a
	// fact about its whole system, not about the media in scope.
	var systemRows []descriptorRow
	for _, systemID := range targets {
		rows, loadErr := s.loadDescriptors(ctx, systemID)
		if loadErr == nil {
			loadErr = s.fillTitleSlugs(ctx, rows, opts)
		}
		if loadErr != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: loadErr, Done: true}
			return
		}
		systemRows = append(systemRows, rows...)
	}
	built, err := s.buildTargets(ctx, candidates, indexMains(systemRows), map[int64]struct{}{}, opts)
	writeTargets := built.matched
	if err == nil {
		err = scraper.ApplyTargets(ctx, s.db, opts, scraperID, built.keysOnly, nil)
	}
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
	titleSlug string
	// otherGame marks an alternate that is another game's than its folder's.
	otherGame bool
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
			path: row.Path, dbid: row.DBID, titleDBID: row.MediaTitleDBID, titleSlug: row.TitleSlug,
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

// builtTargets is what a set of descriptors resolved to. matched are the rows
// the catalog describes, each with its metadata write. keysOnly are rows it
// does not, which have no metadata but still say which game they are a version
// of, and carry nothing but those keys.
type builtTargets struct {
	matched  []database.ScrapeWriteTarget
	keysOnly []database.ScrapeWriteTarget
	stats    matchStats
}

// fillTitleSlugs reads the title slug of every main descriptor. The media
// walk leaves it out to stay a single-table read, and a main descriptor needs
// it to answer to the folder its alternates are filed under.
func (s *scraperImpl) fillTitleSlugs(
	ctx context.Context, candidates []descriptorRow, opts scraper.ScrapeOptions,
) error {
	var titleDBIDs []int64
	for i := range candidates {
		row := &candidates[i]
		if row.titleSlug != "" {
			continue
		}
		if _, alternate := alternativesFolder(row.path); !alternate {
			titleDBIDs = append(titleDBIDs, row.titleDBID)
		}
	}
	if len(titleDBIDs) == 0 {
		return nil
	}
	if err := waitForScrape(ctx, opts); err != nil {
		return err
	}
	slices.Sort(titleDBIDs)
	titles, err := s.db.GetTitlesByDBIDs(ctx, slices.Compact(titleDBIDs))
	if err != nil {
		return fmt.Errorf("misterarcade: load title slugs: %w", err)
	}
	slugByTitle := make(map[int64]string, len(titles))
	for i := range titles {
		slugByTitle[titles[i].DBID] = titles[i].Slug
	}
	for i := range candidates {
		if candidates[i].titleSlug == "" {
			candidates[i].titleSlug = slugByTitle[candidates[i].titleDBID]
		}
	}
	return nil
}

// buildTargets resolves every indexed descriptor to the write that enriches it.
// A descriptor with no readable set name, or one the catalog does not list,
// counts as skipped: the catalog omits hundreds of sets, most of them
// alternates, and those rows simply have no metadata to import. Reading a
// descriptor yields to the pauser first, so a launch is never kept waiting on
// the scrape's storage reads.
func (s *scraperImpl) buildTargets(
	ctx context.Context,
	candidates []descriptorRow,
	mains mainIndex,
	completed map[int64]struct{},
	opts scraper.ScrapeOptions,
) (built builtTargets, err error) {
	if err := s.fillTitleSlugs(ctx, candidates, opts); err != nil {
		return built, err
	}
	for i := range candidates {
		candidates[i].otherGame = mains.otherGame(&candidates[i])
	}
	for i := range candidates {
		row := &candidates[i]
		if _, already := completed[row.dbid]; already {
			continue
		}
		if err := waitForScrape(ctx, opts); err != nil {
			return built, err
		}
		built.stats.Processed++
		identity := s.identity(row.path)
		entry, found := s.entries[identity.SetName]
		keys := versionKeys(row, identity)
		if !found {
			built.stats.Skipped++
			if len(keys) > 0 {
				built.keysOnly = append(built.keysOnly, database.ScrapeWriteTarget{
					MediaDBID: row.dbid, MediaTitleDBID: row.titleDBID,
					Write: &database.ScrapeWrite{VersionKeySource: scraperID, VersionKeys: keys},
				})
			}
			continue
		}
		write := buildWrite(entry, opts.RunID, s.unmapped)
		write.VersionKeySource = scraperID
		write.VersionKeys = keys
		built.matched = append(built.matched, database.ScrapeWriteTarget{
			MediaDBID: row.dbid, MediaTitleDBID: row.titleDBID, Write: write,
		})
		built.stats.Matched++
	}
	return built, nil
}

// identity resolves one descriptor's set name and parent, preferring the
// platform's caches so a scrape does not re-read thousands of descriptors from
// slow storage. A set name the cache answers without a parent is used as it
// is: the parent stays unknown until the platform next reads that file.
func (s *scraperImpl) identity(path string) mra.Identity {
	if s.identities != nil {
		if setName, parent, ok := s.identities(path); ok {
			identity := mra.NormalizeIdentity(setName, parent)
			// A cached name the strict reader would refuse still keys the
			// catalog as it always has; it just names no parent.
			identity.SetName = strings.ToLower(strings.TrimSpace(setName))
			return identity
		}
	}
	if s.cache != nil {
		if cached, ok := s.cache(path); ok {
			return mra.Identity{SetName: strings.ToLower(strings.TrimSpace(cached))}
		}
	}
	return mra.ReadIdentity(s.fs, path)
}

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	//nolint:wrapcheck // scraper.Wait names the scraper in its error
	return scraper.Wait(ctx, opts, scraperID)
}
