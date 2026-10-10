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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// scrapeScope is one of the two rows a scrape write enriches, a Media row or
// its MediaTitle, with the tag link and property tables that hang off it. The
// multi-row statements below are the same for both.
type scrapeScope struct {
	// exclusiveDelete removes a set of rows' tags of one type. Its placeholders
	// are the row IDs followed by the type DBID. The two tables keep the forms
	// each was measured with on target storage.
	exclusiveDelete func(idPlaceholders string) string
	tagTable        string
	// tagLinkColumns and tagLinkValues are the column list and the per-row
	// placeholder group of a tag link insert.
	tagLinkColumns string
	tagLinkValues  string
	propTable      string
	idColumn       string
	title          bool
}

//nolint:gochecknoglobals // Fixed table descriptions.
var (
	scrapeMediaScope = scrapeScope{
		tagTable: "MediaTags", propTable: "MediaProperties", idColumn: "MediaDBID",
		tagLinkColumns: "MediaDBID, TagDBID, Scraped", tagLinkValues: "(?, ?, 1)",
		exclusiveDelete: func(ids string) string {
			return `DELETE FROM MediaTags WHERE MediaDBID IN (` + ids +
				`) AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`
		},
	}
	scrapeTitleScope = scrapeScope{
		tagTable: "MediaTitleTags", propTable: "MediaTitleProperties", idColumn: "MediaTitleDBID",
		tagLinkColumns: "MediaTitleDBID, TagDBID", tagLinkValues: "(?, ?)",
		exclusiveDelete: func(ids string) string {
			return `DELETE FROM MediaTitleTags WHERE MediaTitleDBID IN (` + ids +
				`) AND EXISTS (` +
				`SELECT 1 FROM Tags WHERE Tags.DBID = MediaTitleTags.TagDBID AND Tags.TypeDBID = ?` +
				`)`
		},
		title: true,
	}
)

func (s *scrapeScope) id(target *database.ScrapeWriteTarget) int64 {
	if s.title {
		return target.MediaTitleDBID
	}
	return target.MediaDBID
}

func (s *scrapeScope) tags(write *database.ScrapeWrite) []database.TagInfo {
	if s.title {
		return write.TitleTags
	}
	return write.MediaTags
}

func (s *scrapeScope) props(write *database.ScrapeWrite) []database.MediaProperty {
	if s.title {
		return write.TitleProps
	}
	return write.MediaProps
}

func (s *scrapeScope) markImageChanged(writeCtx *scrapeWriteTxContext, id int64) {
	if s.title {
		writeCtx.changedImageMediaTitleIDs[id] = struct{}{}
		return
	}
	writeCtx.changedImageMediaIDs[id] = struct{}{}
}

// scrapeTagLink is one row of a tag link table.
type scrapeTagLink struct {
	id      int64
	tagDBID int64
}

// scrapeTypeKey identifies one row's tags of one type.
type scrapeTypeKey struct {
	id       int64
	typeDBID int64
}

// scrapePropKey identifies one property row.
type scrapePropKey struct {
	id          int64
	typeTagDBID int64
}

type scrapePropRow struct {
	blob    *int64
	typeTag string
	text    string
	key     scrapePropKey
}

