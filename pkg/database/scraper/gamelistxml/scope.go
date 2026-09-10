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

package gamelistxml

import (
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
)

func (g *GamelistXMLScraper) scrapeScoped(
	ctx context.Context, opts scraper.ScrapeOptions, systems []scraper.ScrapeSystem,
	mdb database.MediaDBI, ch chan<- scraper.ScrapeUpdate,
) {
	selection, err := scraper.LoadScopedSelection(ctx, mdb, opts, "gamelist.xml")
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
		return
	}
	ch <- scraper.ScrapeUpdate{
		SystemID: opts.Scope.SystemID, Total: len(selection.Media), TotalSteps: 1, CurrentStep: 1,
	}
	if pending, _ := selection.Pending(); len(pending) == 0 {
		scraper.ApplyScopedTargets(ctx, mdb, opts, selection, nil, ch)
		return
	}
	var targets []database.ScrapeWriteTarget
	for _, system := range systems {
		if err := opts.Pauser.Wait(ctx); err != nil {
			ch <- scraper.ScrapeUpdate{Done: true}
			return
		}
		if system.ID != opts.Scope.SystemID || len(selection.Media) == 0 {
			continue
		}
		indexes, err := scopedRecordIndexes(ctx, mdb, system.ID, selection)
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}
		parsed, err := g.loadParsedGamelistSystem(ctx, system)
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}
		targets = append(targets, g.scopedCompanionTargets(ctx, opts, system, indexes, parsed)...)
		records, err := g.loadRecordsFromParsed(ctx, system, indexes, parsed)
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}
		for _, record := range records {
			if err := opts.Pauser.Wait(ctx); err != nil {
				ch <- scraper.ScrapeUpdate{Done: true}
				return
			}
			mapped := g.MapToDB(record)
			write := &database.ScrapeWrite{
				Sentinel: scraper.SentinelTagInfo("gamelist.xml"), MediaTags: mapped.MediaTags,
				MediaProps: mapped.MediaProps, TitleTags: mapped.TitleTags, TitleProps: mapped.TitleProps,
			}
			appendRunMarker("gamelist.xml", opts, write)
			targets = append(targets, database.ScrapeWriteTarget{
				MediaDBID: record.MatchedMediaDBID, MediaTitleDBID: record.MatchedTitleDBID, Write: write,
			})
		}
	}
	scraper.ApplyScopedTargets(ctx, mdb, opts, selection, targets, ch)
}

func scopedRecordIndexes(
	ctx context.Context, mdb database.MediaDBI, systemID string, selection scraper.ScopedSelection,
) (loadRecordIndexes, error) {
	indexes := loadRecordIndexes{
		Scoped: true, TitlesBySlug: make(map[string]database.MediaTitle),
		AllTitlesBySlug: make(map[string]database.MediaTitle),
		MediaByPathFold: make(map[string]database.Media), MediaByTitleDBID: make(map[int64][]database.Media),
		MediaByFilename: make(map[string][]database.Media),
	}
	for _, title := range selection.Titles {
		row := database.MediaTitle{DBID: title.DBID, SystemDBID: title.SystemDBID, Slug: title.Slug, Name: title.Name}
		indexes.TitlesBySlug[title.Slug] = row
		indexes.AllTitlesBySlug[title.Slug] = row
	}
	for _, media := range selection.Media {
		if _, completed := selection.Completed[media.DBID]; completed {
			continue
		}
		row := database.Media{
			DBID: media.DBID, MediaTitleDBID: media.MediaTitleDBID, Path: media.Path, ParentDir: media.ParentDir,
		}
		indexes.MediaByPathFold[pathFoldKey(media.Path)] = row
		indexes.MediaByTitleDBID[media.MediaTitleDBID] = append(indexes.MediaByTitleDBID[media.MediaTitleDBID], row)
		key := mediaFilenameKey(media.Path)
		indexes.MediaByFilename[key] = append(indexes.MediaByFilename[key], row)
	}
	var err error
	indexes.Containers, err = scraper.ScopedContainers(ctx, mdb, systemID, selection.Media)
	if err != nil {
		return indexes, fmt.Errorf("build scoped container context: %w", err)
	}
	return indexes, nil
}

func (g *GamelistXMLScraper) scopedCompanionTargets(
	ctx context.Context, opts scraper.ScrapeOptions, system scraper.ScrapeSystem,
	indexes loadRecordIndexes, parsed parsedGamelistSystem,
) []database.ScrapeWriteTarget {
	parents, children := companionEntriesFromParsed(ctx, system, parsed)
	children = resolveCompanionSlugConflicts(system.ID, parents, children, &companionStats{})
	parentMeta := make(map[string]scraper.MapResult)
	parentRows := make(map[string]*companionParent)
	for i := range parents {
		parentRows[parents[i].GameID] = &parents[i]
	}
	var targets []database.ScrapeWriteTarget
	for _, child := range children {
		if err := opts.Pauser.Wait(ctx); err != nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
		parent, ok := parentRows[child.ParentGameID]
		if !ok {
			continue
		}
		matched := matchCompanionChildMedia(system, child, indexes, nil)
		if len(matched.Media) == 0 {
			continue
		}
		meta, ok := parentMeta[child.ParentGameID]
		if !ok {
			meta = g.mapCompanionParentToResult(parent)
			parentMeta[child.ParentGameID] = meta
		}
		consumeCompanionMediaMatches(indexes, matched.Media)
		for _, media := range matched.Media {
			write := &database.ScrapeWrite{
				Sentinel:  scraper.SentinelTagInfo("gamelist.xml"),
				TitleTags: meta.TitleTags, TitleProps: meta.TitleProps,
			}
			if matched.MediaLevelWriteSafe {
				write.MediaTags = companionChildTags(child)
			}
			appendRunMarker("gamelist.xml", opts, write)
			targets = append(targets, database.ScrapeWriteTarget{
				MediaDBID: media.DBID, MediaTitleDBID: media.MediaTitleDBID, Write: write,
			})
		}
	}
	return targets
}
