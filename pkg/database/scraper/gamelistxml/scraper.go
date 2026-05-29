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

// Package gamelistxml implements a scraper that reads EmulationStation
// gamelist.xml files to enrich the Zaparoo MediaDB with developer, publisher,
// genre, rating, year, artwork paths, and descriptions.
package gamelistxml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// GamelistRecord is one matched gamelist.xml entry, bundled with the filesystem
// root path and the DB identifiers of the matched MediaTitle and one of its
// Media rows (used as the sentinel write target).
type GamelistRecord struct {
	AvailableMediaDirs map[string]string
	SystemRootPath     string
	Game               esapi.Game
	MatchedMediaDBID   int64
	MatchedTitleDBID   int64
}

// mediaDirCandidates maps each TagPropertyImage value to the ordered list of
// media sub-directory names (under <systemRootPath>/media/) that may hold
// artwork for that property. The first matching directory that contains the
// expected filename wins.
var mediaDirCandidates = map[string][]string{
	string(tags.TagPropertyImageImage):      {"image", "images"},
	string(tags.TagPropertyImageBoxart):     {"boxart", "boxart2d", "boxart2dfront", "box2dfront"},
	string(tags.TagPropertyImageBoxart3D):   {"boxart3d"},
	string(tags.TagPropertyImageBoxartSide): {"boxart2dside"},
	string(tags.TagPropertyImageBoxartBack): {"boxart2dback"},
	string(tags.TagPropertyImageScreenshot): {"screenshot", "screenshots"},
	string(tags.TagPropertyImageThumbnail): {
		"thumbnail", "thumbnails", "box2dfront", "boxart2dfront", "supporttexture",
	},
	string(tags.TagPropertyImageMarquee):   {"marquee", "marquees"},
	string(tags.TagPropertyImageWheel):     {"wheel", "wheels"},
	string(tags.TagPropertyImageFanart):    {"fanart", "fanarts"},
	string(tags.TagPropertyImageTitleshot): {"titleshot", "titleshots", "screenshottitle"},
	string(tags.TagPropertyImageMap):       {"map", "maps"},
}

// GamelistXMLScraper loads and maps EmulationStation gamelist.xml records.
// Use [NewPlatformScraper] to obtain a configured [platforms.Scraper].
type GamelistXMLScraper struct {
	db  database.MediaDBI
	fs  afero.Fs
	cfg *config.Instance
}

type companionStats struct {
	Processed int
	Matched   int
	Skipped   int
}

type loadRecordIndexes struct {
	TitlesBySlug     map[string]database.MediaTitle
	MediaByPathFold  map[string]database.Media
	MediaByTitleDBID map[int64][]database.Media
}

func (g *GamelistXMLScraper) filesystem() afero.Fs {
	if g == nil || g.fs == nil {
		return afero.NewOsFs()
	}
	return g.fs
}

// NewPlatformScraper returns a [platforms.Scraper] backed by EmulationStation
// gamelist.xml files. Systems are resolved at scrape time from the platform
// and media database; no state is captured at construction.
func NewPlatformScraper() platforms.Scraper {
	return platforms.Scraper{
		ID:                 "gamelist.xml",
		Name:               "ES gamelist.xml",
		SupportedSystemIDs: []string{},
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
			systems, err := resolveSystemsFromPlatform(ctx, cfg, pl, db.MediaDB, opts.Systems)
			if err != nil {
				return fmt.Errorf("gamelistxml: resolve systems: %w", err)
			}
			s := &GamelistXMLScraper{db: db.MediaDB, fs: fs, cfg: cfg}
			go s.scrapeLoop(ctx, opts, systems, db.MediaDB, ch)
			return nil
		},
	}
}

// resolveSystemsFromPlatform builds the list of ScrapeSystem values by
// querying the indexed systems from mdb, looking up their definitions, and
// resolving ROM root paths via the platform launcher configuration.
func resolveSystemsFromPlatform(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	mdb database.MediaDBI,
	systemIDs []string,
) ([]scraper.ScrapeSystem, error) {
	indexed, err := mdb.IndexedSystems()
	if err != nil {
		return nil, fmt.Errorf("resolveSystemsFromPlatform: list indexed systems: %w", err)
	}

	want := make(map[string]struct{}, len(indexed))
	if len(systemIDs) == 0 {
		for _, id := range indexed {
			want[id] = struct{}{}
		}
	} else {
		indexedSet := make(map[string]struct{}, len(indexed))
		for _, id := range indexed {
			indexedSet[id] = struct{}{}
		}
		for _, id := range systemIDs {
			if _, ok := indexedSet[id]; ok {
				want[id] = struct{}{}
			}
		}
	}

	dbSystems := make(map[string]database.System, len(want))
	sysDefs := make([]systemdefs.System, 0, len(want))
	for sysID := range want {
		sys, err := mdb.FindSystemBySystemID(sysID)
		if err != nil {
			return nil, fmt.Errorf("resolveSystemsFromPlatform: look up system %q: %w", sysID, err)
		}
		systemDef, err := systemdefs.GetSystem(sysID)
		if err != nil {
			log.Debug().Err(err).Str("system", sysID).
				Msg("resolveSystemsFromPlatform: unknown system definition, skipping")
			continue
		}
		dbSystems[sysID] = sys
		sysDefs = append(sysDefs, *systemDef)
	}

	pathsBySystem := make(map[string][]string, len(sysDefs))
	for _, pathResult := range mediascanner.GetSystemPaths(ctx, cfg, pl, pl.RootDirs(cfg), sysDefs) {
		pathsBySystem[pathResult.System.ID] = append(pathsBySystem[pathResult.System.ID], pathResult.Path)
	}

	result := make([]scraper.ScrapeSystem, 0, len(sysDefs))
	for _, sys := range sysDefs {
		romPaths := pathsBySystem[sys.ID]
		if len(romPaths) == 0 {
			log.Debug().Str("system", sys.ID).Msg("resolveSystemsFromPlatform: no launcher paths found, skipping")
			continue
		}
		result = append(result, scraper.ScrapeSystem{
			DBID:     dbSystems[sys.ID].DBID,
			ID:       sys.ID,
			ROMPaths: romPaths,
		})
	}
	return result, nil
}

