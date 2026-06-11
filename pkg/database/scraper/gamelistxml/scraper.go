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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/perfmetrics"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// GamelistRecord is one matched gamelist.xml entry, bundled with the filesystem
// root path and the DB identifiers of the matched MediaTitle and one of its
// Media rows (used as the sentinel write target).
type GamelistRecord struct {
	AvailableMediaDirs  map[string]string
	SystemRootPath      string
	MatchKind           gamelistMatchKind
	Game                esapi.Game
	MatchedMediaDBID    int64
	MatchedTitleDBID    int64
	MediaLevelWriteSafe bool
}

type gamelistMatchKind string

const (
	gamelistMatchSlugPath     gamelistMatchKind = "slug_path"
	gamelistMatchSlugOnly     gamelistMatchKind = "slug_only"
	gamelistMatchSlugConflict gamelistMatchKind = "slug_conflict"
	gamelistMatchPathOnly     gamelistMatchKind = "path_only"
)

type slugMediaSelection struct {
	matchKind gamelistMatchKind
	key       string
	media     database.Media
}

// mediaDirCandidates maps each TagPropertyImage value to the ordered list of
// media sub-directory names (under <systemRootPath>/media/) that may hold
// artwork for that property. The first matching directory that contains the
// expected filename wins.
var fallbackArtworkExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

var mediaDirCandidates = map[string][]string{
	string(tags.TagPropertyImageImage):      {"image", "images"},
	string(tags.TagPropertyImageBoxart):     {"boxart", "boxart2d", "box2d", "boxart2dfront", "box2dfront"},
	string(tags.TagPropertyImageBoxart3D):   {"boxart3d"},
	string(tags.TagPropertyImageBoxartSide): {"boxart2dside"},
	string(tags.TagPropertyImageBoxartBack): {"boxart2dback"},
	string(tags.TagPropertyImageScreenshot): {"screenshot", "screenshots"},
	string(tags.TagPropertyImageThumbnail): {
		"thumbnail", "thumbnails", "box2dfront", "boxart2dfront", "supporttexture",
	},
	string(tags.TagPropertyImageMarquee): {"marquee", "marquees"},
	string(tags.TagPropertyImageWheel):   {"wheel", "wheels", "logo", "logos"},
	string(tags.TagPropertyImageFanart):  {"fanart", "fanarts"},
	string(tags.TagPropertyImageTitleshot): {
		"titleshot", "titleshots", "titlescreen", "titlescreens", "screenshottitle",
	},
	string(tags.TagPropertyImageMap): {"map", "maps"},
}

// GamelistXMLScraper loads and maps EmulationStation gamelist.xml records.
// Use [NewPlatformScraper] to obtain a configured [platforms.Scraper].
type GamelistXMLScraper struct {
	db                 database.MediaDBI
	fs                 afero.Fs
	cfg                *config.Instance
	externalAssetRoots []string
}

type companionStats struct {
	WriteStats             scrapeWriteStats
	Processed              int
	Matched                int
	Skipped                int
	MissingTitleSlugs      int
	MissingTitleMedia      int
	AmbiguousFilenames     int
	UnmatchedFilenames     int
	UniqueTitleWrites      int
	DuplicateTitles        int
	ConflictingTitleWrites int
}

type scrapeWriteStats struct {
	UniqueTitleDBIDs map[int64]struct{}
	TotalDuration    time.Duration
	MaxDuration      time.Duration
	TitleTags        int
	MediaTags        int
	TitleProps       int
	MediaProps       int
	Sentinels        int
	Writes           int
	Batches          int
	BatchFallbacks   int
}

func (s *scrapeWriteStats) recordWrite(target database.ScrapeWriteTarget, duration time.Duration) {
	s.recordDuration(duration)
	s.recordTarget(target)
}

func (s *scrapeWriteStats) recordBatch(targets []database.ScrapeWriteTarget, duration time.Duration) {
	s.Batches++
	s.recordDuration(duration)
	for _, target := range targets {
		s.recordTarget(target)
	}
}

func (s *scrapeWriteStats) recordDuration(duration time.Duration) {
	if duration <= 0 {
		return
	}
	s.TotalDuration += duration
	if duration > s.MaxDuration {
		s.MaxDuration = duration
	}
}

func (s *scrapeWriteStats) recordTarget(target database.ScrapeWriteTarget) {
	s.Writes++
	s.Sentinels++
	if target.Write == nil {
		return
	}
	s.MediaTags += len(target.Write.MediaTags)
	s.TitleTags += len(target.Write.TitleTags)
	s.MediaProps += len(target.Write.MediaProps)
	s.TitleProps += len(target.Write.TitleProps)
	if s.UniqueTitleDBIDs != nil {
		s.UniqueTitleDBIDs[target.MediaTitleDBID] = struct{}{}
	}
}

func (s *scrapeWriteStats) averageDuration() time.Duration {
	if s.Writes == 0 {
		return 0
	}
	return s.TotalDuration / time.Duration(s.Writes)
}

type loadRecordIndexes struct {
	TitlesBySlug     map[string]database.MediaTitle
	AllTitlesBySlug  map[string]database.MediaTitle
	MediaByPathFold  map[string]database.Media
	MediaByTitleDBID map[int64][]database.Media
	MediaByFilename  map[string][]database.Media
}

func (g *GamelistXMLScraper) filesystem() afero.Fs {
	if g == nil || g.fs == nil {
		return afero.NewOsFs()
	}
	return g.fs
}

func externalAssetRootsForPlatform(cfg *config.Instance, pl platforms.Platform) []string {
	if pl == nil {
		return nil
	}
	switch pl.ID() {
	case ids.Mister, ids.Mistex:
		return pl.RootDirs(cfg)
	default:
		return nil
	}
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
			s := &GamelistXMLScraper{
				db:                 db.MediaDB,
				fs:                 fs,
				cfg:                cfg,
				externalAssetRoots: externalAssetRootsForPlatform(cfg, pl),
			}
			go s.scrapeLoop(ctx, opts, systems, db.MediaDB, ch)
			return nil
		},
	}
}

