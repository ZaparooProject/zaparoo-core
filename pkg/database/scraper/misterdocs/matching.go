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

package misterdocs

import (
	"html"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

type systemIndex struct {
	titlesBySlug map[string][]database.TitleWithSystem
	titlesByID   map[int64]database.TitleWithSystem
	mediaByBase  map[string][]database.MediaWithFullPath
	mediaByTitle map[int64][]database.MediaWithFullPath
}

type pendingWrite struct {
	mediaTags map[string]database.TagInfo
	titleTags map[string]database.TagInfo
	mediaProp map[string]database.MediaProperty
	titleProp map[string]database.MediaProperty
	mediaID   int64
	titleID   int64
}

type matchStats struct {
	Processed int
	Matched   int
	Skipped   int
}

func newSystemIndex(titles []database.TitleWithSystem, media []database.MediaWithFullPath) systemIndex {
	idx := systemIndex{
		titlesBySlug: make(map[string][]database.TitleWithSystem, len(titles)),
		titlesByID:   make(map[int64]database.TitleWithSystem, len(titles)),
		mediaByBase:  make(map[string][]database.MediaWithFullPath, len(media)),
		mediaByTitle: make(map[int64][]database.MediaWithFullPath, len(titles)),
	}
	for _, title := range titles {
		idx.titlesBySlug[title.Slug] = append(idx.titlesBySlug[title.Slug], title)
		idx.titlesByID[title.DBID] = title
	}
	for _, item := range media {
		base := normalizedMediaBase(item.Path)
		if base != "" {
			idx.mediaByBase[base] = append(idx.mediaByBase[base], item)
		}
		idx.mediaByTitle[item.MediaTitleDBID] = append(idx.mediaByTitle[item.MediaTitleDBID], item)
	}
	for titleID := range idx.mediaByTitle {
		sort.Slice(idx.mediaByTitle[titleID], func(i, j int) bool {
			a, b := idx.mediaByTitle[titleID][i], idx.mediaByTitle[titleID][j]
			if a.IsMissing != b.IsMissing {
				return !a.IsMissing
			}
			return a.DBID < b.DBID
		})
	}
	return idx
}

func buildPendingWrites(
	idx systemIndex,
	records []sourceRecords,
	runID string,
) ([]database.ScrapeWriteTarget, matchStats, map[string]struct{}) {
	pending := make(map[int64]*pendingWrite)
	foundPaths := make(map[string]struct{})
	stats := matchStats{}
	for _, source := range records {
		for _, record := range source.Artwork {
			stats.Processed++
			foundPaths[filepath.Clean(record.ImagePath)] = struct{}{}
			media, title, exact := matchArtwork(idx, record)
			if media == nil || title == nil {
				stats.Skipped++
				continue
			}
			write := getPending(pending, media.DBID, title.DBID)
			prop := database.MediaProperty{
				TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
				Text:    filepath.ToSlash(record.ImagePath),
			}
			if exact {
				write.mediaProp[prop.TypeTag] = prop
			} else {
				write.titleProp[prop.TypeTag] = prop
			}
			applyGameMetadata(write, source, record.Key)
			stats.Matched++
		}
		for _, manualPath := range source.Manuals {
			stats.Processed++
			foundPaths[filepath.Clean(manualPath)] = struct{}{}
			title := matchManualTitle(idx, manualPath)
			if title == nil {
				stats.Skipped++
				continue
			}
			media := firstMediaForTitle(idx, title.DBID)
			if media == nil {
				stats.Skipped++
				continue
			}
			write := getPending(pending, media.DBID, title.DBID)
			prop := database.MediaProperty{
				TypeTag: tags.PropertyTypeTag(tags.TagPropertyManual),
				Text:    filepath.ToSlash(manualPath),
			}
			if _, exists := write.titleProp[prop.TypeTag]; exists {
				stats.Skipped++
				continue
			}
			write.titleProp[prop.TypeTag] = prop
			stats.Matched++
		}
		stats.Skipped += source.RowErrors
		stats.Processed += source.RowErrors
	}

	mediaIDs := make([]int64, 0, len(pending))
	for mediaID := range pending {
		mediaIDs = append(mediaIDs, mediaID)
	}
	sort.Slice(mediaIDs, func(i, j int) bool { return mediaIDs[i] < mediaIDs[j] })
	targets := make([]database.ScrapeWriteTarget, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		p := pending[mediaID]
		write := &database.ScrapeWrite{
			Sentinel:   scraper.SentinelTagInfo(scraperID),
			MediaTags:  sortedTags(p.mediaTags),
			TitleTags:  sortedTags(p.titleTags),
			MediaProps: sortedProps(p.mediaProp),
			TitleProps: sortedProps(p.titleProp),
		}
		if runID != "" {
			write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(scraperID, runID))
		}
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: p.mediaID, MediaTitleDBID: p.titleID, Write: write,
		})
	}
	return targets, stats, foundPaths
}