// LoadRecords iterates gamelist.xml files found under each ROM root path for
// the given system. It prefers the original slug/title match, uses the XML path
// to select the concrete Media row for that title when possible, and falls back
// to path-only matching for records whose slug is not indexed.
func (g *GamelistXMLScraper) LoadRecords(
	ctx context.Context,
	system scraper.ScrapeSystem,
	indexes loadRecordIndexes,
) ([]*GamelistRecord, error) {
	var records []*GamelistRecord
	candidateMedia := len(indexes.MediaByPathFold)
	candidateTitles := len(indexes.TitlesBySlug)
	var gamelistFiles, gamelistEntries, companionEntriesSkipped, invalidPaths int
	var slugMatches, slugPathSelections, slugFirstMediaFallbacks, pathOnlyFallbacks, unmatchedRecords int

outer:
	for _, rootPath := range system.ROMPaths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		gamelistPath := filepath.Join(rootPath, "gamelist.xml")
		exists, statErr := afero.Exists(g.filesystem(), gamelistPath)
		if statErr != nil || !exists {
			continue
		}

		gl, err := readGameListXMLFS(g.filesystem(), gamelistPath)
		if err != nil {
			log.Warn().Err(err).Str("path", gamelistPath).Msg("gamelistxml: failed to read gamelist.xml, skipping")
			continue
		}

		gamelistFiles++
		gamelistEntries += len(gl.Games)
		log.Info().
			Str("path", gamelistPath).
			Int("entries", len(gl.Games)).
			Msg("gamelistxml: loaded gamelist.xml")

		availableMediaDirs := statMediaDirsFS(g.filesystem(), rootPath)

		for i := range gl.Games {
			game := &gl.Games[i]
			if isCompanionGame(game) {
				companionEntriesSkipped++
				continue
			}

			resolved := resolveESPath(game.Path, rootPath)
			if resolved == "" {
				invalidPaths++
				continue
			}

			pf := mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
				Config:       g.cfg,
				Path:         resolved,
				SystemID:     system.ID,
				NoExt:        true,
				ProvidedName: game.Name,
			})

			if title, ok := indexes.TitlesBySlug[pf.Slug]; ok {
				media, matchedKey := selectMediaForSlugMatch(indexes, title.DBID, resolved)
				if media.DBID == 0 {
					log.Debug().
						Str("system", system.ID).
						Str("slug", pf.Slug).
						Int64("mediaTitleDBID", title.DBID).
						Msg("gamelistxml: slug matched title but no media row found, skipping")
					unmatchedRecords++
					delete(indexes.TitlesBySlug, pf.Slug)
					continue
				}

				slugMatches++
				if matchedKey != "" {
					slugPathSelections++
					delete(indexes.MediaByPathFold, matchedKey)
				} else {
					slugFirstMediaFallbacks++
				}
				records = append(records, &GamelistRecord{
					SystemRootPath:     rootPath,
					AvailableMediaDirs: availableMediaDirs,
					Game:               *game,
					MatchedTitleDBID:   title.DBID,
					MatchedMediaDBID:   media.DBID,
				})
				delete(indexes.TitlesBySlug, pf.Slug)
			} else if media, matchedKey, ok := matchMediaByResolvedPath(indexes.MediaByPathFold, resolved); ok {
				pathOnlyFallbacks++
				log.Debug().
					Str("system", system.ID).
					Str("path", game.Path).
					Str("resolved", resolved).
					Str("name", game.Name).
					Str("slug", pf.Slug).
					Int64("mediaDBID", media.DBID).
					Int64("mediaTitleDBID", media.MediaTitleDBID).
					Msg("gamelistxml: path-only fallback matched record")
				records = append(records, &GamelistRecord{
					SystemRootPath:     rootPath,
					AvailableMediaDirs: availableMediaDirs,
					Game:               *game,
					MatchedTitleDBID:   media.MediaTitleDBID,
					MatchedMediaDBID:   media.DBID,
				})
				delete(indexes.MediaByPathFold, matchedKey)
			} else {
				unmatchedRecords++
			}

			if len(indexes.TitlesBySlug) == 0 && len(indexes.MediaByPathFold) == 0 {
				break outer
			}
		}
	}

	log.Info().
		Str("system", system.ID).
		Int("candidate_titles", candidateTitles).
		Int("candidate_media", candidateMedia).
		Int("gamelist_files", gamelistFiles).
		Int("gamelist_entries", gamelistEntries).
		Int("companion_entries_skipped", companionEntriesSkipped).
		Int("invalid_paths", invalidPaths).
		Int("slug_matches", slugMatches).
		Int("slug_path_selections", slugPathSelections).
		Int("slug_first_media_fallbacks", slugFirstMediaFallbacks).
		Int("path_only_fallbacks", pathOnlyFallbacks).
		Int("unmatched_records", unmatchedRecords).
		Int("matched_records", len(records)).
		Int("remaining_unmatched_titles", len(indexes.TitlesBySlug)).
		Int("remaining_unmatched_media", len(indexes.MediaByPathFold)).
		Int("total_records", len(records)).
		Msg("gamelistxml: finished loading records for system")

	return records, nil
}

const scrapeProgressInterval = 250 * time.Millisecond

