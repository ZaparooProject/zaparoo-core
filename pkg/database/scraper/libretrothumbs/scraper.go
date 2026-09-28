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

// Package libretrothumbs downloads box art, screenshots and title screens
// from the libretro thumbnail server, the source RetroArch uses. Thumbnails
// are named after libretro database entries, so a file is matched by its own
// name first and by its indexed title otherwise. Images are stored under
// Core's data directory and recorded as image properties like local artwork.
package libretrothumbs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scummvmnames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scraperID = "libretro-thumbnails"
	// dirName is the folder under Core's data directory holding downloads.
	dirName = "libretro-thumbnails"
	// workers bounds concurrent requests to the thumbnail server.
	workers = 4
	// batchSize is how many matched rows are written in one transaction.
	batchSize = 32
)

// batchInterval bounds how long a matched row waits for its batch, so a slow
// run still commits its progress regularly. A variable so tests can shorten it.
var batchInterval = time.Second

// kindProperty is the image property each thumbnail folder fills.
var kindProperty = map[string]tags.TagValue{
	"Named_Boxarts": tags.TagPropertyImageBoxart,
	"Named_Snaps":   tags.TagPropertyImageScreenshot,
	"Named_Titles":  tags.TagPropertyImageTitleshot,
}

// NewPlatformScraper returns the libretro thumbnail scraper. It downloads, so
// it only runs when asked and is never an automatic post-index scraper.
func NewPlatformScraper() platforms.Scraper {
	return newScraper(DefaultBaseURL, func(pl platforms.Platform) string {
		return filepath.Join(helpers.DataDir(pl), dirName)
	})
}

func newScraper(baseURL string, dir func(platforms.Platform) string) platforms.Scraper {
	return platforms.Scraper{
		ID:                  scraperID,
		Name:                "libretro thumbnails",
		SupportedSystemIDs:  SupportedSystems(),
		SupportsFillMissing: true,
		Scrape: func(
			ctx context.Context, _ *config.Instance, pl platforms.Platform, fs afero.Fs,
			db *database.Database, opts scraper.ScrapeOptions, _ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("libretro-thumbnails: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("libretro-thumbnails: media database is required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			r := &run{db: db.MediaDB, client: newClient(fs, dir(pl), baseURL), opts: opts}
			go r.loop(ctx, ch)
			return nil
		},
	}
}

type run struct {
	db     database.MediaDBI
	client *client
	opts   scraper.ScrapeOptions
}

func (r *run) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("libretro-thumbnails: %w", err)
	}
	if r.opts.Pauser != nil {
		if err := r.opts.Pauser.Wait(ctx); err != nil {
			return fmt.Errorf("libretro-thumbnails: wait while paused: %w", err)
		}
	}
	return nil
}