func orderedScrapeSystemIDs(indexed, requested []string) []string {
	indexedSet := make(map[string]struct{}, len(indexed))
	for _, id := range indexed {
		indexedSet[id] = struct{}{}
	}

	candidateIDs := indexed
	if len(requested) > 0 {
		candidateIDs = requested
	}

	seen := make(map[string]struct{}, len(candidateIDs))
	ordered := make([]string, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if _, ok := indexedSet[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered
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

	wantedIDs := orderedScrapeSystemIDs(indexed, systemIDs)

	dbSystems := make(map[string]database.System, len(wantedIDs))
	sysDefs := make([]systemdefs.System, 0, len(wantedIDs))
	for _, sysID := range wantedIDs {
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

type parsedGamelistFile struct {
	RootPath           string
	GamelistPath       string
	AvailableMediaDirs map[string]string
	Games              []esapi.Game
}

type parsedGamelistSystem struct {
	Files []parsedGamelistFile
}

func (g *GamelistXMLScraper) loadParsedGamelistSystem(
	ctx context.Context,
	system scraper.ScrapeSystem,
) (parsedGamelistSystem, error) {
	var parsed parsedGamelistSystem
	for _, rootPath := range system.ROMPaths {
		select {
		case <-ctx.Done():
			return parsed, ctx.Err()
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

		log.Info().
			Str("path", gamelistPath).
			Int("entries", len(gl.Games)).
			Msg("gamelistxml: loaded gamelist.xml")
		parsed.Files = append(parsed.Files, parsedGamelistFile{
			RootPath:           rootPath,
			GamelistPath:       gamelistPath,
			AvailableMediaDirs: statMediaDirsFS(g.filesystem(), rootPath),
			Games:              gl.Games,
		})
	}
	return parsed, nil
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
	parsed, err := g.loadParsedGamelistSystem(ctx, system)
	if err != nil {
		return nil, err
	}
	return g.loadRecordsFromParsed(ctx, system, indexes, parsed)
}

func (g *GamelistXMLScraper) loadRecordsFromParsed(
	ctx context.Context,
	system scraper.ScrapeSystem,
	indexes loadRecordIndexes,
	parsed parsedGamelistSystem,
) ([]*GamelistRecord, error) {
	var records []*GamelistRecord
	candidateMedia := len(indexes.MediaByPathFold)
	candidateTitles := len(indexes.TitlesBySlug)
	var gamelistFiles, gamelistEntries, companionEntriesSkipped, invalidPaths int
	var slugMatches, slugPathSelections, slugFirstMediaFallbacks, pathOnlyFallbacks, unmatchedRecords int

outer:
	for _, file := range parsed.Files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		gamelistFiles++
		gamelistEntries += len(file.Games)

		for i := range file.Games {
			game := &file.Games[i]
			if isCompanionGame(game) {
				companionEntriesSkipped++
				continue
			}

			resolved := resolveESPath(game.Path, file.RootPath)
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

			pathMedia, matchedPathKey, pathOK := g.canonicalMediaForResolvedPath(indexes, resolved)

			title, titleOK := indexes.TitlesBySlug[pf.Slug]
			switch {
			case titleOK:
				if pathOK && pathMedia.MediaTitleDBID == title.DBID {
					slugMatches++
					slugPathSelections++
					records = append(records, &GamelistRecord{
						SystemRootPath:      file.RootPath,
						AvailableMediaDirs:  file.AvailableMediaDirs,
						Game:                *game,
						MatchKind:           gamelistMatchSlugPath,
						MatchedTitleDBID:    title.DBID,
						MatchedMediaDBID:    pathMedia.DBID,
						MediaLevelWriteSafe: true,
					})
					delete(indexes.MediaByPathFold, matchedPathKey)
					continue
				}

				selection := selectMediaForSlugMatch(indexes, title.DBID, resolved)
				if selection.media.DBID == 0 {
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
				mediaLevelWriteSafe := false
				if selection.key != "" && selection.matchKind == gamelistMatchSlugPath {
					slugPathSelections++
					mediaLevelWriteSafe = true
					delete(indexes.MediaByPathFold, selection.key)
				} else {
					slugFirstMediaFallbacks++
				}
				records = append(records, &GamelistRecord{
					SystemRootPath:      file.RootPath,
					AvailableMediaDirs:  file.AvailableMediaDirs,
					Game:                *game,
					MatchKind:           selection.matchKind,
					MatchedTitleDBID:    title.DBID,
					MatchedMediaDBID:    selection.media.DBID,
					MediaLevelWriteSafe: mediaLevelWriteSafe,
				})
				delete(indexes.TitlesBySlug, pf.Slug)
			case pathOK:
				pathOnlyFallbacks++
				log.Debug().
					Str("system", system.ID).
					Str("path", game.Path).
					Str("resolved", resolved).
					Str("name", game.Name).
					Str("slug", pf.Slug).
					Int64("mediaDBID", pathMedia.DBID).
					Int64("mediaTitleDBID", pathMedia.MediaTitleDBID).
					Msg("gamelistxml: path-only fallback matched record")
				records = append(records, &GamelistRecord{
					SystemRootPath:      file.RootPath,
					AvailableMediaDirs:  file.AvailableMediaDirs,
					Game:                *game,
					MatchKind:           gamelistMatchPathOnly,
					MatchedTitleDBID:    pathMedia.MediaTitleDBID,
					MatchedMediaDBID:    pathMedia.DBID,
					MediaLevelWriteSafe: true,
				})
				delete(indexes.MediaByPathFold, matchedPathKey)
			case titleSlugKnown(indexes, pf.Slug):
				unmatchedRecords++
				log.Debug().
					Str("system", system.ID).
					Str("path", game.Path).
					Str("resolved", resolved).
					Str("name", game.Name).
					Str("slug", pf.Slug).
					Msg("gamelistxml: slug exists for another or already-scraped title, skipping path-only fallback")
			default:
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

const (
	scrapeProgressInterval  = 250 * time.Millisecond
	companionWriteBatchSize = 10
)

func shouldUseRunMarker(opts scraper.ScrapeOptions) bool {
	return opts.Force && opts.RunID != ""
}

func appendRunMarker(scraperID string, opts scraper.ScrapeOptions, write *database.ScrapeWrite) {
	if !shouldUseRunMarker(opts) || write == nil {
		return
	}
	write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(scraperID, opts.RunID))
}

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
	metrics := perfmetrics.NewRecorderForDB(mdb)
	var totalProcessed, totalMatched, totalSkipped int
	totalSteps := len(systems)

	waitForResume := func(systemID string, currentStep, processed, matched, skipped int) bool {
		if waitErr := opts.Pauser.Wait(ctx); waitErr != nil {
			ch <- scraper.ScrapeUpdate{
				SystemID:    systemID,
				Processed:   processed,
				Matched:     matched,
				Skipped:     skipped,
				TotalSteps:  totalSteps,
				CurrentStep: currentStep,
				Done:        true,
			}
			return false
		}
		return true
	}

	for i, system := range systems {
		currentStep := i + 1
		systemStart := time.Now()
		systemMetricsStart := metrics.Capture(ctx, false)
		var titleLoadDuration, allTitlesLoadDuration, mediaLoadDuration time.Duration
		var scrapedIDsLoadDuration, parseDuration, recordLoadDuration time.Duration
		sendUpdate := func(update scraper.ScrapeUpdate) {
			update.TotalSteps = totalSteps
			update.CurrentStep = currentStep
			ch <- update
		}
		if !waitForResume(system.ID, currentStep, totalProcessed, totalMatched, totalSkipped) {
			return
		}

		select {
		case <-ctx.Done():
			ch <- scraper.ScrapeUpdate{
				Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				TotalSteps: totalSteps, CurrentStep: currentStep, Done: true,
			}
			return
		case ch <- scraper.ScrapeUpdate{
			SystemID: system.ID, Total: 0, TotalSteps: totalSteps, CurrentStep: currentStep,
		}:
		}

		titlesBySlug := make(map[string]database.MediaTitle)
		allTitlesBySlug := make(map[string]database.MediaTitle)
		if opts.Force {
			titlesStart := time.Now()
			allTitles, titlesErr := mdb.GetTitlesBySystemID(system.ID)
			titleLoadDuration = time.Since(titlesStart)
			if titlesErr != nil {
				if errors.Is(titlesErr, context.Canceled) || errors.Is(titlesErr, context.DeadlineExceeded) {
					sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
					return
				}
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: titlesErr, Done: true})
				return
			}
			for _, t := range allTitles {
				title := database.MediaTitle{
					DBID: t.DBID, SystemDBID: t.SystemDBID, Slug: t.Slug, Name: t.Name,
				}
				titlesBySlug[t.Slug] = title
				allTitlesBySlug[t.Slug] = title
			}
		} else {
			sentinel := scraper.SentinelTagInfo(id)
			sentinelTag := sentinel.Type + ":" + sentinel.Tag
			titlesStart := time.Now()
			unscraped, titlesErr := mdb.FindMediaTitlesWithoutSentinel(ctx, system.DBID, sentinelTag)
			titleLoadDuration = time.Since(titlesStart)
			if titlesErr != nil {
				if errors.Is(titlesErr, context.Canceled) || errors.Is(titlesErr, context.DeadlineExceeded) {
					sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
					return
				}
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: titlesErr, Done: true})
				return
			}
			for _, t := range unscraped {
				titlesBySlug[t.Slug] = t
			}
			allTitlesStart := time.Now()
			allTitles, allTitlesErr := mdb.GetTitlesBySystemID(system.ID)
			allTitlesLoadDuration = time.Since(allTitlesStart)
			if allTitlesErr != nil {
				if errors.Is(allTitlesErr, context.Canceled) || errors.Is(allTitlesErr, context.DeadlineExceeded) {
					sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
					return
				}
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: allTitlesErr, Done: true})
				return
			}
			for _, t := range allTitles {
				allTitlesBySlug[t.Slug] = database.MediaTitle{
					DBID: t.DBID, SystemDBID: t.SystemDBID, Slug: t.Slug, Name: t.Name,
				}
			}
		}

		mediaStart := time.Now()
		allMedia, mediaErr := mdb.GetMediaBySystemID(system.ID)
		mediaLoadDuration = time.Since(mediaStart)
		if mediaErr != nil {
			if errors.Is(mediaErr, context.Canceled) || errors.Is(mediaErr, context.DeadlineExceeded) {
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
				return
			}
			sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: mediaErr, Done: true})
			return
		}
		scrapedIDs := map[int64]struct{}{}
		if opts.Force {
			if shouldUseRunMarker(opts) {
				var scrapeRunErr error
				scrapedIDsStart := time.Now()
				scrapedIDs, scrapeRunErr = mdb.GetScrapeRunMediaIDs(ctx, id, opts.RunID, system.DBID)
				scrapedIDsLoadDuration = time.Since(scrapedIDsStart)
				if scrapeRunErr != nil {
					if errors.Is(scrapeRunErr, context.Canceled) || errors.Is(scrapeRunErr, context.DeadlineExceeded) {
						sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
						return
					}
					sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: scrapeRunErr, Done: true})
					return
				}
			}
		} else {
			var scrapedErr error
			scrapedIDsStart := time.Now()
			scrapedIDs, scrapedErr = mdb.GetScrapedMediaIDs(ctx, id, system.DBID)
			scrapedIDsLoadDuration = time.Since(scrapedIDsStart)
			if scrapedErr != nil {
				if errors.Is(scrapedErr, context.Canceled) || errors.Is(scrapedErr, context.DeadlineExceeded) {
					sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
					return
				}
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: scrapedErr, Done: true})
				return
			}
		}

		indexes := loadRecordIndexes{
			TitlesBySlug:     titlesBySlug,
			AllTitlesBySlug:  allTitlesBySlug,
			MediaByPathFold:  make(map[string]database.Media, len(allMedia)),
			MediaByTitleDBID: make(map[int64][]database.Media, len(allMedia)),
			MediaByFilename:  make(map[string][]database.Media, len(allMedia)),
		}
		for _, m := range allMedia {
			media := database.Media{
				DBID:           m.DBID,
				MediaTitleDBID: m.MediaTitleDBID,
				Path:           m.Path,
			}
			if _, scraped := scrapedIDs[m.DBID]; !scraped {
				indexes.MediaByPathFold[pathFoldKey(m.Path)] = media
				indexes.MediaByTitleDBID[m.MediaTitleDBID] = append(indexes.MediaByTitleDBID[m.MediaTitleDBID], media)
				filenameKey := mediaFilenameKey(m.Path)
				if filenameKey != "" {
					indexes.MediaByFilename[filenameKey] = append(indexes.MediaByFilename[filenameKey], media)
				}
			}
		}

		parseStart := time.Now()
		parsed, parseErr := g.loadParsedGamelistSystem(ctx, system)
		parseDuration = time.Since(parseStart)
		if parseErr != nil {
			if errors.Is(parseErr, context.Canceled) || errors.Is(parseErr, context.DeadlineExceeded) {
				sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, Done: true})
				return
			}
			sendUpdate(scraper.ScrapeUpdate{SystemID: system.ID, FatalErr: parseErr, Done: true})
			return
		}

		companion := g.processCompanionEntriesFromParsed(
			ctx, opts, system, mdb, indexes, parsed, ch, totalSteps, currentStep,
		)
		if !waitForResume(system.ID, currentStep, companion.Processed, companion.Matched, companion.Skipped) {
			return
		}

		if len(indexes.TitlesBySlug) == 0 && len(indexes.MediaByPathFold) == 0 {
			if companion.Processed > 0 {
				ch <- scraper.ScrapeUpdate{
					SystemID:    system.ID,
					Total:       companion.Processed,
					Processed:   companion.Processed,
					Matched:     companion.Matched,
					Skipped:     companion.Skipped,
					TotalSteps:  totalSteps,
					CurrentStep: currentStep,
				}
			}
			totalProcessed += companion.Processed
			totalMatched += companion.Matched
			totalSkipped += companion.Skipped
			continue
		}

		recordLoadStart := time.Now()
		records, loadErr := g.loadRecordsFromParsed(ctx, system, indexes, parsed)
		recordLoadDuration = time.Since(recordLoadStart)
		if loadErr != nil {
			if errors.Is(loadErr, context.Canceled) || errors.Is(loadErr, context.DeadlineExceeded) {
				sendUpdate(scraper.ScrapeUpdate{
					SystemID: system.ID, Done: true, Processed: companion.Processed, Matched: companion.Matched,
					Skipped: companion.Skipped,
				})
				return
			}
			sendUpdate(scraper.ScrapeUpdate{
				SystemID: system.ID, FatalErr: loadErr,
				Done: true, Processed: companion.Processed, Matched: companion.Matched, Skipped: companion.Skipped,
			})
			return
		}

		var processed, matched, skipped int
		regularWriteStats := scrapeWriteStats{UniqueTitleDBIDs: make(map[int64]struct{})}
		totalRecords := len(records)
		systemTotal := companion.Processed + totalRecords

		select {
		case <-ctx.Done():
			ch <- scraper.ScrapeUpdate{
				Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
				TotalSteps: totalSteps, CurrentStep: currentStep, Done: true,
			}
			return
		case ch <- scraper.ScrapeUpdate{
			SystemID:    system.ID,
			Total:       systemTotal,
			Processed:   companion.Processed,
			Matched:     companion.Matched,
			Skipped:     companion.Skipped,
			TotalSteps:  totalSteps,
			CurrentStep: currentStep,
		}:
		}
		if !waitForResume(
			system.ID,
			currentStep,
			companion.Processed+processed,
			companion.Matched+matched,
			companion.Skipped+skipped,
		) {
			return
		}

		lastProgress := time.Now()
		emitProgress := func(update scraper.ScrapeUpdate, force bool) bool {
			if !force && time.Since(lastProgress) < scrapeProgressInterval {
				return true
			}
			update.SystemID = system.ID
			update.Total = systemTotal
			update.Processed += companion.Processed
			update.Matched += companion.Matched
			update.Skipped += companion.Skipped
			update.TotalSteps = totalSteps
			update.CurrentStep = currentStep
			select {
			case <-ctx.Done():
				ch <- scraper.ScrapeUpdate{
					SystemID:    system.ID,
					Processed:   companion.Processed + processed,
					Matched:     companion.Matched + matched,
					Skipped:     companion.Skipped + skipped,
					TotalSteps:  totalSteps,
					CurrentStep: currentStep,
					Done:        true,
				}
				return false
			case ch <- update:
				lastProgress = time.Now()
				return true
			}
		}

		for _, record := range records {
			if !waitForResume(
				system.ID,
				currentStep,
				companion.Processed+processed,
				companion.Matched+matched,
				companion.Skipped+skipped,
			) {
				return
			}
			select {
			case <-ctx.Done():
				ch <- scraper.ScrapeUpdate{
					SystemID:    system.ID,
					Processed:   companion.Processed + processed,
					Matched:     companion.Matched + matched,
					Skipped:     companion.Skipped + skipped,
					TotalSteps:  totalSteps,
					CurrentStep: currentStep,
					Done:        true,
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
			if !record.MediaLevelWriteSafe {
				if len(mapped.MediaTags) > 0 || len(mapped.MediaProps) > 0 {
					log.Debug().
						Str("system", system.ID).
						Str("path", record.Game.Path).
						Str("matchKind", string(record.MatchKind)).
						Int64("mediaDBID", record.MatchedMediaDBID).
						Int64("mediaTitleDBID", record.MatchedTitleDBID).
						Msg("gamelistxml: omitting media-level metadata for slug-only match")
				}
				mapped.MediaTags = nil
				mapped.MediaProps = nil
			}
			if !waitForResume(
				system.ID,
				currentStep,
				companion.Processed+processed,
				companion.Matched+matched,
				companion.Skipped+skipped,
			) {
				return
			}

			write := &database.ScrapeWrite{
				Sentinel:   scraper.SentinelTagInfo(id),
				MediaTags:  mapped.MediaTags,
				MediaProps: mapped.MediaProps,
				TitleTags:  mapped.TitleTags,
				TitleProps: mapped.TitleProps,
			}
			appendRunMarker(id, opts, write)
			writeTarget := database.ScrapeWriteTarget{
				MediaDBID:      record.MatchedMediaDBID,
				MediaTitleDBID: record.MatchedTitleDBID,
				Write:          write,
			}
			writeStart := time.Now()
			writeErr := mdb.ApplyScrapeResult(ctx, writeTarget.MediaDBID, writeTarget.MediaTitleDBID, writeTarget.Write)
			regularWriteStats.recordWrite(writeTarget, time.Since(writeStart))
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
		logScrapeWriteStats("gamelistxml: regular write stats", system.ID, &regularWriteStats)
		systemMetricsEnd := metrics.Capture(ctx, false)
		perfmetrics.AddDelta(
			log.Info().
				Str("scraper", id).
				Str("system", system.ID).
				Int("records", totalRecords).
				Int("companionProcessed", companion.Processed).
				Int("processed", companion.Processed+processed).
				Int("matched", companion.Matched+matched).
				Int("skipped", companion.Skipped+skipped).
				Int("titleCandidates", len(titlesBySlug)).
				Int("mediaCandidates", len(indexes.MediaByPathFold)).
				Dur("elapsed", time.Since(systemStart)).
				Dur("titleLoadDuration", titleLoadDuration).
				Dur("allTitlesLoadDuration", allTitlesLoadDuration).
				Dur("mediaLoadDuration", mediaLoadDuration).
				Dur("scrapedIDsLoadDuration", scrapedIDsLoadDuration).
				Dur("parseDuration", parseDuration).
				Dur("recordLoadDuration", recordLoadDuration).
				Dur("writeDuration", regularWriteStats.TotalDuration).
				Dur("avgWriteDuration", regularWriteStats.averageDuration()).
				Dur("maxWriteDuration", regularWriteStats.MaxDuration).
				Int("writes", regularWriteStats.Writes),
			systemMetricsStart,
			systemMetricsEnd,
		).Msg("gamelistxml: completed system scrape")
		totalProcessed += companion.Processed + processed
		totalMatched += companion.Matched + matched
		totalSkipped += companion.Skipped + skipped
	}

	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
		TotalSteps: totalSteps, CurrentStep: totalSteps,
	}
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
		titleTags = appendNormalizedTag(titleTags, string(tags.TagTypeDeveloper), game.Developer, game.Developer)
	}
	if game.Publisher != "" {
		titleTags = appendNormalizedTag(titleTags, string(tags.TagTypePublisher), game.Publisher, game.Publisher)
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
		titleTags = appendNormalizedTag(titleTags, string(tags.TagTypeGenre), game.Genre, game.Genre)
	}
	// Players: title-level because it describes the game, not a per-ROM variant.
	// Exclusive type: only the highest player count is kept per title.
	if game.Players != "" {
		if p := normalizePlayers(game.Players); p != "" {
			titleTags = append(titleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: p})
		}
	}
	if game.ArcadeSystemName != "" {
		titleTags = appendNormalizedTag(
			titleTags, string(tags.TagTypeArcadeBoard), game.ArcadeSystemName, game.ArcadeSystemName,
		)
	}
	if game.Family != "" {
		titleTags = appendNormalizedTag(titleTags, string(tags.TagTypeGameFamily), game.Family, game.Family)
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
		p := pathProp(key, xmlPath, root, g.externalAssetRoots)
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

	if p := pathProp(propType+":"+string(tags.TagPropertyVideo), game.Video, root, g.externalAssetRoots); p != nil {
		mediaProps = append(mediaProps, *p)
	}
	if p := pathProp(propType+":"+string(tags.TagPropertyManual), game.Manual, root, g.externalAssetRoots); p != nil {
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
		tagInfos = appendNormalizedTag(tagInfos, tagType, value, "")
	}
	return tagInfos
}

