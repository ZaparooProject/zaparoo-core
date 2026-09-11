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

// Package pinuppopper imports table metadata and artwork from a PinUP Popper
// install into the media database. Popper keeps both in its own library: the
// year, manufacturer, player count, categories and notes live in
// PUPDatabase.db, and the wheel, playfield, backglass and flyer images sit
// under each emulator's media folder named after the table file. Media rows
// indexed by the Popper launcher carry the table's GameID in their path, so
// no name matching is needed.
package pinuppopper

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/pinup"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scraperID      = "pinup-popper"
	scraperName    = "PinUP Popper library"
	writeBatchSize = 100
)

// Locate resolves the PinUP System install the scraper reads.
type Locate func(cfg *config.Instance) (pinup.Install, error)

// imageScreen pairs a Popper media folder with the image property it fills.
type imageScreen struct {
	folder   string
	property tags.TagValue
}

// imageScreens are the Popper screens imported as images, in property order.
// The backglass is the pinball counterpart of an arcade marquee, and the
// playfield image is the closest thing a table has to a screenshot; the
// GameInfo flyer is kept as a generic image rather than mislabelled as box art.
var imageScreens = []imageScreen{ //nolint:gochecknoglobals // Static mapping table.
	{folder: "Wheel", property: tags.TagPropertyImageWheel},
	{folder: "PlayField", property: tags.TagPropertyImageScreenshot},
	{folder: "BackGlass", property: tags.TagPropertyImageMarquee},
	{folder: "GameInfo", property: tags.TagPropertyImageImage},
}

// imageExtensions are tried in order for each screen; Popper media managers
// write PNG by default and accept JPEG.
var imageExtensions = []string{".png", ".jpg", ".jpeg"} //nolint:gochecknoglobals // Static lookup order.

// NewPlatformScraper returns the PinUP Popper scraper. locate supplies the
// install so the Windows platform can share the launcher's registry-aware
// lookup and tests can point at a fixture.
func NewPlatformScraper(locate Locate) platforms.Scraper {
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: []string{systemdefs.SystemPinball},
		SupportsFillMissing: true,
		AutoScrapeLaunchers: []string{pinup.LauncherID},
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
				return errors.New("pinuppopper: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("pinuppopper: media database is required")
			}
			if locate == nil {
				return errors.New("pinuppopper: install locator is required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			inst, err := locate(cfg)
			if err != nil {
				return fmt.Errorf("pinuppopper: %w", err)
			}
			lib, err := pinup.ReadLibrary(ctx, inst.DBPath)
			if err != nil {
				return fmt.Errorf("pinuppopper: %w", err)
			}
			impl := &scraperImpl{fs: fs, db: db.MediaDB, lib: lib, install: inst}
			go impl.scrapeLoop(ctx, opts, ch)
			return nil
		},
	}
}

type scraperImpl struct {
	fs      afero.Fs
	db      database.MediaDBI
	install pinup.Install
	lib     pinup.Library
}

type matchStats struct {
	Processed int
	Matched   int
	Skipped   int
}