// systems are the requested (or all indexed) systems libretro has thumbnails for.
func (r *run) systems() ([]string, error) {
	if r.opts.Scope != nil {
		if _, ok := playlists[r.opts.Scope.SystemID]; ok {
			return []string{r.opts.Scope.SystemID}, nil
		}
		return nil, nil
	}
	indexed, err := r.db.IndexedSystems()
	if err != nil {
		return nil, fmt.Errorf("list indexed systems: %w", err)
	}
	var ids []string
	for _, id := range indexed {
		if _, ok := playlists[id]; !ok {
			continue
		}
		if len(r.opts.Systems) > 0 && !slices.Contains(r.opts.Systems, id) {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// load returns a system's rows still to scrape, with their title slugs.
func (r *run) load(ctx context.Context, systemID string) ([]database.MediaWithFullPath, error) {
	if r.opts.Scope != nil {
		selection, err := scraper.LoadScopedSelection(ctx, r.db, r.opts, scraperID)
		if err != nil {
			return nil, fmt.Errorf("load scoped selection: %w", err)
		}
		rows, titles := selection.Pending()
		slugByTitle := make(map[int64]string, len(titles))
		for _, title := range titles {
			slugByTitle[title.DBID] = title.Slug
		}
		for i := range rows {
			rows[i].TitleSlug = slugByTitle[rows[i].MediaTitleDBID]
		}
		return rows, nil
	}
	rows, err := r.db.GetMediaBySystemID(systemID)
	if err != nil {
		return nil, fmt.Errorf("load media for %s: %w", systemID, err)
	}
	var completed map[int64]struct{}
	if r.opts.RunID != "" && (r.opts.Force || r.opts.FillMissing) || !r.opts.Force && !r.opts.FillMissing {
		system, err := r.db.FindSystemBySystemID(systemID)
		if err != nil {
			return nil, fmt.Errorf("look up system %s: %w", systemID, err)
		}
		if r.opts.Force || r.opts.FillMissing {
			completed, err = r.db.GetScrapeRunMediaIDs(ctx, scraperID, r.opts.RunID, system.DBID)
		} else {
			completed, err = r.db.GetScrapedMediaIDs(ctx, scraperID, system.DBID)
		}
		if err != nil {
			return nil, fmt.Errorf("load scrape markers for %s: %w", systemID, err)
		}
	}
	pending := rows[:0]
	for _, row := range rows {
		if _, done := completed[row.DBID]; !done && !row.IsMissing {
			pending = append(pending, row)
		}
	}
	return pending, nil
}

type result struct {
	err   error
	props []database.MediaProperty
	row   database.MediaWithFullPath
}

// thumbnails downloads every kind of thumbnail for one row. A missing image is
// not an error; a failed request is, and leaves the row for a later run.
func (r *run) thumbnails(
	ctx context.Context, systemID string, indexes map[string]*index, row *database.MediaWithFullPath,
) result {
	out := result{row: *row}
	names := []string{mediaBaseName(row.Path)}
	// libretro names arcade thumbnails after the game, not the set archive.
	if arcadenames.IsArcadeSystem(systemID) && arcadenames.SetArchive(path.Ext(row.Path)) {
		if entry, ok := arcadenames.Lookup(names[0]); ok {
			names = []string{entry.Title, names[0]}
		}
	}
	// ScummVM thumbnails are named after ScummVM's title, not the game ID.
	if scummvmnames.IsTargetFile(systemID, path.Ext(row.Path)) {
		id := strings.TrimSuffix(names[0], path.Ext(names[0]))
		if title, ok := scummvmnames.Title(id, mediaFolderName(row.Path)); ok {
			names = []string{title, names[0]}
		}
	}
	for _, playlist := range playlists[systemID] {
		ix := indexes[playlist]
		if ix == nil {
			continue
		}
		name, ok := "", false
		for _, candidate := range names {
			if name, ok = ix.match(candidate, row.TitleSlug); ok {
				break
			}
		}
		if !ok {
			continue
		}
		for _, kind := range kinds {
			imagePath, err := r.client.image(ctx, playlist, kind, name, r.opts.Force)
			if errors.Is(err, errNotFound) {
				continue
			}
			if err != nil {
				out.err = err
				return out
			}
			out.props = append(out.props, database.MediaProperty{
				TypeTag: tags.PropertyTypeTag(kindProperty[kind]), Text: imagePath,
			})
		}
		return out
	}
	return out
}

func (r *run) loop(ctx context.Context, ch chan<- scraper.ScrapeUpdate) {
	defer close(ch)
	bgpriority.Apply()
	systems, err := r.systems()
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("libretro-thumbnails: %w", err), Done: true}
		return
	}
	var all counts
	for step, systemID := range systems {
		if err := r.wait(ctx); err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}
		got, fatal := r.scrapeSystem(ctx, systemID, step+1, len(systems), ch)
		all.processed += got.processed
		all.matched += got.matched
		all.skipped += got.skipped
		if fatal != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: fatal, Done: true}
			return
		}
	}
	ch <- scraper.ScrapeUpdate{
		Processed: all.processed, Matched: all.matched, Skipped: all.skipped,
		TotalSteps: len(systems), CurrentStep: len(systems), Done: true,
	}
}

type counts struct {
	processed, matched, skipped int
}

