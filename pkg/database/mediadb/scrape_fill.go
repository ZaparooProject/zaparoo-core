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

package mediadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// These helpers run inside the ordinary scrape transaction. Missing means no
// stored field, not an empty value or artwork whose file is temporarily absent.
func fillMissingScrapeTarget(ctx context.Context, c *scrapeWriteTxContext, target database.ScrapeWriteTarget) error {
	write := target.Write
	if err := fillMissingScrapeTags(ctx, c, target.MediaDBID, false, write.MediaTags); err != nil {
		return err
	}
	if err := fillMissingScrapeTags(ctx, c, target.MediaTitleDBID, true, write.TitleTags); err != nil {
		return err
	}
	if err := fillMissingScrapeProperties(ctx, c, target.MediaDBID, false, write.MediaProps); err != nil {
		return err
	}
	if err := fillMissingScrapeProperties(ctx, c, target.MediaTitleDBID, true, write.TitleProps); err != nil {
		return err
	}
	return upsertMediaTagsWithContext(ctx, c, target.MediaDBID, []database.TagInfo{write.Sentinel})
}

func fillMissingScrapeTags(
	ctx context.Context, c *scrapeWriteTxContext, id int64, title bool, values []database.TagInfo,
) error {
	table, column := "MediaTags", "MediaDBID"
	insertLink := insertScrapedMediaTagSQL
	if title {
		table, column = "MediaTitleTags", "MediaTitleDBID"
		insertLink = "INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)"
	}
	for _, tag := range values {
		typeID, exclusive, err := c.resolveTagType(ctx, tag.Type)
		if err != nil {
			return err
		}
		if exclusive {
			var exists bool
			err = c.tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+
				" m JOIN Tags t ON t.DBID=m.TagDBID WHERE m."+column+"=? AND t.TypeDBID=?)", id, typeID).Scan(&exists)
			if err != nil {
				return fmt.Errorf("check missing scrape tag: %w", err)
			}
			if exists {
				continue
			}
		}
		// Stored tag values are zero-padded on their numeric tail, the same as
		// every other scrape write path. Writing the natural spelling here would
		// add a second Tags row for one value that no padded query can reach.
		tagValue := tags.PadTagValue(tag.Tag)
		// Reusing a global tag must not replace its display label for other media.
		var tagID int64
		err = c.tx.QueryRowContext(ctx,
			"SELECT DBID FROM Tags WHERE TypeDBID=? AND Tag=?", typeID, tagValue).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID, err = c.resolveTag(ctx, typeID, tag.Type, tagValue, tag.Label)
		}
		if err != nil {
			return fmt.Errorf("resolve missing scrape tag: %w", err)
		}
		if _, err := c.tx.ExecContext(ctx, insertLink, id, tagID); err != nil {
			return fmt.Errorf("insert missing scrape tag: %w", err)
		}
	}
	return nil
}

func fillMissingScrapeProperties(
	ctx context.Context, c *scrapeWriteTxContext, id int64, title bool, values []database.MediaProperty,
) error {
	table, column := "MediaProperties", "MediaDBID"
	if title {
		table, column = "MediaTitleProperties", "MediaTitleDBID"
	}
	for _, property := range values {
		typeID, err := c.resolvePropertyTypeTag(ctx, property.TypeTag)
		if err != nil {
			return err
		}
		result, err := c.tx.ExecContext(ctx, "INSERT INTO "+table+" ("+column+", TypeTagDBID, Text, BlobDBID)"+
			" VALUES (?, ?, ?, ?) ON CONFLICT("+column+", TypeTagDBID) DO NOTHING",
			id, typeID, property.Text, property.BlobDBID)
		if err != nil {
			return fmt.Errorf("insert missing scrape property: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read inserted scrape properties: %w", err)
		}
		if changed > 0 && isImageProperty(property.TypeTag) {
			if title {
				c.changedImageMediaTitleIDs[id] = struct{}{}
			} else {
				c.changedImageMediaIDs[id] = struct{}{}
			}
		}
	}
	return nil
}