// scrapeLoop runs the full load→match→write cycle for all systems, emitting
// progress updates on ch. It closes ch when done.
func (g *GamelistXMLScraper) scrapeLoop(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systems []scraper.ScrapeSystem,
	mdb database.MediaDBI,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)

	const id = "gamelist.xml"
	var totalProcessed, totalMatched, totalSkipped int

	waitForResume := func(systemID string, processed, matched, skipped int) bool {
		if waitErr := opts.Pauser.Wait(ctx); waitErr != nil {
			ch <- scraper.ScrapeUpdate{
				SystemID:  systemID,
				Processed: totalProcessed + processed,
				Matched:   totalMatched + matched,
				Skipped:   totalSkipped + skipped,
				Done:      true,
			}
			return false
		}
		return true
	}

	for _, system := range systems {
		if !waitForResume(system.ID, 0, 0, 0) {
			return
		}

		select {
		case <-ctx.Done():
			ch <- scraper.ScrapeUpdate{
				Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
			}
			return
		case ch <- scraper.ScrapeUpdate{SystemID: system.ID, Total: 0}:
		}

		// Process ZaparooCompanion parent/child entries before regular slug scrape.
		companion := g.processCompanionEntries(ctx, opts, system, mdb)
		totalProcessed += companion.Processed
		totalMatched += companion.Matched
		totalSkipped += companion.Skipped

		titlesBySlug := make(map[string]database.MediaTitle)
		if opts.Force {
			allTitles, titlesErr := mdb.GetTitlesBySystemID(system.ID)
			if titlesErr != nil {
				if errors.Is(titlesErr, context.Canceled) || errors.Is(titlesErr, context.DeadlineExceeded) {
					ch <- scraper.ScrapeUpdate{
						SystemID: system.ID, Done: true, Processed: totalProcessed,
						Matched: totalMatched, Skipped: totalSkipped,
					}
					return
				}
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, FatalErr: titlesErr,
					Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				}
				return
			}
			for _, t := range allTitles {
				titlesBySlug[t.Slug] = database.MediaTitle{
					DBID: t.DBID, SystemDBID: t.SystemDBID, Slug: t.Slug, Name: t.Name,
				}
			}
		} else {
			sentinel := scraper.SentinelTagInfo(id)
			sentinelTag := sentinel.Type + ":" + sentinel.Tag
			unscraped, titlesErr := mdb.FindMediaTitlesWithoutSentinel(ctx, system.DBID, sentinelTag)
			if titlesErr != nil {
				if errors.Is(titlesErr, context.Canceled) || errors.Is(titlesErr, context.DeadlineExceeded) {
					ch <- scraper.ScrapeUpdate{
						SystemID: system.ID, Done: true, Processed: totalProcessed,
						Matched: totalMatched, Skipped: totalSkipped,
					}
					return
				}
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, FatalErr: titlesErr,
					Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				}
				return
			}
			for _, t := range unscraped {
				titlesBySlug[t.Slug] = t
			}
		}

		allMedia, mediaErr := mdb.GetMediaBySystemID(system.ID)
		if mediaErr != nil {
			if errors.Is(mediaErr, context.Canceled) || errors.Is(mediaErr, context.DeadlineExceeded) {
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, Done: true, Processed: totalProcessed,
					Matched: totalMatched, Skipped: totalSkipped,
				}
				return
			}
			ch <- scraper.ScrapeUpdate{
				SystemID: system.ID, FatalErr: mediaErr,
				Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
			}
			return
		}
		scrapedIDs := map[int64]struct{}{}
		if !opts.Force {
			var scrapedErr error
			scrapedIDs, scrapedErr = mdb.GetScrapedMediaIDs(ctx, id, system.DBID)
			if scrapedErr != nil {
				if errors.Is(scrapedErr, context.Canceled) || errors.Is(scrapedErr, context.DeadlineExceeded) {
					ch <- scraper.ScrapeUpdate{
						SystemID: system.ID, Done: true, Processed: totalProcessed,
						Matched: totalMatched, Skipped: totalSkipped,
					}
					return
				}
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, FatalErr: scrapedErr,
					Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				}
				return
			}
		}

		indexes := loadRecordIndexes{
			TitlesBySlug:     titlesBySlug,
			MediaByPathFold:  make(map[string]database.Media, len(allMedia)),
			MediaByTitleDBID: make(map[int64][]database.Media, len(allMedia)),
		}
		for _, m := range allMedia {
			media := database.Media{
				DBID:           m.DBID,
				MediaTitleDBID: m.MediaTitleDBID,
				Path:           m.Path,
			}
			if _, scraped := scrapedIDs[m.DBID]; !scraped {
				indexes.MediaByPathFold[pathFoldKey(m.Path)] = media
			}
			indexes.MediaByTitleDBID[m.MediaTitleDBID] = append(indexes.MediaByTitleDBID[m.MediaTitleDBID], media)
		}
		if len(indexes.TitlesBySlug) == 0 && len(indexes.MediaByPathFold) == 0 {
			continue
		}

		records, loadErr := g.LoadRecords(ctx, system, indexes)
		if loadErr != nil {
			if errors.Is(loadErr, context.Canceled) || errors.Is(loadErr, context.DeadlineExceeded) {
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, Done: true, Processed: totalProcessed, Matched: totalMatched,
					Skipped: totalSkipped,
				}
				return
			}
			ch <- scraper.ScrapeUpdate{
				SystemID: system.ID, FatalErr: loadErr,
				Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
			}
			return
		}

		select {
		case <-ctx.Done():
			ch <- scraper.ScrapeUpdate{
				Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
			}
			return
		case ch <- scraper.ScrapeUpdate{SystemID: system.ID, Total: len(records)}:
		}

		var processed, matched, skipped int
		totalRecords := len(records)
		if !waitForResume(system.ID, processed, matched, skipped) {
			return
		}

		lastProgress := time.Now()
		emitProgress := func(update scraper.ScrapeUpdate, force bool) bool {
			if !force && time.Since(lastProgress) < scrapeProgressInterval {
				return true
			}
			update.SystemID = system.ID
			update.Total = totalProcessed + totalRecords
			update.Processed += totalProcessed
			update.Matched += totalMatched
			update.Skipped += totalSkipped
			select {
			case <-ctx.Done():
				ch <- scraper.ScrapeUpdate{
					SystemID:  system.ID,
					Processed: totalProcessed + processed,
					Matched:   totalMatched + matched,
					Skipped:   totalSkipped + skipped,
					Done:      true,
				}
				return false
			case ch <- update:
				lastProgress = time.Now()
				return true
			}
		}

		for _, record := range records {
			if !waitForResume(system.ID, processed, matched, skipped) {
				return
			}
			select {
			case <-ctx.Done():
				ch <- scraper.ScrapeUpdate{
					SystemID:  system.ID,
					Processed: totalProcessed + processed,
					Matched:   totalMatched + matched,
					Skipped:   totalSkipped + skipped,
					Done:      true,
				}
				return
			default:
			}

			processed++
			if record.MatchedMediaDBID == 0 || record.MatchedTitleDBID == 0 {
				log.Debug().
					Int64("mediaTitleDBID", record.MatchedTitleDBID).
					Msg("gamelistxml: no matched DB IDs, skipping")
				skipped++
				update := scraper.ScrapeUpdate{Processed: processed, Matched: matched, Skipped: skipped}
				if !emitProgress(update, false) {
					return
				}
				continue
			}

			mapped := g.MapToDB(record)
			if !waitForResume(system.ID, processed, matched, skipped) {
				return
			}

			writeErr := mdb.ApplyScrapeResult(
				ctx, record.MatchedMediaDBID, record.MatchedTitleDBID,
				&database.ScrapeWrite{
					Sentinel:   scraper.SentinelTagInfo(id),
					MediaTags:  mapped.MediaTags,
					MediaProps: mapped.MediaProps,
					TitleTags:  mapped.TitleTags,
					TitleProps: mapped.TitleProps,
				})
			if writeErr != nil {
				log.Warn().Err(writeErr).
					Int64("mediaTitleDBID", record.MatchedTitleDBID).
					Str("system", system.ID).
					Msg("gamelistxml: write failed")
				skipped++
				update := scraper.ScrapeUpdate{
					Processed: processed, Matched: matched, Skipped: skipped, Err: writeErr,
				}
				if !emitProgress(update, true) {
					return
				}
				continue
			}

			matched++
			if !emitProgress(scraper.ScrapeUpdate{Processed: processed, Matched: matched, Skipped: skipped}, false) {
				return
			}
		}

		if !emitProgress(scraper.ScrapeUpdate{Processed: processed, Matched: matched, Skipped: skipped}, true) {
			return
		}
		totalProcessed += processed
		totalMatched += matched
		totalSkipped += skipped
	}

	ch <- scraper.ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
}