func (s *scraperImpl) scrapeLoop(ctx context.Context, opts scraper.ScrapeOptions, ch chan<- scraper.ScrapeUpdate) {
	defer close(ch)
	bgpriority.Apply()

	done := func(processed, matched, skipped int) {
		ch <- scraper.ScrapeUpdate{
			Done: true, Processed: processed, Total: processed, Matched: matched, Skipped: skipped,
			TotalSteps: 1, CurrentStep: 1,
		}
	}
	if !wantsSystem(opts.Systems, systemdefs.SystemPinball) {
		done(0, 0, 0)
		return
	}
	if err := waitForScrape(ctx, opts); err != nil {
		done(0, 0, 0)
		return
	}

	started := time.Now()
	titles, err := s.db.GetTitlesBySystemID(systemdefs.SystemPinball)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("pinuppopper: load titles: %w", err), Done: true}
		return
	}
	media, err := s.db.GetMediaBySystemID(systemdefs.SystemPinball)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("pinuppopper: load media: %w", err), Done: true}
		return
	}
	scraped := map[int64]struct{}{}
	if len(titles) > 0 {
		switch {
		case opts.RunID != "" && (opts.Force || opts.FillMissing):
			scraped, err = s.db.GetScrapeRunMediaIDs(ctx, scraperID, opts.RunID, titles[0].SystemDBID)
		case !opts.Force && !opts.FillMissing:
			scraped, err = s.db.GetScrapedMediaIDs(ctx, scraperID, titles[0].SystemDBID)
		}
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("pinuppopper: load scraped media: %w", err), Done: true}
			return
		}
	}

	// First fill of a shared title is deterministic, independent of query order.
	slices.SortFunc(media, func(a, b database.MediaWithFullPath) int { return strings.Compare(a.Path, b.Path) })
	targets, stats := s.buildTargets(media, scraped, opts.RunID)
	for i := range targets {
		targets[i].Write.FillMissing = opts.FillMissing
	}
	report := func(processed, matched, skipped int) {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemdefs.SystemPinball, Processed: processed, Total: stats.Processed,
			Matched: matched, Skipped: skipped, TotalSteps: 1, CurrentStep: 1,
		}:
		case <-ctx.Done():
		}
	}
	if stats.Processed > 0 {
		report(stats.Skipped, 0, stats.Skipped)
	}

	written := 0
	stepErr := s.applyTargets(ctx, opts, targets, func(from, to int) {
		written += to - from
		if to < len(targets) {
			report(stats.Skipped+written, written, stats.Skipped)
		}
	})
	if stepErr != nil {
		ch <- scraper.ScrapeUpdate{
			FatalErr: stepErr, Done: true, SystemID: systemdefs.SystemPinball,
			Processed: stats.Skipped + written, Total: stats.Processed,
			Matched: written, Skipped: stats.Skipped,
		}
		return
	}
	log.Debug().
		Int("tables", stats.Processed).
		Int("matched", written).
		Int("skipped", stats.Skipped).
		Dur("total", time.Since(started)).
		Msg("pinuppopper: scrape complete")
	ch <- scraper.ScrapeUpdate{
		Err: stepErr, SystemID: systemdefs.SystemPinball, Processed: stats.Processed, Total: stats.Processed,
		Matched: written, Skipped: stats.Skipped, TotalSteps: 1, CurrentStep: 1,
	}
	done(stats.Processed, written, stats.Skipped)
}

// buildTargets resolves every Popper-launched media row to the write that
// enriches it. Rows already carrying the sentinel are left out entirely; rows
// whose table has gone from Popper are counted as skipped.
func (s *scraperImpl) buildTargets(
	media []database.MediaWithFullPath, scraped map[int64]struct{}, runID string,
) ([]database.ScrapeWriteTarget, matchStats) {
	targets := make([]database.ScrapeWriteTarget, 0, len(media))
	stats := matchStats{}
	prefix := shared.SchemePopper + "://"
	for i := range media {
		row := &media[i]
		if row.IsMissing || !strings.HasPrefix(strings.ToLower(row.Path), prefix) {
			continue
		}
		if _, already := scraped[row.DBID]; already {
			continue
		}
		stats.Processed++
		gameID, err := pinup.ParseTablePath(row.Path)
		if err != nil {
			log.Debug().Err(err).Str("path", row.Path).Msg("pinuppopper: skipping unparseable media path")
			stats.Skipped++
			continue
		}
		table, emu, ok := s.lib.Table(gameID)
		if !ok {
			log.Debug().Int("gameID", gameID).Msg("pinuppopper: table no longer in Popper library")
			stats.Skipped++
			continue
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID,
			Write: s.buildWrite(&table, &emu, runID),
		})
		stats.Matched++
	}
	return targets, stats
}