// applyFillMissingScrapeTargetsBulk applies a batch of fill-missing targets
// with multi-row statements. It stores exactly what fillMissingScrapeTarget
// stores for the same targets in the same order: nothing is replaced or
// deleted, and where two targets offer the same field the first one wins.
func applyFillMissingScrapeTargetsBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	targets []database.ScrapeWriteTarget,
	stats *scrapeBatchSQLStats,
) error {
	media, title := &scrapeMediaScope, &scrapeTitleScope
	if err := fillMissingScrapeTagsBulk(ctx, writeCtx, media, targets, stats, &stats.MediaTags); err != nil {
		return err
	}
	if err := fillMissingScrapeTagsBulk(ctx, writeCtx, title, targets, stats, &stats.TitleTags); err != nil {
		return err
	}
	if err := fillMissingScrapePropertiesBulk(ctx, writeCtx, media, targets, &stats.MediaProps); err != nil {
		return err
	}
	if err := fillMissingScrapePropertiesBulk(ctx, writeCtx, title, targets, &stats.TitleProps); err != nil {
		return err
	}
	if err := upsertScrapeSentinelsBulk(ctx, writeCtx, targets, &stats.Sentinels); err != nil {
		return fmt.Errorf("upsert sentinel tag: %w", err)
	}
	return nil
}

// fillTag is one tag a fill-missing batch offers to one row.
type fillTag struct {
	typeName  string
	value     string
	label     string
	key       scrapeTypeKey
	exclusive bool
}

func fillMissingScrapeTagsBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	targets []database.ScrapeWriteTarget,
	stats *scrapeBatchSQLStats,
	linkStats *scrapeLinkStats,
) error {
	var offered []fillTag
	exclusiveIDs := make(map[int64]struct{})
	exclusiveTypes := make(map[int64]struct{})
	for i := range targets {
		id := scope.id(&targets[i])
		for _, tag := range scope.tags(targets[i].Write) {
			typeDBID, exclusive, err := writeCtx.resolveTagType(ctx, tag.Type)
			if err != nil {
				return err
			}
			if exclusive {
				exclusiveIDs[id] = struct{}{}
				exclusiveTypes[typeDBID] = struct{}{}
			}
			offered = append(offered, fillTag{
				key: scrapeTypeKey{id: id, typeDBID: typeDBID}, exclusive: exclusive,
				typeName: tag.Type, value: tags.PadTagValue(tag.Tag), label: tag.Label,
			})
		}
	}
	if len(offered) == 0 {
		return nil
	}

	held, err := storedScrapeTagTypes(ctx, writeCtx.tx, scope, exclusiveIDs, exclusiveTypes, stats)
	if err != nil {
		return err
	}
	// An exclusive type a row already holds is left alone, and so is its
	// value: the tag is not even created. The first offer in input order
	// claims the type for the rest of the batch.
	wanted := offered[:0]
	for i := range offered {
		if offered[i].exclusive {
			if _, ok := held[offered[i].key]; ok {
				continue
			}
			held[offered[i].key] = struct{}{}
		}
		wanted = append(wanted, offered[i])
	}
	if err := resolveFillTags(ctx, writeCtx, wanted); err != nil {
		return err
	}

	links := make([]scrapeTagLink, 0, len(wanted))
	seen := make(map[scrapeTagLink]struct{}, len(wanted))
	for i := range wanted {
		tagDBID, ok := writeCtx.tags[tagCacheKey{typeDBID: wanted[i].key.typeDBID, tag: wanted[i].value}]
		if !ok {
			return fmt.Errorf("resolve missing scrape tag %q:%q: not found", wanted[i].typeName, wanted[i].value)
		}
		link := scrapeTagLink{id: wanted[i].key.id, tagDBID: tagDBID}
		if _, dup := seen[link]; dup {
			continue
		}
		seen[link] = struct{}{}
		links = append(links, link)
	}
	if err := insertScrapeTagLinks(ctx, writeCtx.tx, scope, links, linkStats); err != nil {
		return fmt.Errorf("insert missing scrape tags: %w", err)
	}
	return nil
}

// storedScrapeTagTypes reports which of the given rows already hold a tag of
// which of the given types.
func storedScrapeTagTypes(
	ctx context.Context,
	tx *sql.Tx,
	scope *scrapeScope,
	ids, typeDBIDs map[int64]struct{},
	stats *scrapeBatchSQLStats,
) (map[scrapeTypeKey]struct{}, error) {
	held := make(map[scrapeTypeKey]struct{})
	if len(ids) == 0 || len(typeDBIDs) == 0 {
		return held, nil
	}
	typeArgs := make([]any, 0, len(typeDBIDs))
	for typeDBID := range typeDBIDs {
		typeArgs = append(typeArgs, typeDBID)
	}
	idList := make([]int64, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	idsPerQuery := sqliteMaxParams - len(typeArgs)
	if idsPerQuery < 1 {
		return nil, fmt.Errorf("check missing scrape tags: %d tag types in one batch", len(typeArgs))
	}
	for start := 0; start < len(idList); start += idsPerQuery {
		chunk := idList[start:min(start+idsPerQuery, len(idList))]
		args := make([]any, 0, len(chunk)+len(typeArgs))
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, typeArgs...)
		// CROSS JOIN pins the plan to the rows' own links: each row holds a
		// handful of tags, while a type can have thousands of values.
		//nolint:gosec // Safe: fixed table names and generated placeholders.
		query := `SELECT x.` + scope.idColumn + `, t.TypeDBID FROM ` + scope.tagTable + ` x` +
			` CROSS JOIN Tags t ON t.DBID = x.TagDBID` +
			` WHERE x.` + scope.idColumn + ` IN (` + prepareVariadic("?", ",", len(chunk)) + `)` +
			` AND t.TypeDBID IN (` + prepareVariadic("?", ",", len(typeArgs)) + `)`
		if err := scanStoredScrapeTagTypes(ctx, tx, query, args, held); err != nil {
			return nil, err
		}
		stats.ExistenceQueries++
	}
	return held, nil
}