// MapToDB converts a GamelistRecord into the tag and property writes to apply
// to the matched Media and MediaTitle rows.
func (g *GamelistXMLScraper) MapToDB(record *GamelistRecord) scraper.MapResult {
	var mediaTags []database.TagInfo
	var titleTags []database.TagInfo
	var titleProps []database.MediaProperty
	var mediaProps []database.MediaProperty
	game := record.Game

	// Normalise all string fields before mapping: unescape HTML entities,
	// collapse control whitespace to spaces, and trim surrounding whitespace.
	game.Developer = cleanField(game.Developer)
	game.Publisher = cleanField(game.Publisher)
	game.ReleaseDate = cleanField(game.ReleaseDate)
	game.Rating = cleanField(game.Rating)
	game.Genre = cleanField(game.Genre)
	game.Players = cleanField(game.Players)
	game.ArcadeSystemName = cleanField(game.ArcadeSystemName)
	game.Family = cleanField(game.Family)
	game.Region = cleanField(game.Region)
	game.Lang = cleanField(game.Lang)
	game.Desc = cleanField(game.Desc)
	game.Image = cleanField(game.Image)
	game.Thumbnail = cleanField(game.Thumbnail)
	game.Video = cleanField(game.Video)
	game.Marquee = cleanField(game.Marquee)
	game.Wheel = cleanField(game.Wheel)
	game.FanArt = cleanField(game.FanArt)
	game.TitleShot = cleanField(game.TitleShot)
	game.Map = cleanField(game.Map)
	game.Manual = cleanField(game.Manual)
	game.Screenshot = cleanField(game.Screenshot)
	game.TitleScreen = cleanField(game.TitleScreen)
	game.Boxart2D = cleanField(game.Boxart2D)
	game.Boxart3D = cleanField(game.Boxart3D)
	game.Logo = cleanField(game.Logo)

	// --- MediaTags: ROM-level variant metadata ---

	mediaTags = appendCSVTags(mediaTags, string(tags.TagTypeRegion), game.Region)
	mediaTags = appendCSVTags(mediaTags, string(tags.TagTypeLang), game.Lang)

	// --- MediaTitleTags: title-level, shared across all ROMs ---

	if game.Developer != "" {
		titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypeDeveloper), Tag: game.Developer})
	}
	if game.Publisher != "" {
		titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypePublisher), Tag: game.Publisher})
	}
	if game.ReleaseDate != "" {
		if year := extractYear(game.ReleaseDate); year != "" {
			titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypeYear), Tag: year})
		}
	}
	if game.Rating != "" {
		if r := normalizeRating(game.Rating); r != "" {
			titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypeRating), Tag: r})
		}
	}
	if game.Genre != "" {
		titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypeGenre), Tag: game.Genre})
	}
	// Players: title-level because it describes the game, not a per-ROM variant.
	// Exclusive type: only the highest player count is kept per title.
	if game.Players != "" {
		if p := normalizePlayers(game.Players); p != "" {
			titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: p})
		}
	}
	if game.ArcadeSystemName != "" {
		titleTags = append(titleTags, database.TagInfo{
			Type: string(tags.TagTypeArcadeBoard),
			Tag:  game.ArcadeSystemName,
		})
	}
	if game.Family != "" {
		titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypeGameFamily), Tag: game.Family})
	}

	// --- MediaTitleProperties: title-level shared static content ---

	propType := string(tags.TagTypeProperty)
	root := record.SystemRootPath

	// fallbackNames are ROM-relative PNG filenames used to locate matching
	// artwork files under media/ sub-directories.
	fallbackNames := artworkFallbackNames(game.Path, record.SystemRootPath)

	if game.Desc != "" {
		titleProps = append(titleProps,
			textProp(propType+":"+string(tags.TagPropertyDescription), game.Desc))
	}

	if game.ScreenScraperIDAttr != "" && game.ScreenScraperIDAttr != "0" {
		titleProps = append(titleProps,
			textProp(propType+":"+string(tags.TagPropertyXMLGameID), game.ScreenScraperIDAttr))
	} else if game.ScreenScraperID != 0 {
		titleProps = append(titleProps,
			textProp(propType+":"+string(tags.TagPropertyXMLGameID), strconv.Itoa(game.ScreenScraperID)))
	}

	// For each image property: use the XML path when present, otherwise scan
	// the pre-stated media sub-directories for a matching <stem>.png file.
	appendImageProp := func(propValue tags.TagValue, xmlPath string) {
		key := propType + ":" + string(propValue)
		p := pathProp(key, xmlPath, root)
		if p == nil {
			p = findMediaFilePropFS(
				g.filesystem(), key, fallbackNames,
				mediaDirCandidates[string(propValue)], record.AvailableMediaDirs,
			)
		}
		if p != nil {
			mediaProps = append(mediaProps, *p)
		}
	}

	appendImageProp(tags.TagPropertyImageImage, game.Image)
	appendImageProp(tags.TagPropertyImageBoxart, game.Boxart2D)
	appendImageProp(tags.TagPropertyImageBoxart3D, game.Boxart3D)
	appendImageProp(tags.TagPropertyImageBoxartSide, "")
	appendImageProp(tags.TagPropertyImageBoxartBack, "")
	appendImageProp(tags.TagPropertyImageScreenshot, game.Screenshot)
	// game.Thumbnail in most ES forks (RPI, Sky, Batocera, ES-DE) holds cover art.
	// See esapi/gamelist.go for field-level fork documentation.
	appendImageProp(tags.TagPropertyImageThumbnail, game.Thumbnail)
	appendImageProp(tags.TagPropertyImageMarquee, game.Marquee)
	wheelXML := game.Logo
	if wheelXML == "" {
		wheelXML = game.Wheel
	}
	appendImageProp(tags.TagPropertyImageWheel, wheelXML)
	appendImageProp(tags.TagPropertyImageFanart, game.FanArt)
	titleshotXML := game.TitleScreen
	if titleshotXML == "" {
		titleshotXML = game.TitleShot
	}
	appendImageProp(tags.TagPropertyImageTitleshot, titleshotXML)
	appendImageProp(tags.TagPropertyImageMap, game.Map)

	if p := pathProp(propType+":"+string(tags.TagPropertyVideo), game.Video, root); p != nil {
		mediaProps = append(mediaProps, *p)
	}
	if p := pathProp(propType+":"+string(tags.TagPropertyManual), game.Manual, root); p != nil {
		mediaProps = append(mediaProps, *p)
	}

	return scraper.MapResult{
		MediaTags:  mediaTags,
		MediaProps: mediaProps,
		TitleTags:  titleTags,
		TitleProps: titleProps,
	}
}

