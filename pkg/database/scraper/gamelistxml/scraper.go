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
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/rs/zerolog/log"
)

// GamelistRecord is one entry from a gamelist.xml, bundled with the filesystem
// root path that the relative ES paths should be resolved against.
type GamelistRecord struct {
	AvailableMediaDirs map[string]string
	SystemRootPath     string
	Game               esapi.Game
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

// SystemResolver resolves ScrapeSystem values for the active system IDs.
// The API layer injects this so the scraper can retrieve ROMPaths without
// importing the config package.
type SystemResolver func(ctx context.Context, systemIDs []string) ([]scraper.ScrapeSystem, error)

// GamelistXMLScraper enriches Zaparoo MediaDB records from EmulationStation
// gamelist.xml files found in each system's ROM root paths.
type GamelistXMLScraper struct {
	db               database.MediaDBI
	resolveSystemsFn SystemResolver
}

// NewGamelistXMLScraper creates a new GamelistXMLScraper.
// db is used for all database operations; resolveSystemsFn provides system info
// (DBID, ROMPaths) when Scrape is called.
func NewGamelistXMLScraper(db database.MediaDBI, resolveSystemsFn SystemResolver) *GamelistXMLScraper {
	return &GamelistXMLScraper{db: db, resolveSystemsFn: resolveSystemsFn}
}

// ID returns the stable scraper identifier.
func (*GamelistXMLScraper) ID() string {
	return "gamelist.xml"
}

// Name returns the human-readable display name for this scraper.
func (*GamelistXMLScraper) Name() string {
	return "ES gamelist.xml"
}

// SupportedSystems returns an empty slice, meaning this scraper supports all systems.
func (*GamelistXMLScraper) SupportedSystems() []string {
	return []string{}
}

// Scrape implements [scraper.Scraper]. It resolves active systems via the
// injected resolver and delegates to [scraper.RunScraper].
func (g *GamelistXMLScraper) Scrape(
	ctx context.Context, opts scraper.ScrapeOptions,
) (<-chan scraper.ScrapeUpdate, error) {
	systems, err := g.resolveSystemsFn(ctx, opts.Systems)
	if err != nil {
		return nil, fmt.Errorf("gamelistxml: failed to resolve systems: %w", err)
	}
	return scraper.RunScraper[*GamelistRecord](ctx, opts, systems, g.db, g), nil
}

// LoadRecords searches each of system.ROMPaths for a gamelist.xml and yields
// one GamelistRecord per <game> entry found.
func (*GamelistXMLScraper) LoadRecords(
	ctx context.Context,
	system scraper.ScrapeSystem,
) ([]*GamelistRecord, error) {
	var records []*GamelistRecord
	for _, rootPath := range system.ROMPaths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		gamelistPath := filepath.Join(rootPath, "gamelist.xml")
		if _, statErr := os.Stat(gamelistPath); os.IsNotExist(statErr) {
			continue
		}

		gl, err := esapi.ReadGameListXML(gamelistPath)
		if err != nil {
			log.Warn().Err(err).Str("path", gamelistPath).Msg("gamelistxml: failed to read gamelist.xml, skipping")
			continue
		}

		log.Info().
			Str("path", gamelistPath).
			Int("entries", len(gl.Games)).
			Msg("gamelistxml: loaded gamelist.xml")

		// Stat media sub-directories once per root to avoid repeated lookups
		// in MapToDB when falling back to filesystem-based image discovery.
		availableMediaDirs := statMediaDirs(rootPath)

		for i := range gl.Games {
			records = append(records, &GamelistRecord{
				SystemRootPath:     rootPath,
				AvailableMediaDirs: availableMediaDirs,
				Game:               gl.Games[i],
			})
		}
	}

	log.Info().
		Str("system", system.ID).
		Int("total_records", len(records)).
		Msg("gamelistxml: finished loading records for system")

	return records, nil
}