func appendNormalizedTag(tagInfos []database.TagInfo, tagType, raw, label string) []database.TagInfo {
	normalized := tags.NormalizeTagValue(tagType, raw)
	if normalized == "" {
		return tagInfos
	}
	return append(tagInfos, database.TagInfo{Type: tagType, Tag: normalized, Label: label})
}

// pathProp resolves esPath to an absolute path and returns a MediaProperty for
// the given typeTag. Returns nil if the path cannot be resolved (skipped cleanly).
func pathProp(typeTag, esPath, systemRootPath string, externalAssetRoots []string) *database.MediaProperty {
	if esPath == "" {
		return nil
	}
	abs := filepath.ToSlash(resolveESAssetPath(esPath, systemRootPath, externalAssetRoots))
	if abs == "" {
		return nil
	}
	return &database.MediaProperty{
		TypeTag:     typeTag,
		Text:        abs,
		ContentType: mimeFromExt(abs),
	}
}

// resolveESAssetPath resolves a gamelist asset path. Relative asset paths stay
// bound to the system ROM root. Absolute paths can also resolve under configured
// external asset roots for platforms whose storage routes intentionally overlap.
func resolveESAssetPath(esPath, systemRootPath string, externalAssetRoots []string) string {
	abs, ok := resolveESPathAbs(esPath, systemRootPath)
	if !ok {
		return ""
	}
	if pathWithinRoot(abs, systemRootPath) {
		return abs
	}
	if filepath.IsAbs(esPath) || strings.HasPrefix(esPath, "~/") {
		for _, root := range externalAssetRoots {
			if pathWithinRoot(abs, root) {
				return abs
			}
		}
	}
	return ""
}

