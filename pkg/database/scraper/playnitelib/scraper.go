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

// Package playnitelib imports game metadata and artwork from a Playnite
// library into the media database. Playnite holds both: the release year,
// companies, genres and description are fields of each game, and the cover
// and background are image files in its library folder. Media rows indexed by
// the Playnite launcher carry the game's ID in their path, so no name
// matching is needed. The library is read through the Zaparoo extension, so
// Playnite has to be running.
package playnitelib

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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/ssgenre"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/playnite"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/net/html"
)

const (
	scraperID      = "playnite"
	scraperName    = "Playnite library"
	writeBatchSize = 100
	// maxDescriptionLength bounds a stored description. Store pages can run
	// to many thousands of characters of marketing copy.
	maxDescriptionLength = 4000
)

// Library reads the Playnite library with metadata and image paths.
type Library func(ctx context.Context) ([]playnite.Game, error)

// NewPlatformScraper returns the Playnite scraper. library supplies the
// games so the Windows platform can share the launcher's connection to the
// extension and tests can supply a fixture.
func NewPlatformScraper(library Library) platforms.Scraper {
	return platforms.Scraper{
		ID: scraperID, Name: scraperName, SupportedSystemIDs: playnite.Systems(),
		SupportsFillMissing: true,
		AutoScrapeLaunchers: []string{playnite.LauncherID},
		Scrape: func(
			ctx context.Context,
			_ *config.Instance,
			_ platforms.Platform,
			fs afero.Fs,
			db *database.Database,
			opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("playnite: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("playnite: media database is required")
			}
			if library == nil {
				return errors.New("playnite: library reader is required")
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			games, err := library(ctx)
			if err != nil {
				return fmt.Errorf("playnite: %w", err)
			}
			impl := &scraperImpl{fs: fs, db: db.MediaDB, games: indexGames(games)}
			go impl.scrapeLoop(ctx, opts, ch)
			return nil
		},
	}
}

// indexGames keys the library by canonical game ID.
func indexGames(games []playnite.Game) map[string]*playnite.Game {
	byID := make(map[string]*playnite.Game, len(games))
	for i := range games {
		id, err := playnite.NormalizeGameID(games[i].ID)
		if err != nil {
			continue
		}
		byID[id] = &games[i]
	}
	return byID
}

type scraperImpl struct {
	fs    afero.Fs
	db    database.MediaDBI
	games map[string]*playnite.Game
	// unmapped collects the game values this run had no tag mapping for.
	unmapped scraper.UnmappedValues
}

type matchStats struct {
	Processed int
	Matched   int
	Skipped   int
}

// systems returns the systems this run covers: those Playnite games are
// filed under, narrowed to the ones the caller asked for.
func (s *scraperImpl) systems(wanted []string) []string {
	seen := make(map[string]struct{})
	for _, game := range s.games {
		systemID, ok := playnite.SystemForGame(game)
		if !ok || !wantsSystem(wanted, systemID) {
			continue
		}
		seen[systemID] = struct{}{}
	}
	systems := make([]string, 0, len(seen))
	for systemID := range seen {
		systems = append(systems, systemID)
	}
	slices.Sort(systems)
	return systems
}

func (s *scraperImpl) scrapeLoop(ctx context.Context, opts scraper.ScrapeOptions, ch chan<- scraper.ScrapeUpdate) {
	defer close(ch)
	defer s.unmapped.LogSummary(scraperID)
	bgpriority.Apply()

	started := time.Now()
	systems := s.systems(opts.Systems)
	total := matchStats{}
	for i, systemID := range systems {
		if err := waitForScrape(ctx, opts); err != nil {
			break
		}
		stats, err := s.scrapeSystem(ctx, opts, systemID, i+1, len(systems), ch)
		total.Processed += stats.Processed
		total.Matched += stats.Matched
		total.Skipped += stats.Skipped
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: err, Done: true, SystemID: systemID,
				Processed: total.Processed, Total: total.Processed,
				Matched: total.Matched, Skipped: total.Skipped,
				TotalSteps: len(systems), CurrentStep: i + 1,
			}
			return
		}
	}
	log.Debug().
		Int("games", total.Processed).
		Int("matched", total.Matched).
		Int("skipped", total.Skipped).
		Dur("total", time.Since(started)).
		Msg("playnite: scrape complete")
	steps := max(len(systems), 1)
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: total.Processed, Total: total.Processed,
		Matched: total.Matched, Skipped: total.Skipped, TotalSteps: steps, CurrentStep: steps,
	}
}