// Match resolves the game path from the record to an absolute filesystem path,
// then looks up the corresponding Media row in the DB. Returns nil when the path
// cannot be resolved or the Media row does not exist.
func (*GamelistXMLScraper) Match(
	ctx context.Context,
	record *GamelistRecord,
	system scraper.ScrapeSystem,
	db database.MediaDBI,
) (*scraper.MatchResult, error) {
	resolved := resolveESPath(record.Game.Path, record.SystemRootPath)
	if resolved == "" {
		log.Info().
			Str("path", record.Game.Path).
			Str("root", record.SystemRootPath).
			Msg("gamelistxml: unresolvable path, skipping")
		return nil, nil //nolint:nilnil // unresolvable path means no match; nil result is the "skip" sentinel
	}
	absPath := filepath.ToSlash(filepath.Clean(resolved))

	media, err := db.FindMediaBySystemAndPathFold(ctx, system.DBID, absPath)
	if err != nil {
		return nil, fmt.Errorf("gamelistxml: FindMediaBySystemAndPathFold: %w", err)
	}
	if media == nil || media.DBID == 0 {
		log.Info().Str("path", absPath).Int64("systemDBID", system.DBID).Msg("gamelistxml: media not indexed, skipping")
		return nil, nil //nolint:nilnil // media not indexed; nil result is the "skip" sentinel
	}

	log.Debug().
		Str("path", absPath).
		Int64("mediaDBID", media.DBID).
		Msg("gamelistxml: matched media record")

	return &scraper.MatchResult{
		MediaDBID:      media.DBID,
		MediaTitleDBID: media.MediaTitleDBID,
	}, nil
}

// MapToDB converts a GamelistRecord into the tag and property writes to apply
// to the matched Media and MediaTitle rows.
func (*GamelistXMLScraper) MapToDB(record *GamelistRecord) scraper.MapResult {
	var mediaTags []database.TagInfo
	var titleTags []database.TagInfo
	var titleProps []database.MediaProperty
	var mediaProps []database.MediaProperty
	game := record.Game

	// Normalise all string fields before mapping: unescape HTML entities,
	// collapse control whitespace to spaces, and trim surrounding whitespace.
	game.Lang = cleanField(game.Lang)
	game.Region = cleanField(game.Region)
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

	// --- MediaTags: ROM-level, variant-specific ---

	// Lang: split comma-separated values; each becomes a separate lang: tag.
	if game.Lang != "" {
		for _, lang := range splitCSV(game.Lang) {
			if lang != "" {
				mediaTags = append(mediaTags, database.TagInfo{
					Type: string(tags.TagTypeLang), Tag: strings.ToLower(lang),
				})
			}
		}
	}

	// Region: split comma-separated values.
	if game.Region != "" {
		for _, region := range splitCSV(game.Region) {
			if region != "" {
				mediaTags = append(mediaTags, database.TagInfo{
					Type: string(tags.TagTypeRegion), Tag: strings.ToLower(region),
				})
			}
		}
	}

	// favorite/hidden/kidgame: user-state — not scraped.
	// disc/track: owned by filename parser — not overwritten here.

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
			p = findMediaFileProp(key, stem, mediaDirCandidates[string(propValue)], record.AvailableMediaDirs)
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

	// mediaProps: gamelist.xml scraper writes no ROM-level properties.
	return scraper.MapResult{
		MediaTags:  mediaTags,
		TitleTags:  titleTags,
		TitleProps: titleProps,
		MediaProps: mediaProps,
	}
}

// resolveESPath converts an EmulationStation path to an absolute filesystem path.
//
//   - "./relative" or "relative" → filepath.Join(systemRootPath, rest), confined to systemRootPath
//   - "~/..." → filepath.Join(os.UserHomeDir(), rest)
//   - Already absolute → returned as-is
//
// Returns "" if the result is not an absolute path, input is empty, or a relative
// path escapes systemRootPath via ".." components.
func resolveESPath(esPath, systemRootPath string) string {
	if esPath == "" {
		return ""
	}

	var abs string
	switch {
	case strings.HasPrefix(esPath, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		abs = filepath.Join(home, esPath[2:])
	case filepath.IsAbs(esPath):
		abs = filepath.Clean(esPath)
	default:
		// Handles both "./relative" and "relative".
		rel := strings.TrimPrefix(esPath, "./")
		abs = filepath.Join(systemRootPath, rel)
		// Containment check: relative inputs must stay within systemRootPath.
		root := filepath.Clean(systemRootPath) + string(filepath.Separator)
		if !strings.HasPrefix(abs+string(filepath.Separator), root) {
			return ""
		}
	}

	if !filepath.IsAbs(abs) {
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
	mediaRoot := filepath.Join(rootPath, "media")
	entries, err := os.ReadDir(mediaRoot)
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
	for _, dir := range candidates {
		dirPath, ok := availableDirs[dir]
		if !ok {
			continue
		}
		candidate := filepath.Join(dirPath, stem+".png")
		if _, err := os.Stat(candidate); err == nil {
			return &database.MediaProperty{
				TypeTag:     typeTag,
				Text:        filepath.ToSlash(candidate),
				ContentType: "image/png",
			}
		}
	}
	return nil
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