func resolveESPathAbs(esPath, systemRootPath string) (string, bool) {
	if esPath == "" {
		return "", false
	}
	rootAbs, err := filepath.Abs(systemRootPath)
	if err != nil {
		return "", false
	}
	rootAbs = filepath.Clean(rootAbs)

	var abs string
	switch {
	case strings.HasPrefix(esPath, "~/"):
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", false
		}
		abs = filepath.Join(home, esPath[2:])
	case filepath.IsAbs(esPath):
		abs = filepath.Clean(esPath)
	default:
		rel := strings.TrimPrefix(esPath, "./")
		abs = filepath.Join(rootAbs, rel)
	}

	abs, err = filepath.Abs(abs)
	if err != nil || !filepath.IsAbs(abs) {
		return "", false
	}
	return filepath.Clean(abs), true
}

func pathWithinRoot(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs = filepath.Clean(pathAbs)
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	return findMediaFilePropFS(afero.NewOsFs(), typeTag, fallbackArtworkNames(stem), candidates, availableDirs)
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

	flatNames := fallbackArtworkNames(stem)
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return flatNames
	}

	fallbackNames := make([]string, 0, len(flatNames)*2)
	for _, flat := range flatNames {
		nested := filepath.Join(dir, flat)
		if nested != flat {
			fallbackNames = append(fallbackNames, nested)
		}
	}
	fallbackNames = append(fallbackNames, flatNames...)
	return fallbackNames
}

