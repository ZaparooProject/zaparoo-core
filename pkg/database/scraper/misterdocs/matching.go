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
	// mediaByTrailingTag indexes media on the parenthesised tag their name ends
	// with, so a ROM pack that prefixes an identifier with a title of its own
	// invention still resolves: "Shock Troopers (set 1) (shocktro)".
	mediaByTrailingTag map[string][]database.MediaWithFullPath
	// mediaBySetName indexes arcade media on the setname inside their MRA. It
	// is populated only for systems with an arcade artwork source, because
	// filling it means reading every MRA.
	mediaBySetName map[string][]database.MediaWithFullPath
}

type pendingWrite struct {
	mediaTags map[string]database.TagInfo
	titleTags map[string]database.TagInfo
	mediaProp map[string]database.MediaProperty
	titleProp map[string]database.MediaProperty
	mediaID   int64
	titleID   int64
	// records counts the pack records this write resolves, so write progress
	// can be reported in the same unit the step's totals use.
	records int
}

type matchStats struct {
	Processed int
	Matched   int
	Skipped   int
}

func newSystemIndex(titles []database.TitleWithSystem, media []database.MediaWithFullPath) systemIndex {
	idx := systemIndex{
		titlesBySlug:       make(map[string][]database.TitleWithSystem, len(titles)),
		titlesByID:         make(map[int64]database.TitleWithSystem, len(titles)),
		mediaByBase:        make(map[string][]database.MediaWithFullPath, len(media)),
		mediaByTitle:       make(map[int64][]database.MediaWithFullPath, len(titles)),
		mediaByTrailingTag: make(map[string][]database.MediaWithFullPath),
		mediaBySetName:     make(map[string][]database.MediaWithFullPath),
	}
	for _, title := range titles {
		idx.titlesBySlug[title.Slug] = append(idx.titlesBySlug[title.Slug], title)
		idx.titlesByID[title.DBID] = title
	}
	for _, item := range media {
		base := normalizedMediaBase(item.Path)
		if base != "" {
			idx.mediaByBase[base] = append(idx.mediaByBase[base], item)
			if tag := trailingParenTag(base); tag != "" {
				idx.mediaByTrailingTag[tag] = append(idx.mediaByTrailingTag[tag], item)
			}
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

// matchResult is what one step's matching produced: the write targets, how
// many pack records each target resolves (index-aligned, for progress in the
// same unit as the totals), the match counters, and every image path a
// record referenced, which force runs use to recognise stale properties.
type matchResult struct {
	Found            map[string]struct{}
	Targets          []database.ScrapeWriteTarget
	RecordsPerTarget []int
	Stats            matchStats
}

// buildPendingWrites resolves every record to a write target.
func buildPendingWrites(
	idx systemIndex,
	records []sourceRecords,
	runID string,
) matchResult {
	pending := make(map[int64]*pendingWrite)
	foundPaths := make(map[string]struct{})
	stats := matchStats{}
	for _, source := range records {
		for _, record := range source.Artwork {
			stats.Processed++
			if record.ImagePath != "" {
				foundPaths[filepath.Clean(record.ImagePath)] = struct{}{}
			}
			if applyArtworkRecord(idx, pending, source, record) {
				stats.Matched++
			} else {
				stats.Skipped++
			}
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
			write.records++
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
	recordsPerTarget := make([]int, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		p := pending[mediaID]
		recordsPerTarget = append(recordsPerTarget, p.records)
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
	return matchResult{Targets: targets, RecordsPerTarget: recordsPerTarget, Stats: stats, Found: foundPaths}
}

// applyArtworkRecord resolves one pack record to a media row and stages its
// image and metadata. A record with no image still carries title metadata, for
// games the pack catalogues without artwork.
func applyArtworkRecord(
	idx systemIndex,
	pending map[int64]*pendingWrite,
	source sourceRecords,
	record artworkRecord,
) bool {
	media, title, exact := matchArtwork(idx, record)
	if media == nil || title == nil {
		return false
	}
	write := getPending(pending, media.DBID, title.DBID)
	if record.ImagePath != "" {
		prop := database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
			Text:    filepath.ToSlash(record.ImagePath),
		}
		props := write.titleProp
		if exact {
			props = write.mediaProp
		}
		if _, exists := props[prop.TypeTag]; exists {
			return false
		}
		props[prop.TypeTag] = prop
	}
	applyGameMetadata(write, source, record.Key)
	write.records++
	return true
}

// matchArtwork resolves a pack record to installed media, cheapest step first
// and stopping at the first hit: the catalogued name as a filename, the arcade
// setname inside an MRA, the name's trailing tag when it is itself a pack key,
// and finally the bare title.
func matchArtwork(
	idx systemIndex,
	record artworkRecord,
) (*database.MediaWithFullPath, *database.TitleWithSystem, bool) {
	name := strings.ToLower(strings.TrimSpace(record.Name))
	if media, title := uniqueMedia(idx, idx.mediaByBase[name], record.Name); media != nil {
		return media, title, true
	}
	if media, title := uniqueMedia(idx, idx.mediaBySetName[name], record.Name); media != nil {
		return media, title, true
	}
	key := strings.ToLower(strings.TrimSpace(record.Key))
	if media, title := uniqueMedia(idx, idx.mediaByTrailingTag[key], record.Name); media != nil {
		return media, title, true
	}

	if !record.SlugUnique {
		return nil, nil, false
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

// uniqueMedia picks the single media a candidate list points at. When several
// share a name, only a candidate whose title also matches the record resolves;
// anything still ambiguous is left for a later step.
func uniqueMedia(
	idx systemIndex,
	candidates []database.MediaWithFullPath,
	recordName string,
) (*database.MediaWithFullPath, *database.TitleWithSystem) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) == 1 {
		media := candidates[0]
		if title := titleByID(idx, media.MediaTitleDBID); title != nil {
			return &media, title
		}
		return nil, nil
	}
	slug := slugs.Slugify(slugs.MediaTypeGame, recordName)
	var selected *database.MediaWithFullPath
	for i := range candidates {
		title := titleByID(idx, candidates[i].MediaTitleDBID)
		if title == nil || title.Slug != slug {
			continue
		}
		if selected != nil {
			return nil, nil
		}
		candidate := candidates[i]
		selected = &candidate
	}
	if selected == nil {
		return nil, nil
	}
	return selected, titleByID(idx, selected.MediaTitleDBID)
}

// trailingParenTag returns the content of the parenthesised tag a name ends
// with. Matching on it is only safe against an existing pack key, so callers
// look the result up rather than trusting it.
func trailingParenTag(base string) string {
	trimmed := strings.TrimSpace(base)
	if !strings.HasSuffix(trimmed, ")") {
		return ""
	}
	open := strings.LastIndex(trimmed, "(")
	if open < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(trimmed[open+1 : len(trimmed)-1]))
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

// applyGameMetadata stages title metadata for a key. The first record to
// reach a title wins each field: index rows are processed before the records
// synthesised from images and gameinfo, so the representative dump's details
// are not replaced by a demo or regional variant that resolves to the same
// title later.
func applyGameMetadata(write *pendingWrite, source sourceRecords, key string) {
	info, ok := source.GameInfo[key]
	if ok {
		if year := normalizedYear(info.Year); year != "" {
			setTitleTag(write, database.TagInfo{Type: string(tags.TagTypeYear), Tag: year})
		}
		appendNormalizedTitleTag(write, tags.TagTypeGenre, info.Genre)
		appendNormalizedTitleTag(write, tags.TagTypeDeveloper, info.Developer)
		if players := normalizePlayers(info.Players); players != "" {
			setTitleTag(write, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: players})
		}
	}
	synopsis := cleanText(source.Synopsis[key])
	if synopsis == "" {
		return
	}
	description := tags.PropertyTypeTag(tags.TagPropertyDescription)
	if _, exists := write.titleProp[description]; !exists {
		write.titleProp[description] = database.MediaProperty{TypeTag: description, Text: synopsis}
	}
}

func setTitleTag(write *pendingWrite, tag database.TagInfo) {
	if _, exists := write.titleTags[tag.Type]; !exists {
		write.titleTags[tag.Type] = tag
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
	setTitleTag(write, database.TagInfo{Type: string(tagType), Tag: normalized, Label: raw})
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