// scrapeSystem enriches the Playnite media rows of one system. The returned
// stats count what was written, so Matched is the number of rows enriched.
func (s *scraperImpl) scrapeSystem(
	ctx context.Context, opts scraper.ScrapeOptions, systemID string, step, steps int,
	ch chan<- scraper.ScrapeUpdate,
) (matchStats, error) {
	titles, err := s.db.GetTitlesBySystemID(systemID)
	if err != nil {
		return matchStats{}, fmt.Errorf("playnite: load titles: %w", err)
	}
	if len(titles) == 0 {
		return matchStats{}, nil
	}
	media, err := s.db.GetMediaBySystemID(systemID)
	if err != nil {
		return matchStats{}, fmt.Errorf("playnite: load media: %w", err)
	}
	scraped := map[int64]struct{}{}
	switch {
	case opts.RunID != "" && (opts.Force || opts.FillMissing):
		scraped, err = s.db.GetScrapeRunMediaIDs(ctx, scraperID, opts.RunID, titles[0].SystemDBID)
	case !opts.Force && !opts.FillMissing:
		scraped, err = s.db.GetScrapedMediaIDs(ctx, scraperID, titles[0].SystemDBID)
	}
	if err != nil {
		return matchStats{}, fmt.Errorf("playnite: load scraped media: %w", err)
	}

	// First fill of a shared title is deterministic, independent of query order.
	slices.SortFunc(media, func(a, b database.MediaWithFullPath) int { return strings.Compare(a.Path, b.Path) })
	targets, stats := s.buildTargets(media, scraped, opts.RunID)
	for i := range targets {
		targets[i].Write.FillMissing = opts.FillMissing
	}
	report := func(processed, matched int) {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemID, Processed: processed, Total: stats.Processed,
			Matched: matched, Skipped: stats.Skipped, TotalSteps: steps, CurrentStep: step,
		}:
		case <-ctx.Done():
		}
	}

	written := 0
	applyErr := s.applyTargets(ctx, opts, targets, func(from, to int) {
		written += to - from
		if to < len(targets) {
			report(stats.Skipped+written, written)
		}
	})
	result := matchStats{Processed: stats.Skipped + written, Matched: written, Skipped: stats.Skipped}
	if applyErr != nil {
		return result, applyErr
	}
	if stats.Processed > 0 {
		report(stats.Processed, written)
	}
	return result, nil
}

// buildTargets resolves every Playnite-launched media row to the write that
// enriches it. Rows already carrying the sentinel are left out entirely; rows
// whose game has gone from Playnite are counted as skipped.
func (s *scraperImpl) buildTargets(
	media []database.MediaWithFullPath, scraped map[int64]struct{}, runID string,
) ([]database.ScrapeWriteTarget, matchStats) {
	targets := make([]database.ScrapeWriteTarget, 0, len(media))
	stats := matchStats{}
	prefix := shared.SchemePlaynite + "://"
	for i := range media {
		row := &media[i]
		if row.IsMissing || !strings.HasPrefix(strings.ToLower(row.Path), prefix) {
			continue
		}
		if _, already := scraped[row.DBID]; already {
			continue
		}
		stats.Processed++
		gameID, err := playnite.ParseGamePath(row.Path)
		if err != nil {
			log.Debug().Err(err).Str("path", row.Path).Msg("playnite: skipping unparseable media path")
			stats.Skipped++
			continue
		}
		game, ok := s.games[gameID]
		if !ok {
			log.Debug().Str("gameID", gameID).Msg("playnite: game no longer in Playnite library")
			stats.Skipped++
			continue
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID,
			Write: s.buildWrite(game, runID),
		})
		stats.Matched++
	}
	return targets, stats
}