func fallbackArtworkNames(stem string) []string {
	if stem == "" || stem == "." {
		return nil
	}
	names := make([]string, 0, len(fallbackArtworkExtensions))
	for _, ext := range fallbackArtworkExtensions {
		names = append(names, stem+ext)
	}
	return names
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
					ContentType: mimeFromExt(candidate),
				}
			}
		}
	}
	return nil
}

func titleSlugKnown(indexes loadRecordIndexes, slug string) bool {
	if indexes.AllTitlesBySlug != nil {
		_, ok := indexes.AllTitlesBySlug[slug]
		return ok
	}
	_, ok := indexes.TitlesBySlug[slug]
	return ok
}

func selectMediaForSlugMatch(
	indexes loadRecordIndexes,
	mediaTitleDBID int64,
	resolved string,
) slugMediaSelection {
	media, matchedKey, ok := matchMediaByResolvedPath(indexes.MediaByPathFold, resolved)
	if ok && media.MediaTitleDBID == mediaTitleDBID {
		return slugMediaSelection{media: media, matchKind: gamelistMatchSlugPath, key: matchedKey}
	}

	matchKind := gamelistMatchSlugOnly
	if ok {
		matchKind = gamelistMatchSlugConflict
		log.Warn().
			Str("resolved", resolved).
			Int64("pathMediaDBID", media.DBID).
			Int64("pathMediaTitleDBID", media.MediaTitleDBID).
			Int64("slugMediaTitleDBID", mediaTitleDBID).
			Msg("gamelistxml: slug match path points at different title, writing title metadata only")
	}

	mediaRows := indexes.MediaByTitleDBID[mediaTitleDBID]
	if len(mediaRows) == 0 {
		return slugMediaSelection{matchKind: matchKind}
	}
	return slugMediaSelection{media: mediaRows[0], matchKind: matchKind}
}

