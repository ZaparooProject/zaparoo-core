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
	string(tags.TagPropertyImageBoxart):     {"boxart", "boxart2d", "boxart3d", "boxart2dfront"},
	string(tags.TagPropertyImageScreenshot): {"screenshot", "screenshots"},
	string(tags.TagPropertyImageThumbnail):  {"thumbnail", "thumbnails", "supporttexture"},
	string(tags.TagPropertyImageMarquee):    {"marquee", "marquees"},
	string(tags.TagPropertyImageWheel):      {"wheel", "wheels"},
	string(tags.TagPropertyImageFanart):     {"fanart", "fanarts"},
	string(tags.TagPropertyImageTitleshot):  {"titleshot", "titleshots", "screenshottitle"},
	string(tags.TagPropertyImageMap):        {"map", "maps"},
}

// GamelistXMLScraper loads and maps EmulationStation gamelist.xml records.
// Use [NewPlatformScraper] to obtain a configured [platforms.Scraper].
type GamelistXMLScraper struct {
	db  database.MediaDBI
	fs  afero.Fs
	cfg *config.Instance
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
// the given system. For each game entry whose slug matches a key in
// titlesBySlug, a [GamelistRecord] is emitted and the slug is removed from
// titlesBySlug (first match wins across all gamelists for the system).
// titlesBySlug is mutated by this call — callers must not reuse it.
//
// mediaByTitleDBID supplies the Media DBID to use as the sentinel write target:
// it must map each MediaTitle DBID to any one of its associated Media DBIDs.
// A record whose title DBID has no entry in mediaByTitleDBID will have
// MatchedMediaDBID = 0 and will be skipped by the scrape loop.
func (g *GamelistXMLScraper) LoadRecords(
	ctx context.Context,
	system scraper.ScrapeSystem,
	titlesBySlug map[string]database.MediaTitle,
	mediaByTitleDBID map[int64]int64,
) ([]*GamelistRecord, error) {
	var records []*GamelistRecord

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

		log.Info().
			Str("path", gamelistPath).
			Int("entries", len(gl.Games)).
			Msg("gamelistxml: loaded gamelist.xml")

		availableMediaDirs := statMediaDirsFS(g.filesystem(), rootPath)

		for i := range gl.Games {
			resolved := resolveESPath(gl.Games[i].Path, rootPath)
			if resolved == "" {
				continue
			}

			pf := mediascanner.GetPathFragments(mediascanner.PathFragmentParams{
				Config:   g.cfg,
				Path:     resolved,
				SystemID: system.ID,
				NoExt:    true,
			})

			title, ok := titlesBySlug[pf.Slug]
			if !ok {
				continue
			}

			records = append(records, &GamelistRecord{
				SystemRootPath:     rootPath,
				AvailableMediaDirs: availableMediaDirs,
				Game:               gl.Games[i],
				MatchedTitleDBID:   title.DBID,
				MatchedMediaDBID:   mediaByTitleDBID[title.DBID],
			})
			delete(titlesBySlug, pf.Slug)
			if len(titlesBySlug) == 0 {
				break outer
			}
		}
	}

	log.Info().
		Str("system", system.ID).
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

		// Build slug → MediaTitle map: only unscraped titles unless Force.
		titlesBySlug := make(map[string]database.MediaTitle)
		if opts.Force {
			allTitles, titlesErr := mdb.GetTitlesBySystemID(system.ID)
			if titlesErr != nil {
				if errors.Is(titlesErr, context.Canceled) || errors.Is(titlesErr, context.DeadlineExceeded) {
					ch <- scraper.ScrapeUpdate{
						Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
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
						Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped,
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

		if len(titlesBySlug) == 0 {
			continue
		}

		// Build MediaTitle DBID → first Media DBID map for sentinel writes.
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
		mediaByTitleDBID := make(map[int64]int64, len(allMedia))
		for _, m := range allMedia {
			if _, exists := mediaByTitleDBID[m.MediaTitleDBID]; !exists {
				mediaByTitleDBID[m.MediaTitleDBID] = m.DBID
			}
		}

		records, loadErr := g.LoadRecords(ctx, system, titlesBySlug, mediaByTitleDBID)
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
			update.Total = totalRecords
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
// to the matched MediaTitle row. Media-level tags are not written by this
// scraper; only title-level tags and properties are populated.
func (g *GamelistXMLScraper) MapToDB(record *GamelistRecord) scraper.MapResult {
	var titleTags []database.TagInfo
	var titleProps []database.MediaProperty
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

	// --- MediaTitleProperties: title-level static content ---

	propType := string(tags.TagTypeProperty)
	root := record.SystemRootPath

	// stem is the ROM filename without extension, used to locate matching
	// artwork files under media/ sub-directories.
	stem := strings.TrimSuffix(filepath.Base(game.Path), filepath.Ext(game.Path))

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
				g.filesystem(), key, stem,
				mediaDirCandidates[string(propValue)], record.AvailableMediaDirs,
			)
		}
		if p != nil {
			titleProps = append(titleProps, *p)
		}
	}

	appendImageProp(tags.TagPropertyImageImage, game.Image)
	// image-boxart and image-screenshot have no ES XML fields; filesystem-only.
	appendImageProp(tags.TagPropertyImageBoxart, "")
	appendImageProp(tags.TagPropertyImageScreenshot, "")
	// game.Thumbnail in most ES forks (RPI, Sky, Batocera, ES-DE) holds cover art.
	// See esapi/gamelist.go for field-level fork documentation.
	appendImageProp(tags.TagPropertyImageThumbnail, game.Thumbnail)
	appendImageProp(tags.TagPropertyImageMarquee, game.Marquee)
	appendImageProp(tags.TagPropertyImageWheel, game.Wheel)
	appendImageProp(tags.TagPropertyImageFanart, game.FanArt)
	appendImageProp(tags.TagPropertyImageTitleshot, game.TitleShot)
	appendImageProp(tags.TagPropertyImageMap, game.Map)

	if p := pathProp(propType+":"+string(tags.TagPropertyVideo), game.Video, root); p != nil {
		titleProps = append(titleProps, *p)
	}
	if p := pathProp(propType+":"+string(tags.TagPropertyManual), game.Manual, root); p != nil {
		titleProps = append(titleProps, *p)
	}

	return scraper.MapResult{
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
	return findMediaFilePropFS(afero.NewOsFs(), typeTag, stem, candidates, availableDirs)
}

func findMediaFilePropFS(
	fs afero.Fs,
	typeTag, stem string,
	candidates []string,
	availableDirs map[string]string,
) *database.MediaProperty {
	if stem == "" || stem == "." {
		return nil
	}
	for _, dir := range candidates {
		dirPath, ok := availableDirs[dir]
		if !ok {
			continue
		}
		candidate := filepath.Join(dirPath, stem+".png")
		if exists, err := afero.Exists(fs, candidate); err == nil && exists {
			return &database.MediaProperty{
				TypeTag:     typeTag,
				Text:        filepath.ToSlash(candidate),
				ContentType: "image/png",
			}
		}
	}
	return nil
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