// resolveESPath converts an EmulationStation path to an absolute filesystem path.
//
//   - "./relative" or "relative" → filepath.Join(systemRootPath, rest)
//   - "~/..." → filepath.Join(os.UserHomeDir(), rest)
//   - Already absolute → cleaned as-is
//
// Returns "" if the result is not absolute, input is empty, or the resolved path
// escapes systemRootPath.
func resolveESPath(esPath, systemRootPath string) string {
	if esPath == "" {
		return ""
	}
	rootAbs, err := filepath.Abs(systemRootPath)
	if err != nil {
		return ""
	}
	rootAbs = filepath.Clean(rootAbs)

	var abs string
	switch {
	case strings.HasPrefix(esPath, "~/"):
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return ""
		}
		abs = filepath.Join(home, esPath[2:])
	case filepath.IsAbs(esPath):
		abs = filepath.Clean(esPath)
	default:
		// Handles both "./relative" and "relative".
		rel := strings.TrimPrefix(esPath, "./")
		abs = filepath.Join(rootAbs, rel)
	}

	abs, err = filepath.Abs(abs)
	if err != nil || !filepath.IsAbs(abs) {
		return ""
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}

// normalizePlayers extracts the maximum player count from an ES players string.
//
//	"1"   → "1"
//	"1-4" → "4"
//	"1, 2, 4" → "4"
//	"2-4" → "4"
//	""    → ""
func normalizePlayers(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	var maxVal int
	// Split on commas and hyphens to extract all numeric tokens.
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '-' || unicode.IsSpace(r)
	}) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if n > maxVal {
			maxVal = n
		}
	}
	if maxVal == 0 {
		return ""
	}
	return strconv.Itoa(maxVal)
}

// normalizeRating converts an ES float rating string ("0.75") to an integer
// percentage string ("75"). Returns "" for empty or unparseable input.
func normalizeRating(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ""
	}
	return strconv.Itoa(int(math.Round(f * 100)))
}

// extractYear parses an ES date string and returns the 4-digit year.
// Handles formats: "YYYYMMDDTHHMMSS", "YYYY-MM-DD", "YYYY".
// Returns "" for empty or unparseable input.
func extractYear(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return ""
	}
	year := s[:4]
	for _, r := range year {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return year
}