func (g *GamelistXMLScraper) canonicalMediaForResolvedPath(
	indexes loadRecordIndexes,
	resolved string,
) (database.Media, string, bool) {
	media, matchedKey, ok := matchMediaByResolvedPath(indexes.MediaByPathFold, resolved)
	if ok {
		return media, matchedKey, true
	}
	if !isCDTrackLikePath(resolved) {
		return database.Media{}, "", false
	}
	return g.matchCanonicalCDMedia(indexes, resolved)
}

func (g *GamelistXMLScraper) matchCanonicalCDMedia(
	indexes loadRecordIndexes,
	resolved string,
) (database.Media, string, bool) {
	m3uMatches := make(map[string]database.Media)
	cueMatches := make(map[string]database.Media)
	for key, media := range indexes.MediaByPathFold {
		switch strings.ToLower(filepath.Ext(media.Path)) {
		case ".m3u":
			if g.m3uReferencesPath(media.Path, resolved) {
				m3uMatches[key] = media
			}
		case ".cue":
			if g.cueReferencesPath(media.Path, resolved) {
				cueMatches[key] = media
			}
		}
	}

	if len(m3uMatches) == 1 {
		for key, media := range m3uMatches {
			return media, key, true
		}
	}
	if len(m3uMatches) > 1 {
		log.Debug().Str("path", resolved).Int("matches", len(m3uMatches)).
			Msg("gamelistxml: track path matched multiple m3u media rows, skipping canonical match")
		return database.Media{}, "", false
	}

	if len(cueMatches) == 1 {
		for key, media := range cueMatches {
			return media, key, true
		}
	}
	if len(cueMatches) > 1 {
		log.Debug().Str("path", resolved).Int("matches", len(cueMatches)).
			Msg("gamelistxml: track path matched multiple cue media rows, skipping canonical match")
	}
	return database.Media{}, "", false
}

func isCDTrackLikePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bin", ".iso", ".wav", ".flac", ".mp3", ".ogg", ".raw", ".img":
		return true
	default:
		return false
	}
}

func (g *GamelistXMLScraper) m3uReferencesPath(m3uPath, resolved string) bool {
	data, err := afero.ReadFile(g.filesystem(), m3uPath)
	if err != nil {
		log.Debug().Err(err).Str("path", m3uPath).Msg("gamelistxml: failed to read m3u for canonical match")
		return false
	}
	baseDir := filepath.Dir(m3uPath)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entryPath := g.caseInsensitiveExistingPath(resolveReferencedPath(baseDir, line))
		if samePath(entryPath, resolved) {
			return true
		}
		if strings.EqualFold(filepath.Ext(entryPath), ".cue") && g.cueReferencesPath(entryPath, resolved) {
			return true
		}
	}
	return false
}

func (g *GamelistXMLScraper) cueReferencesPath(cuePath, resolved string) bool {
	data, err := afero.ReadFile(g.filesystem(), cuePath)
	if err != nil {
		log.Debug().Err(err).Str("path", cuePath).Msg("gamelistxml: failed to read cue for canonical match")
		return false
	}
	baseDir := filepath.Dir(cuePath)
	for _, rawLine := range strings.Split(string(data), "\n") {
		entry := parseCueFileEntry(rawLine)
		if entry == "" {
			continue
		}
		if samePath(resolveReferencedPath(baseDir, entry), resolved) {
			return true
		}
	}
	return false
}

func parseCueFileEntry(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(strings.ToUpper(line), "FILE ") {
		return ""
	}
	rest := strings.TrimSpace(line[len("FILE "):])
	if rest == "" {
		return ""
	}
	if strings.HasPrefix(rest, "\"") {
		rest = rest[1:]
		end := strings.Index(rest, "\"")
		if end <= 0 {
			return ""
		}
		return strings.TrimSpace(rest[:end])
	}

	upper := strings.ToUpper(rest)
	for _, token := range []string{" BINARY", " WAVE", " MP3", " AIFF", " MOTOROLA"} {
		if idx := strings.LastIndex(upper, token); idx > 0 {
			return strings.TrimSpace(rest[:idx])
		}
	}
	return strings.TrimSpace(rest)
}

func (g *GamelistXMLScraper) caseInsensitiveExistingPath(path string) string {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return path
	}
	if exists, err := afero.Exists(g.filesystem(), path); err == nil && exists {
		return path
	}

	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	actualParent := g.caseInsensitiveExistingPath(parent)
	entries, err := afero.ReadDir(g.filesystem(), actualParent)
	if err != nil {
		return path
	}
	base := filepath.Base(path)
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), base) {
			return filepath.Join(actualParent, entry.Name())
		}
	}
	return path
}

func resolveReferencedPath(baseDir, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if filepath.IsAbs(ref) {
		return filepath.Clean(ref)
	}
	return filepath.Clean(filepath.Join(baseDir, ref))
}

func samePath(a, b string) bool {
	return pathFoldKey(a) == pathFoldKey(b)
}

func matchMediaByResolvedPath(
	mediaByPathFold map[string]database.Media,
	resolved string,
) (database.Media, string, bool) {
	key := pathFoldKey(resolved)
	if media, ok := mediaByPathFold[key]; ok {
		return media, key, true
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
			Msg("gamelistxml: container path matched multiple indexed media rows, skipping")
	}
	return database.Media{}, "", false
}

func pathFoldKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