// upsertScrapeTagsBulk applies the ordinary (replace) tag policy for one scope
// across a batch: an exclusive type is cleared and takes the last target's
// value, additive types gain every value.
func upsertScrapeTagsBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	targets []database.ScrapeWriteTarget,
	stats *scrapeLinkStats,
) error {
	exclusiveDeletes := make(map[int64]map[int64]struct{})
	exclusiveFinal := make(map[scrapeTypeKey][]int64)
	additive := make(map[scrapeTagLink]struct{})

	for i := range targets {
		tagInfos := scope.tags(targets[i].Write)
		if len(tagInfos) == 0 {
			continue
		}
		id := scope.id(&targets[i])
		typeOrder := make([]string, 0, len(tagInfos))
		byType := make(map[string][]database.TagInfo, len(tagInfos))
		for _, ti := range tagInfos {
			if _, ok := byType[ti.Type]; !ok {
				typeOrder = append(typeOrder, ti.Type)
			}
			byType[ti.Type] = append(byType[ti.Type], ti)
		}
		for _, typeName := range typeOrder {
			typeDBID, isExclusive, err := writeCtx.resolveTagType(ctx, typeName)
			if err != nil {
				return err
			}
			typeTags := byType[typeName]
			if isExclusive {
				seen := make(map[string]struct{}, len(typeTags))
				for _, ti := range typeTags {
					seen[tags.PadTagValue(ti.Tag)] = struct{}{}
				}
				if len(seen) > 1 {
					return fmt.Errorf("exclusive tag type %q received multiple values", typeName)
				}
				if _, ok := exclusiveDeletes[typeDBID]; !ok {
					exclusiveDeletes[typeDBID] = make(map[int64]struct{})
				}
				exclusiveDeletes[typeDBID][id] = struct{}{}
			}

			resolved := make([]int64, 0, len(typeTags))
			for _, ti := range typeTags {
				tagDBID, err := writeCtx.resolveTag(ctx, typeDBID, typeName, tags.PadTagValue(ti.Tag), ti.Label)
				if err != nil {
					return err
				}
				resolved = append(resolved, tagDBID)
			}
			if isExclusive {
				exclusiveFinal[scrapeTypeKey{id: id, typeDBID: typeDBID}] = resolved
				continue
			}
			for _, tagDBID := range resolved {
				additive[scrapeTagLink{id: id, tagDBID: tagDBID}] = struct{}{}
			}
		}
	}

	if err := deleteScrapeTagsByExclusiveType(ctx, writeCtx.tx, scope, exclusiveDeletes, stats); err != nil {
		return err
	}

	links := make([]scrapeTagLink, 0, len(additive)+len(exclusiveFinal))
	for link := range additive {
		links = append(links, link)
	}
	for key, tagDBIDs := range exclusiveFinal {
		for _, tagDBID := range tagDBIDs {
			links = append(links, scrapeTagLink{id: key.id, tagDBID: tagDBID})
		}
	}
	return insertScrapeTagLinks(ctx, writeCtx.tx, scope, links, stats)
}

func deleteScrapeTagsByExclusiveType(
	ctx context.Context,
	tx *sql.Tx,
	scope *scrapeScope,
	exclusiveDeletes map[int64]map[int64]struct{},
	stats *scrapeLinkStats,
) error {
	for typeDBID, idSet := range exclusiveDeletes {
		ids := make([]int64, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		for start := 0; start < len(ids); start += bulkDeleteEntityIDsPerStmt {
			chunk := ids[start:min(start+bulkDeleteEntityIDsPerStmt, len(ids))]
			args := make([]any, 0, len(chunk)+1)
			for _, id := range chunk {
				args = append(args, id)
			}
			args = append(args, typeDBID)
			query := scope.exclusiveDelete(prepareVariadic("?", ",", len(chunk)))
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("failed to delete %s rows for type: %w", scope.tagTable, err)
			}
			stats.Deletes++
		}
	}
	return nil
}

// insertScrapeTagLinks inserts tag links that are not already stored and
// counts the rows it added.
func insertScrapeTagLinks(
	ctx context.Context, tx *sql.Tx, scope *scrapeScope, links []scrapeTagLink, stats *scrapeLinkStats,
) error {
	for start := 0; start < len(links); start += bulkTagInsertRowsPerStmt {
		chunk := links[start:min(start+bulkTagInsertRowsPerStmt, len(links))]
		args := make([]any, 0, len(chunk)*2)
		for _, link := range chunk {
			args = append(args, link.id, link.tagDBID)
		}
		//nolint:gosec // Safe: fixed table names and generated placeholders.
		query := `INSERT OR IGNORE INTO ` + scope.tagTable + ` (` + scope.tagLinkColumns + `) VALUES ` +
			prepareVariadic(scope.tagLinkValues, ",", len(chunk))
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to insert %s rows: %w", scope.tagTable, err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read inserted %s rows: %w", scope.tagTable, err)
		}
		stats.InsertRows += int(inserted)
		stats.InsertStatements++
	}
	return nil
}

// upsertScrapePropertiesBulk applies the ordinary (replace) property policy
// for one scope: the last target to name a property wins it.
func upsertScrapePropertiesBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	targets []database.ScrapeWriteTarget,
	stats *scrapePropStats,
) error {
	index := make(map[scrapePropKey]int)
	var rows []scrapePropRow
	for i := range targets {
		id := scope.id(&targets[i])
		for _, p := range scope.props(targets[i].Write) {
			typeTagDBID, err := writeCtx.resolvePropertyTypeTag(ctx, p.TypeTag)
			if err != nil {
				return fmt.Errorf("failed to resolve property type tag %q: %w", p.TypeTag, err)
			}
			key := scrapePropKey{id: id, typeTagDBID: typeTagDBID}
			row := scrapePropRow{key: key, typeTag: p.TypeTag, text: p.Text, blob: p.BlobDBID}
			if at, ok := index[key]; ok {
				rows[at] = row
				continue
			}
			index[key] = len(rows)
			rows = append(rows, row)
		}
	}
	const conflict = `DO UPDATE SET Text = excluded.Text, BlobDBID = excluded.BlobDBID` +
		` WHERE %[1]s.Text IS NOT excluded.Text OR %[1]s.BlobDBID IS NOT excluded.BlobDBID`
	return writeScrapePropertyRows(ctx, writeCtx, scope, rows, fmt.Sprintf(conflict, scope.propTable), stats)
}