// cleanField normalises a raw string value from a gamelist.xml field:
//  1. Unescapes HTML entities (e.g. &amp; → &, &lt; → <).
//  2. Replaces tab, newline, and carriage-return characters with a single space.
//  3. Trims leading and trailing whitespace.
func cleanField(s string) string {
	s = html.UnescapeString(s)
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// splitCSV splits a comma-separated string and trims each element.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

func appendCSVTags(tagInfos []database.TagInfo, tagType, raw string) []database.TagInfo {
	for _, value := range splitCSV(raw) {
		tagInfos = append(tagInfos, database.TagInfo{Type: tagType, Tag: strings.ToLower(value)})
	}
	return tagInfos
}

// pathProp resolves esPath to an absolute path and returns a MediaProperty for
// the given typeTag. Returns nil if the path cannot be resolved (skipped cleanly).
func pathProp(typeTag, esPath, systemRootPath string) *database.MediaProperty {
	if esPath == "" {
		return nil
	}
	abs := filepath.ToSlash(resolveESAssetPath(esPath, systemRootPath))
	if abs == "" {
		return nil
	}
	return &database.MediaProperty{
		TypeTag:     typeTag,
		Text:        abs,
		ContentType: mimeFromExt(abs),
	}
}

// bound retrieved paths to child dirs of media
func resolveESAssetPath(esPath, systemRootPath string) string {
	abs := resolveESPath(esPath, systemRootPath)
	if abs == "" {
		return ""
	}

	root := filepath.Clean(systemRootPath) + string(filepath.Separator)
	cleanAbs := filepath.Clean(abs) + string(filepath.Separator)
	if !strings.HasPrefix(cleanAbs, root) {
		return ""
	}
	return abs
}

// textProp creates a plain-text MediaProperty.
func textProp(typeTag, text string) database.MediaProperty {
	return database.MediaProperty{
		TypeTag:     typeTag,
		Text:        text,
		ContentType: "text/plain",
	}
}

// statMediaDirs reads the media/ directory under rootPath and returns a map of
// directory name → absolute path for every sub-directory found. Returns nil
// when media/ does not exist or cannot be read — callers treat nil as empty.
func statMediaDirs(rootPath string) map[string]string {
	return statMediaDirsFS(afero.NewOsFs(), rootPath)
}

func statMediaDirsFS(fs afero.Fs, rootPath string) map[string]string {
	mediaRoot := filepath.Join(rootPath, "media")
	entries, err := afero.ReadDir(fs, mediaRoot)
	if err != nil {
		return nil
	}
	dirs := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = filepath.Join(mediaRoot, e.Name())
		}
	}
	return dirs
}

// findMediaFileProp searches for <stem>.png inside the first candidate
// directory (from candidates) that appears in availableDirs. Returns a
// MediaProperty for the file when found, nil otherwise.
func findMediaFileProp(
	typeTag, stem string,
	candidates []string,
	availableDirs map[string]string,
) *database.MediaProperty {
	if stem == "" || stem == "." {
		return nil
	}
	return findMediaFilePropFS(afero.NewOsFs(), typeTag, []string{stem + ".png"}, candidates, availableDirs)
}

func artworkFallbackNames(gamePath, systemRootPath string) []string {
	resolved := resolveESPath(gamePath, systemRootPath)
	if resolved == "" {
		return nil
	}

	rootAbs, err := filepath.Abs(systemRootPath)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(resolved))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}

	stem := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if stem == "" || stem == "." {
		return nil
	}

	flat := stem + ".png"
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return []string{flat}
	}

	nested := filepath.Join(dir, flat)
	if nested == flat {
		return []string{flat}
	}
	return []string{nested, flat}
}

func findMediaFilePropFS(
	fs afero.Fs,
	typeTag string,
	fallbackNames []string,
	candidates []string,
	availableDirs map[string]string,
) *database.MediaProperty {
	if len(fallbackNames) == 0 {
		return nil
	}
	for _, dir := range candidates {
		dirPath, ok := availableDirs[dir]
		if !ok {
			continue
		}
		for _, name := range fallbackNames {
			cleanName := filepath.Clean(name)
			if name == "" || cleanName == "." || cleanName == ".." ||
				strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
				continue
			}
			candidate := filepath.Join(dirPath, name)
			if exists, err := afero.Exists(fs, candidate); err == nil && exists {
				return &database.MediaProperty{
					TypeTag:     typeTag,
					Text:        filepath.ToSlash(candidate),
					ContentType: "image/png",
				}
			}
		}
	}
	return nil
}

func selectMediaForSlugMatch(
	indexes loadRecordIndexes,
	mediaTitleDBID int64,
	resolved string,
) (media database.Media, matchedKey string) {
	media, matchedKey, ok := matchMediaByResolvedPath(indexes.MediaByPathFold, resolved)
	if ok && media.MediaTitleDBID == mediaTitleDBID {
		return media, matchedKey
	}
	if ok {
		log.Debug().
			Str("resolved", resolved).
			Int64("pathMediaDBID", media.DBID).
			Int64("pathMediaTitleDBID", media.MediaTitleDBID).
			Int64("slugMediaTitleDBID", mediaTitleDBID).
			Msg("gamelistxml: slug match path points at different title, using slug title media")
	}

	mediaRows := indexes.MediaByTitleDBID[mediaTitleDBID]
	if len(mediaRows) == 0 {
		return database.Media{}, ""
	}
	return mediaRows[0], ""
}

func matchMediaByResolvedPath(
	mediaByPathFold map[string]database.Media,
	resolved string,
) (database.Media, string, bool) {
	key := pathFoldKey(resolved)
	if media, ok := mediaByPathFold[key]; ok {
		return media, key, true
	}

	if !strings.EqualFold(filepath.Ext(resolved), ".zip") {
		return database.Media{}, "", false
	}

	prefix := key + "/"
	var matchedMedia database.Media
	var matchedKey string
	matches := 0
	for mediaKey, media := range mediaByPathFold {
		if !strings.HasPrefix(mediaKey, prefix) {
			continue
		}
		matches++
		if matches == 1 {
			matchedMedia = media
			matchedKey = mediaKey
		}
	}

	if matches == 1 {
		return matchedMedia, matchedKey, true
	}
	if matches > 1 {
		log.Warn().Str("path", resolved).Int("matches", matches).
			Msg("gamelistxml: zip-as-dir path matched multiple indexed media rows, skipping")
	}
	return database.Media{}, "", false
}

func pathFoldKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