func scanStoredScrapeTagTypes(
	ctx context.Context, tx *sql.Tx, query string, args []any, held map[scrapeTypeKey]struct{},
) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("check missing scrape tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key scrapeTypeKey
		if err := rows.Scan(&key.id, &key.typeDBID); err != nil {
			return fmt.Errorf("scan stored scrape tag type: %w", err)
		}
		held[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stored scrape tag types: %w", err)
	}
	return nil
}

// resolveFillTags loads the Tags rows the batch needs into the write cache,
// creating those that do not exist. A new tag takes the label of the first
// offer that names it; an existing tag keeps the label it has, because it is
// shared with every other media that carries it.
func resolveFillTags(ctx context.Context, writeCtx *scrapeWriteTxContext, wanted []fillTag) error {
	var typeOrder []int64
	byType := make(map[int64][]int)
	queued := make(map[tagCacheKey]struct{})
	for i := range wanted {
		key := tagCacheKey{typeDBID: wanted[i].key.typeDBID, tag: wanted[i].value}
		if _, ok := writeCtx.tags[key]; ok {
			continue
		}
		if _, ok := queued[key]; ok {
			continue
		}
		queued[key] = struct{}{}
		if _, ok := byType[key.typeDBID]; !ok {
			typeOrder = append(typeOrder, key.typeDBID)
		}
		byType[key.typeDBID] = append(byType[key.typeDBID], i)
	}
	for _, typeDBID := range typeOrder {
		indexes := byType[typeDBID]
		values := make([]string, 0, len(indexes))
		for _, i := range indexes {
			values = append(values, wanted[i].value)
		}
		if err := queryTagsIntoCache(ctx, writeCtx, typeDBID, values); err != nil {
			return fmt.Errorf("resolve missing scrape tag: %w", err)
		}
		created := make([]string, 0)
		for _, i := range indexes {
			tag := &wanted[i]
			if _, ok := writeCtx.tags[tagCacheKey{typeDBID: typeDBID, tag: tag.value}]; ok {
				continue
			}
			if err := tags.ValidateTagValue(tags.TagType(tag.typeName), tag.value); err != nil {
				return fmt.Errorf("resolve missing scrape tag: %w", err)
			}
			if _, err := writeCtx.tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO Tags (TypeDBID, Tag, DisplayName) VALUES (?, ?, ?)`,
				typeDBID, tag.value, tag.label,
			); err != nil {
				return fmt.Errorf("resolve missing scrape tag %q:%q: %w", tag.typeName, tag.value, err)
			}
			created = append(created, tag.value)
		}
		if len(created) == 0 {
			continue
		}
		if err := queryTagsIntoCache(ctx, writeCtx, typeDBID, created); err != nil {
			return fmt.Errorf("resolve missing scrape tag: %w", err)
		}
	}
	return nil
}

func fillMissingScrapePropertiesBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	targets []database.ScrapeWriteTarget,
	stats *scrapePropStats,
) error {
	seen := make(map[scrapePropKey]struct{})
	var rows []scrapePropRow
	for i := range targets {
		id := scope.id(&targets[i])
		for _, p := range scope.props(targets[i].Write) {
			typeTagDBID, err := writeCtx.resolvePropertyTypeTag(ctx, p.TypeTag)
			if err != nil {
				return err
			}
			key := scrapePropKey{id: id, typeTagDBID: typeTagDBID}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, scrapePropRow{key: key, typeTag: p.TypeTag, text: p.Text, blob: p.BlobDBID})
		}
	}
	if err := writeScrapePropertyRows(ctx, writeCtx, scope, rows, "DO NOTHING", stats); err != nil {
		return fmt.Errorf("insert missing scrape properties: %w", err)
	}
	return nil
}