// scrapeSystem answers its counts and a fatal error; a system whose playlists
// cannot be read is reported and skipped so the rest of the run continues.
func (r *run) scrapeSystem(
	ctx context.Context, systemID string, step, steps int, ch chan<- scraper.ScrapeUpdate,
) (counts, error) {
	var c counts
	rows, err := r.load(ctx, systemID)
	if err != nil {
		return c, fmt.Errorf("libretro-thumbnails: %w", err)
	}
	total := len(rows)
	progress := func(err error) {
		ch <- scraper.ScrapeUpdate{
			Err: err, SystemID: systemID, Processed: c.processed, Total: total,
			Matched: c.matched, Skipped: c.skipped, TotalSteps: steps, CurrentStep: step,
		}
	}
	progress(nil)
	if total == 0 {
		return c, nil
	}
	indexes := make(map[string]*index)
	var indexErr error
	for _, playlist := range playlists[systemID] {
		ix, err := r.client.index(ctx, playlist)
		if err != nil {
			indexErr = err
			continue
		}
		indexes[playlist] = ix
	}
	if len(indexes) == 0 {
		c.processed, c.skipped = total, total
		progress(fmt.Errorf("libretro-thumbnails: %s: %w", systemID, indexErr))
		return c, nil
	}

	jobs := make(chan *database.MediaWithFullPath)
	results := make(chan result)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				select {
				case results <- r.thumbnails(workCtx, systemID, indexes, row):
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	batch := &writeBatch{db: r.db}
	var feedErr error
	go func() {
		defer close(jobs)
		for i := range rows {
			if err := r.wait(workCtx); err != nil {
				feedErr = err
				return
			}
			select {
			case jobs <- &rows[i]:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	fail := func(err error) (counts, error) {
		cancel()
		for range results { //nolint:revive // drain so the workers exit
		}
		return c, fmt.Errorf("libretro-thumbnails: %w", err)
	}
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()
loop:
	for {
		var res result
		select {
		case got, ok := <-results:
			if !ok {
				break loop
			}
			res = got
		case <-ticker.C:
			// A slow run still commits what it has matched.
			if batch.due() {
				if err := batch.flush(ctx); err != nil {
					return fail(err)
				}
			}
			continue
		}
		c.processed++
		if res.err != nil {
			c.skipped++
			log.Debug().Err(res.err).Str("path", res.row.Path).Msg("libretro thumbnail download failed")
			progress(&scraper.SourceError{Path: res.row.Path, Err: res.err})
			continue
		}
		if len(res.props) == 0 {
			c.skipped++
			progress(nil)
			continue
		}
		write := &database.ScrapeWrite{
			Sentinel: scraper.SentinelTagInfo(scraperID), FillMissing: r.opts.FillMissing,
			MediaProps: res.props,
		}
		if r.opts.RunID != "" {
			write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(scraperID, r.opts.RunID))
		}
		if err := batch.add(ctx, database.ScrapeWriteTarget{
			MediaDBID: res.row.DBID, MediaTitleDBID: res.row.MediaTitleDBID, Write: write,
		}); err != nil {
			return fail(err)
		}
		c.matched++
		progress(nil)
	}
	// Downloads that finished are kept even when the run was cancelled: the
	// final write does not inherit the cancellation.
	if err := batch.flush(context.WithoutCancel(ctx)); err != nil {
		return c, fmt.Errorf("libretro-thumbnails: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return c, fmt.Errorf("libretro-thumbnails: %w", err)
	}
	return c, feedErr
}

// writeBatch collects matched rows and writes them together, so one
// transaction and one title disambiguation pass cover many rows. It flushes
// at batchSize rows or once its oldest row has waited batchInterval.
type writeBatch struct {
	started time.Time
	db      database.MediaDBI
	now     func() time.Time
	pending []database.ScrapeWriteTarget
}

func (b *writeBatch) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *writeBatch) add(ctx context.Context, target database.ScrapeWriteTarget) error {
	if len(b.pending) == 0 {
		b.started = b.clock()
	}
	b.pending = append(b.pending, target)
	if b.due() {
		return b.flush(ctx)
	}
	return nil
}

func (b *writeBatch) due() bool {
	return len(b.pending) >= batchSize ||
		(len(b.pending) > 0 && b.clock().Sub(b.started) >= batchInterval)
}

func (b *writeBatch) flush(ctx context.Context) error {
	if len(b.pending) == 0 {
		return nil
	}
	targets := b.pending
	b.pending = nil
	if batcher, ok := b.db.(database.ScrapeResultBatchApplier); ok {
		batchErr := batcher.ApplyScrapeResults(ctx, targets)
		if batchErr == nil {
			return nil
		}
		log.Warn().Err(batchErr).Int("targets", len(targets)).
			Msg("libretro-thumbnails: batch write failed, falling back to per-record writes")
	}
	for _, target := range targets {
		if err := b.db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("write media %d: %w", target.MediaDBID, err)
		}
	}
	return nil
}