func readGameListXMLFS(fs afero.Fs, path string) (*esapi.GameList, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("read gamelist XML %q: %w", path, err)
	}
	var gameList esapi.GameList
	if err := xml.Unmarshal(data, &gameList); err != nil {
		return nil, fmt.Errorf("parse gamelist XML %q: %w", path, err)
	}
	return &gameList, nil
}

// companionSource is the XML source attribute value that marks ZaparooCompanion entries.
const companionSource = "ZaparooCompanion"

func isCompanionGame(game *esapi.Game) bool {
	return game != nil && (game.Source == companionSource || game.SourceAttr == companionSource)
}

// companionParent holds a ZaparooCompanion parent meta record parsed from a gamelist.xml.
// Parent records carry full metadata but no ROM path; they represent the canonical game
// title shared by multiple regional ROM releases.
type companionParent struct {
	AvailableMediaDirs map[string]string
	SystemRootPath     string
	GameID             string
	Game               esapi.Game
}

// companionChild holds a ZaparooCompanion ROM child record parsed from a gamelist.xml.
// Child records carry only a path and optional region/lang; their metadata is inherited
// from the parent via the parentid → id link.
type companionChild struct {
	ResolvedPath string
	ParentGameID string // value of the XML parentid attribute
	Region       string
	Lang         string
}

// loadCompanionEntries scans all gamelist.xml files for the system and separates
// entries with source="ZaparooCompanion" into parent records (id attr, no path) and
// child records (parentid attr, has path).
func (g *GamelistXMLScraper) loadCompanionEntries(
	ctx context.Context,
	system scraper.ScrapeSystem,
) (parents []companionParent, children []companionChild) {
	for _, rootPath := range system.ROMPaths {
		select {
		case <-ctx.Done():
			return parents, children
		default:
		}

		gamelistPath := filepath.Join(rootPath, "gamelist.xml")
		exists, statErr := afero.Exists(g.filesystem(), gamelistPath)
		if statErr != nil || !exists {
			continue
		}

		gl, err := readGameListXMLFS(g.filesystem(), gamelistPath)
		if err != nil {
			log.Warn().Err(err).Str("path", gamelistPath).
				Msg("gamelistxml: companion: failed to read gamelist.xml")
			continue
		}

		availableMediaDirs := statMediaDirsFS(g.filesystem(), rootPath)

		for i := range gl.Games {
			select {
			case <-ctx.Done():
				return parents, children
			default:
			}
			game := gl.Games[i]
			if !isCompanionGame(&game) {
				log.Debug().Str("source", game.Source).Msg("source not companion")
				continue
			}
			switch {
			case game.ScreenScraperIDAttr != "" && game.Path == "":
				log.Debug().
					Str("gameID", game.ScreenScraperIDAttr).
					Str("name", game.Name).
					Str("gamelist", gamelistPath).
					Msg("gamelistxml: companion: found parent entry")
				parents = append(parents, companionParent{
					Game:               game,
					SystemRootPath:     rootPath,
					AvailableMediaDirs: availableMediaDirs,
					GameID:             game.ScreenScraperIDAttr,
				})
			case game.ParentIDAttr != "" && game.Path != "":
				resolved := resolveESPath(game.Path, rootPath)
				if resolved == "" {
					log.Debug().
						Str("path", game.Path).
						Str("parentID", game.ParentIDAttr).
						Str("gamelist", gamelistPath).
						Msg("gamelistxml: companion: child path failed to resolve, skipping")
					continue
				}
				log.Debug().
					Str("parentID", game.ParentIDAttr).
					Str("resolvedPath", resolved).
					Str("region", game.Region).
					Str("lang", game.Lang).
					Msg("gamelistxml: companion: found child entry")
				children = append(children, companionChild{
					ResolvedPath: resolved,
					ParentGameID: game.ParentIDAttr,
					Region:       cleanField(game.Region),
					Lang:         cleanField(game.Lang),
				})
			default:
				log.Debug().
					Str("gameID", game.ScreenScraperIDAttr).
					Str("parentID", game.ParentIDAttr).
					Str("path", game.Path).
					Str("name", game.Name).
					Msg("gamelistxml: companion: entry skipped (no id+empty-path or parentid+path)")
			}
		}
	}
	return parents, children
}

// mapCompanionParentToResult builds the tag and property writes for a companion parent
// record. MapToDB is safe with an empty Game.Path; the stem becomes "." which is
// rejected by findMediaFilePropFS, so filesystem fallbacks are skipped cleanly.
func (g *GamelistXMLScraper) mapCompanionParentToResult(p *companionParent) scraper.MapResult {
	result := g.MapToDB(&GamelistRecord{
		SystemRootPath:     p.SystemRootPath,
		AvailableMediaDirs: p.AvailableMediaDirs,
		Game:               p.Game,
	})
	result.TitleProps = append(result.TitleProps, result.MediaProps...)
	result.MediaProps = nil
	return result
}