// buildWrite maps one Playnite game to title tags, a description and the
// image properties whose files exist.
func (s *scraperImpl) buildWrite(game *playnite.Game, runID string) *database.ScrapeWrite {
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

	if year := strconv.Itoa(game.ReleaseYear); game.ReleaseYear > 0 && tags.IsValidTagValue(tags.TagTypeYear, year) {
		addTitleTag(database.TagInfo{Type: string(tags.TagTypeYear), Tag: year})
	}
	addCompanies := func(tagType tags.TagType, names []string) {
		for _, raw := range names {
			name := cleanText(raw)
			if name == "" {
				continue
			}
			normalized := string(tags.NormalizeCompanyName(name))
			if tags.IsValidTagValue(tagType, normalized) {
				addTitleTag(database.TagInfo{Type: string(tagType), Tag: normalized, Label: name})
			}
		}
	}
	addCompanies(tags.TagTypeDeveloper, game.Developers)
	addCompanies(tags.TagTypePublisher, game.Publishers)
	for _, raw := range game.Genres {
		values, unmapped := ssgenre.Lookup(raw)
		for _, value := range values {
			addTitleTag(database.TagInfo{Type: string(tags.TagTypeGenre), Tag: string(value)})
		}
		for _, piece := range unmapped {
			s.unmapped.Note(scraperID, tags.TagTypeGenre, piece)
		}
	}
	if description := descriptionText(game.Description); description != "" {
		write.TitleProps = append(write.TitleProps, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyDescription), Text: description,
		})
	}
	write.MediaProps = s.imageProps(game)
	return write
}

// imageProps returns the game's images that exist on disk. Playnite's cover
// is the box art; its background is the wide art shown behind a game.
func (s *scraperImpl) imageProps(game *playnite.Game) []database.MediaProperty {
	images := []struct {
		path     string
		property tags.TagValue
	}{
		{path: game.Cover, property: tags.TagPropertyImageBoxart},
		{path: game.Background, property: tags.TagPropertyImageFanart},
	}
	props := make([]database.MediaProperty, 0, len(images))
	for _, image := range images {
		path := strings.TrimSpace(image.path)
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		info, err := s.fs.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		props = append(props, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(image.property), Text: filepath.ToSlash(path),
		})
	}
	return props
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
			Msg("playnite: batch write failed, falling back to per-record writes")
	}
	for _, target := range batch {
		if err := s.db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("playnite: write media %d: %w", target.MediaDBID, err)
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
			return fmt.Errorf("playnite: wait while paused: %w", err)
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

// descriptionText reduces Playnite's HTML description to plain text, bounded
// in length. Playnite stores descriptions as HTML fragments from store pages.
func descriptionText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var b strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	skipDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return truncateText(cleanText(b.String()), maxDescriptionLength)
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			if isHiddenElement(string(name)) {
				skipDepth++
			}
			_ = b.WriteByte(' ')
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if isHiddenElement(string(name)) && skipDepth > 0 {
				skipDepth--
			}
			_ = b.WriteByte(' ')
		case html.SelfClosingTagToken:
			_ = b.WriteByte(' ')
		case html.TextToken:
			if skipDepth == 0 {
				_, _ = b.Write(tokenizer.Text())
			}
		case html.CommentToken, html.DoctypeToken:
		}
	}
}

// isHiddenElement reports elements whose content is not text to show.
func isHiddenElement(name string) bool {
	return name == "script" || name == "style"
}

// truncateText cuts text to at most limit runes, at a word boundary.
func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:limit])
	if idx := strings.LastIndexByte(cut, ' '); idx > limit/2 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut)
}

// cleanText collapses whitespace and drops control characters from a field.
func cleanText(value string) string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r < 0x20 || r == 0x7f || r == 0xa0
	})
	return strings.Join(fields, " ")
}