// buildWrite maps one Popper table to title tags, a description and the
// image properties whose files exist.
func (s *scraperImpl) buildWrite(table *pinup.Table, emu *pinup.Emulator, runID string) *database.ScrapeWrite {
	write := &database.ScrapeWrite{Sentinel: scraper.SentinelTagInfo(scraperID)}
	if runID != "" {
		write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(scraperID, runID))
	}

	seen := make(map[string]struct{})
	addTitleTag := func(tag database.TagInfo) {
		key := tag.Type + ":" + tag.Tag
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		write.TitleTags = append(write.TitleTags, tag)
	}
	addNormalized := func(tagType tags.TagType, raw string) {
		raw = cleanText(raw)
		if raw == "" {
			return
		}
		normalized := tags.NormalizeTagValue(string(tagType), raw)
		if normalized == "" {
			return
		}
		addTitleTag(database.TagInfo{Type: string(tagType), Tag: normalized, Label: raw})
	}

	if table.Year > 0 {
		addTitleTag(database.TagInfo{Type: string(tags.TagTypeYear), Tag: strconv.Itoa(table.Year)})
	}
	addNormalized(tags.TagTypeDeveloper, table.Manufacturer)
	if table.Players > 0 {
		addTitleTag(database.TagInfo{Type: string(tags.TagTypePlayers), Tag: strconv.Itoa(table.Players)})
	}
	for _, genre := range []string{table.GameType, table.Category, table.Theme} {
		addNormalized(tags.TagTypeGenre, genre)
	}
	if notes := cleanText(table.Notes); notes != "" {
		write.TitleProps = append(write.TitleProps, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyDescription), Text: notes,
		})
	}
	write.MediaProps = s.imageProps(table, emu)
	return write
}

// imageProps finds the table's images under the emulator's media folder.
// Popper names every media file after the table file's stem.
func (s *scraperImpl) imageProps(table *pinup.Table, emu *pinup.Emulator) []database.MediaProperty {
	if table.Name == "" {
		return nil
	}
	dir := s.mediaDir(emu)
	props := make([]database.MediaProperty, 0, len(imageScreens))
	for _, screen := range imageScreens {
		for _, ext := range imageExtensions {
			path := filepath.Join(dir, screen.folder, table.Name+ext)
			info, err := s.fs.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			props = append(props, database.MediaProperty{
				TypeTag: tags.PropertyTypeTag(screen.property), Text: filepath.ToSlash(path),
			})
			break
		}
	}
	return props
}

// mediaDir returns the emulator's media folder. Popper's stock database
// records media paths for the drive its template was built on, which need not
// be where the install landed, so a directory that does not exist falls back
// to the install's own POPMedia tree.
func (s *scraperImpl) mediaDir(emu *pinup.Emulator) string {
	if emu.MediaDir != "" {
		if info, err := s.fs.Stat(emu.MediaDir); err == nil && info.IsDir() {
			return emu.MediaDir
		}
	}
	return filepath.Join(s.install.MediaDir, emu.Name)
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
			Msg("pinuppopper: batch write failed, falling back to per-record writes")
	}
	for _, target := range batch {
		if err := s.db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("pinuppopper: write media %d: %w", target.MediaDBID, err)
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
			return fmt.Errorf("pinuppopper: wait while paused: %w", err)
		}
	}
	return nil
}

// wantsSystem reports whether a scrape limited to systems includes systemID;
// an empty list means every system.
func wantsSystem(systems []string, systemID string) bool {
	if len(systems) == 0 {
		return true
	}
	for _, system := range systems {
		if strings.EqualFold(system, systemID) {
			return true
		}
	}
	return false
}

// cleanText collapses whitespace and drops control characters from a field.
func cleanText(value string) string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r < 0x20 || r == 0x7f
	})
	return strings.Join(fields, " ")
}