// writeScrapePropertyRows inserts property rows under the given conflict
// action and records which image properties the statement changed.
func writeScrapePropertyRows(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	rows []scrapePropRow,
	conflictAction string,
	stats *scrapePropStats,
) error {
	for start := 0; start < len(rows); start += bulkPropUpsertRowsPerStmt {
		chunk := rows[start:min(start+bulkPropUpsertRowsPerStmt, len(rows))]
		args := make([]any, 0, len(chunk)*4)
		imageKeys := make(map[scrapePropKey]struct{})
		for i := range chunk {
			args = append(args, chunk[i].key.id, chunk[i].key.typeTagDBID, chunk[i].text, chunk[i].blob)
			if isImageProperty(chunk[i].typeTag) {
				imageKeys[chunk[i].key] = struct{}{}
			}
		}
		//nolint:gosec // Safe: fixed table names and generated placeholders.
		query := `INSERT INTO ` + scope.propTable + ` (` + scope.idColumn + `, TypeTagDBID, Text, BlobDBID) VALUES ` +
			prepareVariadic("(?, ?, ?, ?)", ",", len(chunk)) +
			` ON CONFLICT(` + scope.idColumn + `, TypeTagDBID) ` + conflictAction +
			` RETURNING ` + scope.idColumn + `, TypeTagDBID`
		changed, err := scanChangedScrapeProperties(ctx, writeCtx, scope, query, args, imageKeys)
		if err != nil {
			return err
		}
		stats.ChangedRows += changed
		stats.Statements++
	}
	return nil
}

func scanChangedScrapeProperties(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	scope *scrapeScope,
	query string,
	args []any,
	imageKeys map[scrapePropKey]struct{},
) (int, error) {
	changedRows, err := writeCtx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to write %s rows: %w", scope.propTable, err)
	}
	defer func() { _ = changedRows.Close() }()
	changed := 0
	for changedRows.Next() {
		var key scrapePropKey
		if err := changedRows.Scan(&key.id, &key.typeTagDBID); err != nil {
			return 0, fmt.Errorf("failed to scan changed %s row: %w", scope.propTable, err)
		}
		changed++
		if _, ok := imageKeys[key]; ok {
			scope.markImageChanged(writeCtx, key.id)
		}
	}
	if err := changedRows.Err(); err != nil {
		return 0, fmt.Errorf("failed to iterate changed %s rows: %w", scope.propTable, err)
	}
	return changed, nil
}

// upsertScrapeSentinelsBulk writes every target's sentinel tag. It runs last
// in the transaction, so an interrupted batch leaves its rows retryable.
func upsertScrapeSentinelsBulk(
	ctx context.Context,
	writeCtx *scrapeWriteTxContext,
	targets []database.ScrapeWriteTarget,
	stats *scrapeLinkStats,
) error {
	links := make([]scrapeTagLink, 0, len(targets))
	seen := make(map[scrapeTagLink]struct{}, len(targets))
	exclusiveDeletes := make(map[int64]map[int64]struct{})
	for i := range targets {
		sentinel := targets[i].Write.Sentinel
		typeDBID, isExclusive, err := writeCtx.resolveTagType(ctx, sentinel.Type)
		if err != nil {
			return err
		}
		if isExclusive {
			if _, ok := exclusiveDeletes[typeDBID]; !ok {
				exclusiveDeletes[typeDBID] = make(map[int64]struct{})
			}
			exclusiveDeletes[typeDBID][targets[i].MediaDBID] = struct{}{}
		}
		tagDBID, err := writeCtx.resolveTag(
			ctx, typeDBID, sentinel.Type, tags.PadTagValue(sentinel.Tag), sentinel.Label,
		)
		if err != nil {
			return err
		}
		link := scrapeTagLink{id: targets[i].MediaDBID, tagDBID: tagDBID}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		links = append(links, link)
	}
	if err := deleteScrapeTagsByExclusiveType(
		ctx, writeCtx.tx, &scrapeMediaScope, exclusiveDeletes, stats,
	); err != nil {
		return err
	}
	return insertScrapeTagLinks(ctx, writeCtx.tx, &scrapeMediaScope, links, stats)
}