func mediaFilenameKey(path string) string {
	filename := filepath.Base(filepath.ToSlash(path))
	if filename == "." || filename == string(filepath.Separator) {
		return ""
	}
	return strings.ToLower(filename)
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

type companionMediaMatch struct {
	Media               []database.Media
	MediaLevelWriteSafe bool
}

// loadCompanionEntries scans all gamelist.xml files for the system and separates
// entries with source="ZaparooCompanion" into parent records (id attr, no path) and
// child records (parentid attr, has path).
func (g *GamelistXMLScraper) loadCompanionEntries(
	ctx context.Context,
	system scraper.ScrapeSystem,
) (parents []companionParent, children []companionChild) {
	parsed, err := g.loadParsedGamelistSystem(ctx, system)
	if err != nil {
		return nil, nil
	}
	return companionEntriesFromParsed(ctx, system, parsed)
}

func companionEntriesFromParsed(
	ctx context.Context,
	system scraper.ScrapeSystem,
	parsed parsedGamelistSystem,
) (parents []companionParent, children []companionChild) {
	var skippedNonCompanion int
	var skippedMalformed int
	var unresolvedChildPaths int
	for _, file := range parsed.Files {
		for i := range file.Games {
			select {
			case <-ctx.Done():
				return parents, children
			default:
			}
			game := file.Games[i]
			if !isCompanionGame(&game) {
				skippedNonCompanion++
				continue
			}
			switch {
			case game.ScreenScraperIDAttr != "" && game.Path == "":
				parents = append(parents, companionParent{
					Game:               game,
					SystemRootPath:     file.RootPath,
					AvailableMediaDirs: file.AvailableMediaDirs,
					GameID:             game.ScreenScraperIDAttr,
				})
			case game.ParentIDAttr != "" && game.Path != "":
				resolved := resolveESPath(game.Path, file.RootPath)
				if resolved == "" {
					unresolvedChildPaths++
					continue
				}
				children = append(children, companionChild{
					ResolvedPath: resolved,
					ParentGameID: game.ParentIDAttr,
					Region:       cleanField(game.Region),
					Lang:         cleanField(game.Lang),
				})
			default:
				skippedMalformed++
			}
		}
	}
	log.Debug().
		Str("system", system.ID).
		Int("parents", len(parents)).
		Int("children", len(children)).
		Int("skipped_non_companion", skippedNonCompanion).
		Int("skipped_malformed", skippedMalformed).
		Int("unresolved_child_paths", unresolvedChildPaths).
		Msg("gamelistxml: companion: loaded entries")
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
	indexes loadRecordIndexes,
	ch chan<- scraper.ScrapeUpdate,
) companionStats {
	parsed, err := g.loadParsedGamelistSystem(ctx, system)
	if err != nil {
		return companionStats{}
	}
	return g.processCompanionEntriesFromParsed(ctx, opts, system, mdb, indexes, parsed, ch, 0, 0)
}

func (g *GamelistXMLScraper) processCompanionEntriesFromParsed(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	system scraper.ScrapeSystem,
	mdb database.MediaDBI,
	indexes loadRecordIndexes,
	parsed parsedGamelistSystem,
	ch chan<- scraper.ScrapeUpdate,
	totalSteps int,
	currentStep int,
) companionStats {
	parents, children := companionEntriesFromParsed(ctx, system, parsed)
	if len(parents) == 0 && len(children) == 0 {
		log.Debug().Msg("gamelistxml: companion entries not found")
		return companionStats{}
	}

	log.Info().
		Str("system", system.ID).
		Int("parents", len(parents)).
		Int("children", len(children)).
		Msg("gamelistxml: companion: processing entries")

	parentMeta := make(map[string]scraper.MapResult, len(parents))
	for i := range parents {
		p := &parents[i]
		parentMeta[p.GameID] = g.mapCompanionParentToResult(p)
	}
	if len(children) == 0 {
		return companionStats{}
	}

	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	stats := companionStats{WriteStats: scrapeWriteStats{UniqueTitleDBIDs: make(map[int64]struct{})}}
	lastProgress := time.Now().Add(-scrapeProgressInterval)
	emitProgress := func(force bool) bool {
		if ch == nil {
			return true
		}
		if !force && time.Since(lastProgress) < scrapeProgressInterval {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case ch <- scraper.ScrapeUpdate{
			SystemID:    system.ID,
			Total:       len(children),
			Processed:   stats.Processed,
			Matched:     stats.Matched,
			Skipped:     stats.Skipped,
			TotalSteps:  totalSteps,
			CurrentStep: currentStep,
		}:
			lastProgress = time.Now()
			return true
		}
	}

	defer func() {
		log.Info().
			Str("system", system.ID).
			Int("parents", len(parents)).
			Int("children", len(children)).
			Int("processed", stats.Processed).
			Int("matched", stats.Matched).
			Int("skipped", stats.Skipped).
			Int("missing_title_slugs", stats.MissingTitleSlugs).
			Int("missing_title_media", stats.MissingTitleMedia).
			Int("ambiguous_filenames", stats.AmbiguousFilenames).
			Int("unmatched_filenames", stats.UnmatchedFilenames).
			Int("unique_title_writes", stats.UniqueTitleWrites).
			Int("duplicate_title_writes", stats.DuplicateTitles).
			Int("conflicting_title_writes", stats.ConflictingTitleWrites).
			Bool("force", opts.Force).
			Msg("gamelistxml: companion: finished entries")
		logScrapeWriteStats("gamelistxml: companion write stats", system.ID, &stats.WriteStats)
	}()

	if !emitProgress(true) {
		return stats
	}
	titlePayloadByTitleDBID := make(map[int64]string)
	pendingWrites := make([]database.ScrapeWriteTarget, 0, companionWriteBatchSize)
	flushWrites := func(force bool) bool {
		if len(pendingWrites) == 0 || (!force && len(pendingWrites) < companionWriteBatchSize) {
			return true
		}
		if !emitProgress(force) {
			return false
		}
		succeeded, failed := applyCompanionWriteTargets(ctx, mdb, pendingWrites, &stats.WriteStats)
		stats.Matched += succeeded
		stats.Skipped += failed
		pendingWrites = pendingWrites[:0]
		return emitProgress(force)
	}
	for _, c := range children {
		select {
		case <-ctx.Done():
			return stats
		default:
		}
		stats.Processed++
		meta, ok := parentMeta[c.ParentGameID]
		if !ok {
			stats.Skipped++
			if !emitProgress(false) {
				return stats
			}
			continue
		}

		matched := matchCompanionChildMedia(system, c, indexes, &stats)
		if len(matched.Media) == 0 {
			stats.Skipped++
			if !emitProgress(false) {
				return stats
			}
			continue
		}
		consumeCompanionMediaMatches(indexes, matched.Media)

		for _, media := range matched.Media {
			write := &database.ScrapeWrite{
				Sentinel:   sentinel,
				TitleTags:  meta.TitleTags,
				TitleProps: meta.TitleProps,
			}
			if matched.MediaLevelWriteSafe {
				write.MediaTags = companionChildTags(c)
			}
			appendRunMarker("gamelist.xml", opts, write)
			titlePayloadKey := companionTitlePayloadKey(write)
			if existingKey, ok := titlePayloadByTitleDBID[media.MediaTitleDBID]; !ok {
				titlePayloadByTitleDBID[media.MediaTitleDBID] = titlePayloadKey
				stats.UniqueTitleWrites++
			} else if existingKey == titlePayloadKey {
				write.TitleTags = nil
				write.TitleProps = nil
				stats.DuplicateTitles++
			} else {
				stats.ConflictingTitleWrites++
			}
			pendingWrites = append(pendingWrites, database.ScrapeWriteTarget{
				MediaDBID:      media.DBID,
				MediaTitleDBID: media.MediaTitleDBID,
				Write:          write,
			})
			if !flushWrites(false) {
				return stats
			}
		}
		if !emitProgress(false) {
			return stats
		}
	}
	if !flushWrites(true) {
		return stats
	}
	_ = emitProgress(true)
	return stats
}

func companionTitlePayloadKey(write *database.ScrapeWrite) string {
	if write == nil {
		return ""
	}
	var b strings.Builder
	_, _ = b.WriteString("tags:")
	_, _ = b.WriteString(strconv.Itoa(len(write.TitleTags)))
	for _, tag := range write.TitleTags {
		appendKeyPart(&b, tag.Type)
		appendKeyPart(&b, tag.Tag)
	}
	_, _ = b.WriteString("props:")
	_, _ = b.WriteString(strconv.Itoa(len(write.TitleProps)))
	for _, prop := range write.TitleProps {
		appendKeyPart(&b, prop.TypeTag)
		appendKeyPart(&b, prop.Text)
		appendKeyPart(&b, prop.ContentType)
		if prop.BlobDBID == nil {
			_, _ = b.WriteString("nil;")
			continue
		}
		appendKeyPart(&b, strconv.FormatInt(*prop.BlobDBID, 10))
	}
	return b.String()
}

func appendKeyPart(b *strings.Builder, value string) {
	_, _ = b.WriteString(strconv.Itoa(len(value)))
	_ = b.WriteByte(':')
	_, _ = b.WriteString(value)
	_ = b.WriteByte(';')
}

func applyCompanionWriteTargets(
	ctx context.Context,
	mdb database.MediaDBI,
	targets []database.ScrapeWriteTarget,
	writeStats *scrapeWriteStats,
) (succeeded, failed int) {
	if len(targets) == 0 {
		return 0, 0
	}
	if batcher, ok := mdb.(database.ScrapeResultBatchApplier); ok {
		writeStart := time.Now()
		err := batcher.ApplyScrapeResults(ctx, targets)
		if err == nil {
			writeStats.recordBatch(targets, time.Since(writeStart))
			return len(targets), 0
		}
		writeStats.BatchFallbacks++
		log.Warn().Err(err).
			Int("targets", len(targets)).
			Msg("gamelistxml: companion: batch write failed, falling back to per-target writes")
	}

	for _, target := range targets {
		writeStart := time.Now()
		err := mdb.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write)
		writeStats.recordWrite(target, time.Since(writeStart))
		if err != nil {
			log.Warn().Err(err).
				Int64("mediaDBID", target.MediaDBID).
				Int64("mediaTitleDBID", target.MediaTitleDBID).
				Msg("gamelistxml: companion: write failed")
			failed++
			continue
		}
		succeeded++
	}
	return succeeded, failed
}