// processCompanionEntries handles ZaparooCompanion-sourced entries in gamelist.xml.
//
// Phase 1 — parent metadata map: entries with an id attribute and no path are mapped
// to their tags and properties using MapToDB. No new MediaTitle rows are created.
//
// Phase 2 — child enrichment: entries with a parentid attribute and a path are resolved
// to their indexed Media row. The parent's metadata (tags + properties) is upserted
// onto the child's existing MediaTitle. Optional region and lang tags are also written
// to the child Media row.
func (g *GamelistXMLScraper) processCompanionEntries(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	system scraper.ScrapeSystem,
	mdb database.MediaDBI,
) companionStats {
	parents, children := g.loadCompanionEntries(ctx, system)
	if len(parents) == 0 && len(children) == 0 {
		log.Debug().Msg("gamelistxml: companion entries not found")
		return companionStats{}
	}

	log.Info().
		Str("system", system.ID).
		Int("parents", len(parents)).
		Int("children", len(children)).
		Msg("gamelistxml: companion: processing entries")

	// Phase 1: map each parent record to its tag+property writes.
	parentMeta := make(map[string]scraper.MapResult, len(parents))
	for i := range parents {
		p := &parents[i]
		parentMeta[p.GameID] = g.mapCompanionParentToResult(p)
		log.Debug().
			Str("gameID", p.GameID).
			Str("name", p.Game.Name).
			Int("tags", len(parentMeta[p.GameID].TitleTags)).
			Int("props", len(parentMeta[p.GameID].TitleProps)).
			Msg("gamelistxml: companion: parent mapped")
	}
	if len(children) == 0 {
		return companionStats{}
	}

	mediaByTitleDBID, mediaErr := companionMediaByTitle(system.ID, mdb)
	if mediaErr != nil {
		log.Warn().Err(mediaErr).Str("system", system.ID).
			Msg("gamelistxml: companion: failed to load media map, skipping companion entries")
		return companionStats{Processed: len(children), Skipped: len(children)}
	}

	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	var stats companionStats

	defer func() {
		log.Info().
			Str("system", system.ID).
			Int("parents", len(parents)).
			Int("children", len(children)).
			Int("processed", stats.Processed).
			Int("matched", stats.Matched).
			Int("skipped", stats.Skipped).
			Bool("force", opts.Force).
			Msg("gamelistxml: companion: finished entries")
	}()

	for _, c := range children {
		stats.Processed++
		meta, ok := parentMeta[c.ParentGameID]
		if !ok {
			log.Debug().Str("parentGameID", c.ParentGameID).
				Msg("gamelistxml: companion: parent not found for child, skipping")
			stats.Skipped++
			continue
		}

		matched := g.matchCompanionChildMedia(ctx, system, c, mediaByTitleDBID, mdb)
		if len(matched) == 0 {
			stats.Skipped++
			continue
		}

		for _, media := range matched {
			writeErr := mdb.ApplyScrapeResult(ctx, media.DBID, media.MediaTitleDBID, &database.ScrapeWrite{
				Sentinel:   sentinel,
				MediaTags:  companionChildTags(c),
				TitleTags:  meta.TitleTags,
				TitleProps: meta.TitleProps,
			})
			if writeErr != nil {
				log.Warn().Err(writeErr).
					Int64("mediaDBID", media.DBID).
					Int64("mediaTitleDBID", media.MediaTitleDBID).
					Msg("gamelistxml: companion: write failed")
				stats.Skipped++
				continue
			}
			stats.Matched++
		}
	}
	return stats
}

func companionMediaByTitle(systemID string, mdb database.MediaDBI) (map[int64]database.Media, error) {
	allMedia, err := mdb.GetMediaBySystemID(systemID)
	if err != nil {
		return nil, fmt.Errorf("load media by system %q: %w", systemID, err)
	}
	mediaByTitleDBID := make(map[int64]database.Media, len(allMedia))
	for _, m := range allMedia {
		if _, exists := mediaByTitleDBID[m.MediaTitleDBID]; !exists {
			mediaByTitleDBID[m.MediaTitleDBID] = database.Media{
				DBID:           m.DBID,
				MediaTitleDBID: m.MediaTitleDBID,
				Path:           m.Path,
			}
		}
	}
	return mediaByTitleDBID, nil
}

func (*GamelistXMLScraper) matchCompanionChildMedia(
	ctx context.Context,
	system scraper.ScrapeSystem,
	child companionChild,
	mediaByTitleDBID map[int64]database.Media,
	mdb database.MediaDBI,
) []database.Media {
	filename := filepath.Base(child.ResolvedPath)
	if filepath.Ext(filename) == ".slug" {
		slug := strings.TrimSuffix(filename, ".slug")
		title, titleErr := mdb.FindMediaTitleBySystemAndSlug(ctx, system.DBID, slug)
		if titleErr != nil {
			log.Debug().Err(titleErr).Str("slug", slug).
				Msg("gamelistxml: companion: error looking up child title by slug")
			return nil
		}
		if title == nil {
			log.Debug().Str("slug", slug).
				Msg("gamelistxml: companion: no indexed title found for child slug, skipping")
			return nil
		}
		media, ok := mediaByTitleDBID[title.DBID]
		if !ok || media.DBID == 0 {
			log.Debug().Int64("mediaTitleDBID", title.DBID).
				Msg("gamelistxml: companion: no media row for slug-matched title, skipping")
			return nil
		}
		return []database.Media{media}
	}

	resolvedPath := filepath.ToSlash(child.ResolvedPath)
	exact, exactErr := mdb.FindMediaBySystemAndPathFold(ctx, system.DBID, resolvedPath)
	if exactErr != nil {
		log.Debug().Err(exactErr).Str("path", resolvedPath).
			Msg("gamelistxml: companion: exact child path lookup failed")
	}
	if exact != nil {
		return []database.Media{*exact}
	}

	matched, mediaErr := mdb.FindMediaBySystemAndPathSuffix(ctx, system.DBID, filename)
	if mediaErr != nil {
		log.Debug().Err(mediaErr).Str("filename", filename).
			Msg("gamelistxml: companion: error looking up child by filename")
		return nil
	}
	if len(matched) == 0 {
		log.Debug().Str("filename", filename).
			Msg("gamelistxml: companion: no indexed media found for child filename, skipping")
		return nil
	}
	if len(matched) > 1 {
		log.Warn().Str("filename", filename).Int("matches", len(matched)).
			Msg("gamelistxml: companion: ambiguous child filename matches, skipping")
		return nil
	}
	return matched
}

func companionChildTags(c companionChild) []database.TagInfo {
	var childTags []database.TagInfo
	childTags = appendCSVTags(childTags, string(tags.TagTypeRegion), c.Region)
	childTags = appendCSVTags(childTags, string(tags.TagTypeLang), c.Lang)
	return childTags
}

// mimeFromExt returns a MIME type based on file extension.
func mimeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/avi"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b":
		return "audio/mp4"
	case ".mpg", ".mpeg":
		return "video/mpeg"
	case ".m4v":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}
