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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// GetTitleFacetTags returns the tags of the given types each given title
// carries, read from the title's own tags.
func (db *MediaDB) GetTitleFacetTags(
	ctx context.Context, titleDBIDs []int64, tagTypes []string,
) ([]database.TitleFacetTag, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	if len(titleDBIDs) == 0 || len(tagTypes) == 0 {
		return nil, nil
	}
	typeArgs := make([]any, len(tagTypes))
	for i, tagType := range tagTypes {
		typeArgs[i] = tagType
	}
	var facets []database.TitleFacetTag
	perQuery := max(sqliteMaxParams-len(typeArgs), 1)
	for start := 0; start < len(titleDBIDs); start += perQuery {
		chunk := titleDBIDs[start:min(start+perQuery, len(titleDBIDs))]
		read, err := sqlTitleFacetTags(ctx, conn, chunk, typeArgs)
		if err != nil {
			return nil, err
		}
		facets = append(facets, read...)
	}
	return facets, nil
}

// sqlTitleFacetTags reads one statement's worth of titles. Each title is a
// primary-key range of MediaTitleTags, so the read costs the tags of the
// titles asked for and nothing else.
func sqlTitleFacetTags(
	ctx context.Context, conn sqlQueryable, titleDBIDs []int64, typeArgs []any,
) ([]database.TitleFacetTag, error) {
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
	query := `SELECT mtt.MediaTitleDBID, s.SystemID, mtl.Slug, t.DBID, tt.Type, t.Tag
		FROM MediaTitleTags mtt
		CROSS JOIN Tags t ON t.DBID = mtt.TagDBID
		CROSS JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		CROSS JOIN MediaTitles mtl ON mtl.DBID = mtt.MediaTitleDBID
		CROSS JOIN Systems s ON s.DBID = mtl.SystemDBID
		WHERE mtt.MediaTitleDBID IN (` + prepareVariadic("?", ",", len(titleDBIDs)) + `)
		  AND tt.Type IN (` + prepareVariadic("?", ",", len(typeArgs)) + `)
		ORDER BY mtt.MediaTitleDBID, t.DBID`
	rows, err := conn.QueryContext(ctx, query, append(int64Args(titleDBIDs), typeArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("query title facet tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var facets []database.TitleFacetTag
	for rows.Next() {
		var facet database.TitleFacetTag
		if err := rows.Scan(
			&facet.TitleDBID, &facet.SystemID, &facet.Slug, &facet.TagDBID, &facet.Type, &facet.Tag,
		); err != nil {
			return nil, fmt.Errorf("scan title facet tag: %w", err)
		}
		facets = append(facets, facet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate title facet tags: %w", err)
	}
	return facets, nil
}

// CountTitlesByTag returns how many titles carry each given tag, counting no
// further than limit. The bound keeps a tag most of the library carries from
// costing a walk of its whole posting list.
func (db *MediaDB) CountTitlesByTag(ctx context.Context, tagDBIDs []int64, limit int) (map[int64]int, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	counts := make(map[int64]int, len(tagDBIDs))
	if len(tagDBIDs) == 0 {
		return counts, nil
	}
	stmt, err := conn.PrepareContext(ctx,
		`SELECT COUNT(*) FROM (SELECT 1 FROM MediaTitleTags WHERE TagDBID = ? LIMIT ?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare tag title count: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, tagDBID := range tagDBIDs {
		if _, done := counts[tagDBID]; done {
			continue
		}
		var count int
		if err := stmt.QueryRowContext(ctx, tagDBID, limit).Scan(&count); err != nil {
			return nil, fmt.Errorf("count titles of tag %d: %w", tagDBID, err)
		}
		counts[tagDBID] = count
	}
	return counts, nil
}

// FindTitlesSharingTags returns the titles carrying any of the given tags,
// ordered by the summed weight of the tags they share. The caller bounds the
// work by which tags it passes: the read costs their posting lists. A non-nil
// systemIDs keeps only titles on those systems, before the limit, so a
// narrowed lookup is not starved by titles on other systems.
func (db *MediaDB) FindTitlesSharingTags(
	ctx context.Context, weighted []database.WeightedTag, excludeTitleDBIDs []int64,
	systemIDs []string, limit int,
) ([]database.TagSharingTitle, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	if len(weighted) == 0 || limit <= 0 || (systemIDs != nil && len(systemIDs) == 0) {
		return nil, nil
	}
	params := 2*len(weighted) + len(excludeTitleDBIDs) + len(systemIDs) + 1
	if params > sqliteMaxParams {
		return nil, fmt.Errorf("too many tags, exclusions and systems for one query: %d, %d and %d",
			len(weighted), len(excludeTitleDBIDs), len(systemIDs))
	}
	args := make([]any, 0, params)
	for i := range weighted {
		args = append(args, weighted[i].TagDBID, weighted[i].Weight)
	}
	var query strings.Builder
	// CROSS JOIN pins the order to start from the handful of tags, so each
	// one is a range of the tag index and no title is visited that shares
	// none of them. A system filter then probes each posting's title by its
	// primary key.
	query.WriteString(`WITH tok(TagDBID, W) AS (VALUES `)
	query.WriteString(prepareVariadic("(?,?)", ",", len(weighted)))
	query.WriteString(`) SELECT p.MediaTitleDBID, SUM(tok.W) AS S
		FROM tok CROSS JOIN MediaTitleTags p ON p.TagDBID = tok.TagDBID`)
	if systemIDs != nil {
		query.WriteString(` CROSS JOIN MediaTitles t ON t.DBID = p.MediaTitleDBID`)
	}
	query.WriteString(` WHERE 1`)
	if len(excludeTitleDBIDs) > 0 {
		query.WriteString(` AND p.MediaTitleDBID NOT IN (`)
		query.WriteString(prepareVariadic("?", ",", len(excludeTitleDBIDs)))
		query.WriteString(`)`)
		args = append(args, int64Args(excludeTitleDBIDs)...)
	}
	if systemIDs != nil {
		query.WriteString(` AND t.SystemDBID IN (SELECT DBID FROM Systems WHERE SystemID IN (`)
		query.WriteString(prepareVariadic("?", ",", len(systemIDs)))
		query.WriteString(`))`)
		for _, id := range systemIDs {
			args = append(args, id)
		}
	}
	query.WriteString(` GROUP BY p.MediaTitleDBID ORDER BY S DESC, p.MediaTitleDBID ASC LIMIT ?`)
	args = append(args, limit)

	rows, err := conn.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query titles sharing tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	candidates := make([]database.TagSharingTitle, 0, limit)
	for rows.Next() {
		var candidate database.TagSharingTitle
		if err := rows.Scan(&candidate.TitleDBID, &candidate.Score); err != nil {
			return nil, fmt.Errorf("scan title sharing tags: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate titles sharing tags: %w", err)
	}
	return candidates, nil
}

// userTagMediaSQL starts from the user tag type for the same reason the hidden
// media read does: left to the planner, it walks every present media row and
// probes MediaTags for each.
const userTagMediaSQL = `SELECT mt.MediaDBID, m.MediaTitleDBID, t.Tag FROM TagTypes tt
	CROSS JOIN Tags t ON t.TypeDBID = tt.DBID
	CROSS JOIN MediaTags mt ON mt.TagDBID = t.DBID
	CROSS JOIN Media m ON m.DBID = mt.MediaDBID
	WHERE tt.Type = ? AND m.IsMissing = 0
	ORDER BY mt.MediaDBID, t.Tag`

// GetUserTagMedia returns every user tag on a present media row.
func (db *MediaDB) GetUserTagMedia(ctx context.Context) ([]database.UserTagMedia, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	rows, err := conn.QueryContext(ctx, userTagMediaSQL, string(tags.TagTypeUser))
	if err != nil {
		return nil, fmt.Errorf("query user tag media: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tagged []database.UserTagMedia
	for rows.Next() {
		var row database.UserTagMedia
		if err := rows.Scan(&row.MediaDBID, &row.TitleDBID, &row.Tag); err != nil {
			return nil, fmt.Errorf("scan user tag media: %w", err)
		}
		tagged = append(tagged, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user tag media: %w", err)
	}
	return tagged, nil
}

// MediaTitleCountEstimate returns the highest title row ID, which is at least
// the number of titles and costs one seek where a count would walk the table.
func (db *MediaDB) MediaTitleCountEstimate(ctx context.Context) (int64, error) {
	conn := db.sql.Load()
	if conn == nil {
		return 0, ErrNullSQL
	}
	var count int64
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(DBID), 0) FROM MediaTitles`).Scan(&count); err != nil {
		return 0, fmt.Errorf("estimate title count: %w", err)
	}
	return count, nil
}