func matchArtwork(
	idx systemIndex,
	record artworkRecord,
) (*database.MediaWithFullPath, *database.TitleWithSystem, bool) {
	base := strings.ToLower(strings.TrimSpace(record.Name))
	if candidates := idx.mediaByBase[base]; len(candidates) == 1 {
		media := candidates[0]
		if title := titleByID(idx, media.MediaTitleDBID); title != nil {
			return &media, title, true
		}
	} else if len(candidates) > 1 {
		slug := slugs.Slugify(slugs.MediaTypeGame, record.Name)
		var selected *database.MediaWithFullPath
		for i := range candidates {
			title := titleByID(idx, candidates[i].MediaTitleDBID)
			if title == nil || title.Slug != slug {
				continue
			}
			if selected != nil {
				selected = nil
				break
			}
			candidate := candidates[i]
			selected = &candidate
		}
		if selected != nil {
			return selected, titleByID(idx, selected.MediaTitleDBID), true
		}
	}

	slug := slugs.Slugify(slugs.MediaTypeGame, record.Name)
	titles := idx.titlesBySlug[slug]
	if len(titles) != 1 {
		return nil, nil, false
	}
	media := firstMediaForTitle(idx, titles[0].DBID)
	if media == nil {
		return nil, nil, false
	}
	title := titles[0]
	return media, &title, false
}

func matchManualTitle(idx systemIndex, path string) *database.TitleWithSystem {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	slug := slugs.Slugify(slugs.MediaTypeGame, name)
	if slug == "" {
		return nil
	}
	titles := idx.titlesBySlug[slug]
	if len(titles) == 1 {
		title := titles[0]
		return &title
	}
	if len(titles) < 2 {
		return nil
	}
	normalizedName := normalizeTitleName(name)
	var selected *database.TitleWithSystem
	for i := range titles {
		if normalizeTitleName(titles[i].Name) != normalizedName {
			continue
		}
		if selected != nil {
			return nil
		}
		title := titles[i]
		selected = &title
	}
	return selected
}

func applyGameMetadata(write *pendingWrite, source sourceRecords, key string) {
	info, ok := source.GameInfo[key]
	if ok {
		if year := normalizedYear(info.Year); year != "" {
			write.titleTags[string(tags.TagTypeYear)] = database.TagInfo{Type: string(tags.TagTypeYear), Tag: year}
		}
		appendNormalizedTitleTag(write, tags.TagTypeGenre, info.Genre)
		appendNormalizedTitleTag(write, tags.TagTypeDeveloper, info.Developer)
		if players := normalizePlayers(info.Players); players != "" {
			write.titleTags[string(tags.TagTypePlayers)] = database.TagInfo{
				Type: string(tags.TagTypePlayers), Tag: players,
			}
		}
	}
	if synopsis := cleanText(source.Synopsis[key]); synopsis != "" {
		write.titleProp[tags.PropertyTypeTag(tags.TagPropertyDescription)] = database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyDescription), Text: synopsis,
		}
	}
}

func appendNormalizedTitleTag(write *pendingWrite, tagType tags.TagType, raw string) {
	raw = cleanText(raw)
	if raw == "" {
		return
	}
	normalized := tags.NormalizeTagValue(string(tagType), raw)
	if normalized == "" {
		return
	}
	write.titleTags[string(tagType)] = database.TagInfo{Type: string(tagType), Tag: normalized, Label: raw}
}

func normalizedYear(value string) string {
	value = strings.TrimSpace(value)
	for i := 0; i+4 <= len(value); i++ {
		candidate := value[i : i+4]
		valid := true
		for _, r := range candidate {
			if !unicode.IsDigit(r) {
				valid = false
				break
			}
		}
		if valid {
			return candidate
		}
	}
	return ""
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}

func normalizedMediaBase(path string) string {
	name := filepath.Base(filepath.ToSlash(path))
	return strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
}

func normalizeTitleName(value string) string {
	return strings.ToLower(cleanText(value))
}

func titleByID(idx systemIndex, titleID int64) *database.TitleWithSystem {
	title, ok := idx.titlesByID[titleID]
	if !ok {
		return nil
	}
	return &title
}

func firstMediaForTitle(idx systemIndex, titleID int64) *database.MediaWithFullPath {
	media := idx.mediaByTitle[titleID]
	if len(media) == 0 {
		return nil
	}
	item := media[0]
	return &item
}

func getPending(pending map[int64]*pendingWrite, mediaID, titleID int64) *pendingWrite {
	if existing := pending[mediaID]; existing != nil {
		return existing
	}
	result := &pendingWrite{
		mediaTags: make(map[string]database.TagInfo), titleTags: make(map[string]database.TagInfo),
		mediaProp: make(map[string]database.MediaProperty), titleProp: make(map[string]database.MediaProperty),
		mediaID: mediaID, titleID: titleID,
	}
	pending[mediaID] = result
	return result
}

func sortedTags(values map[string]database.TagInfo) []database.TagInfo {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]database.TagInfo, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func sortedProps(values map[string]database.MediaProperty) []database.MediaProperty {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]database.MediaProperty, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}