func logScrapeWriteStats(message, systemID string, stats *scrapeWriteStats) {
	log.Info().
		Str("system", systemID).
		Int("writes", stats.Writes).
		Int("batches", stats.Batches).
		Int("batch_fallbacks", stats.BatchFallbacks).
		Dur("total_write_duration", stats.TotalDuration).
		Dur("avg_write_duration", stats.averageDuration()).
		Dur("max_write_duration", stats.MaxDuration).
		Int("media_tag_writes", stats.MediaTags).
		Int("title_tag_writes", stats.TitleTags).
		Int("media_property_writes", stats.MediaProps).
		Int("title_property_writes", stats.TitleProps).
		Int("sentinel_writes", stats.Sentinels).
		Int("unique_title_writes", len(stats.UniqueTitleDBIDs)).
		Int("duplicate_title_writes", stats.Writes-len(stats.UniqueTitleDBIDs)).
		Msg(message)
}

func matchCompanionChildMedia(
	system scraper.ScrapeSystem,
	child companionChild,
	indexes loadRecordIndexes,
	stats *companionStats,
) companionMediaMatch {
	filename := filepath.Base(child.ResolvedPath)
	if strings.EqualFold(filepath.Ext(filename), ".slug") {
		slug := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
		title, ok := indexes.AllTitlesBySlug[slug]
		if !ok {
			title, ok = indexes.TitlesBySlug[slug]
		}
		if !ok {
			if stats != nil {
				stats.MissingTitleSlugs++
			}
			return companionMediaMatch{}
		}
		mediaRows := indexes.MediaByTitleDBID[title.DBID]
		if len(mediaRows) == 0 {
			if stats != nil {
				stats.MissingTitleMedia++
			}
			return companionMediaMatch{}
		}
		return companionMediaMatch{Media: cloneMediaRows(mediaRows)}
	}

	if media, _, ok := matchMediaByResolvedPath(indexes.MediaByPathFold, child.ResolvedPath); ok {
		return companionMediaMatch{Media: []database.Media{media}, MediaLevelWriteSafe: true}
	}

	filenameKey := mediaFilenameKey(child.ResolvedPath)
	matched := indexes.MediaByFilename[filenameKey]
	if len(matched) == 0 {
		if stats != nil {
			stats.UnmatchedFilenames++
		}
		return companionMediaMatch{}
	}
	if len(matched) > 1 {
		if stats != nil {
			stats.AmbiguousFilenames++
		}
		log.Warn().
			Str("system", system.ID).
			Str("path", child.ResolvedPath).
			Str("filename", filename).
			Str("parentGameID", child.ParentGameID).
			Int("matches", len(matched)).
			Msg("gamelistxml: companion: ambiguous child filename matches, skipping")
		return companionMediaMatch{}
	}
	return companionMediaMatch{Media: cloneMediaRows(matched), MediaLevelWriteSafe: true}
}

func cloneMediaRows(rows []database.Media) []database.Media {
	return append([]database.Media(nil), rows...)
}

func consumeCompanionMediaMatches(indexes loadRecordIndexes, mediaRows []database.Media) {
	for _, media := range mediaRows {
		delete(indexes.MediaByPathFold, pathFoldKey(media.Path))
		filenameKey := mediaFilenameKey(media.Path)
		if filenameKey != "" {
			indexes.MediaByFilename[filenameKey] = removeMediaByDBID(indexes.MediaByFilename[filenameKey], media.DBID)
		}
		indexes.MediaByTitleDBID[media.MediaTitleDBID] = removeMediaByDBID(
			indexes.MediaByTitleDBID[media.MediaTitleDBID], media.DBID,
		)
	}
}

func removeMediaByDBID(rows []database.Media, dbid int64) []database.Media {
	for i, row := range rows {
		if row.DBID != dbid {
			continue
		}
		copy(rows[i:], rows[i+1:])
		var zero database.Media
		rows[len(rows)-1] = zero
		return rows[:len(rows)-1]
	}
	return rows
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
