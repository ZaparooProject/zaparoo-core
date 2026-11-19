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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

// Artwork sampling draws random Media DBIDs and keeps the ones that qualify.
// Discarding misses, instead of moving to the next row, keeps every eligible
// row equally likely, and a draw is a primary key lookup plus two probes of
// the UNIQUE (entity, TypeTag) property indexes, which indexing never drops.
// When few rows qualify the draws run out first, and the sampler enumerates
// the eligible rows from the TypeTag indexes instead. That is cheap exactly
// when the draws fail, because few rows hold the requested artwork.
const (
	artworkProbeBatchMin    = 64
	artworkProbeBatchFactor = 4
	artworkProbeBatches     = 8
	// artworkDrawAttemptFactor bounds redraws of already drawn DBIDs per batch
	// slot, so a nearly exhausted range cannot spin.
	artworkDrawAttemptFactor = 4
	artworkFallbackCtxRows   = 1024
)

// artworkDBIDRangeSQL uses separate subqueries: SQLite answers a lone MIN or
// MAX from the end of the primary key, but both in one SELECT scan the table.
const artworkDBIDRangeSQL = "SELECT COALESCE((SELECT MIN(DBID) FROM Media), 0), " +
	"COALESCE((SELECT MAX(DBID) FROM Media), 0)"

// artworkFallbackIndexes are the secondary indexes the enumeration fallback
// reads through. Indexing drops them, and the fallback would then scan every
// property row, so it is skipped until they are back.
var artworkFallbackIndexes = []string{
	"mediaproperties_typetag_idx",
	"mediatitleproperties_typetag_idx",
	"media_title_present_idx",
	"media_mediatitle_idx",
}

// SampleArtworkMedia returns up to query.Limit random media rows holding at
// least one of query.ImageTypes on the media row or its title. All statements
// run on one pooled connection.
func (db *MediaDB) SampleArtworkMedia(
	ctx context.Context, query *database.ArtworkSampleQuery,
) ([]database.ArtworkSample, error) {
	return db.sampleArtworkMediaUsing(ctx, query, helpers.RandomInt)
}

func (db *MediaDB) sampleArtworkMediaUsing(
	ctx context.Context,
	query *database.ArtworkSampleQuery,
	randomInt func(int) (int, error),
) ([]database.ArtworkSample, error) {
	if query == nil || query.Limit <= 0 || len(query.ImageTypes) == 0 {
		return []database.ArtworkSample{}, nil
	}
	call, err := db.beginBrowse(ctx, "artwork sample", 0)
	if err != nil {
		return nil, err
	}
	defer call.finish(db)

	scoped := *query
	scoped.Tags, err = discoveryTags(
		ctx, call.conn, query.Tags, query.ExcludeHidden, recursiveScope(query.PathPrefix),
	)
	if err != nil {
		return nil, err
	}
	return sqlSampleArtworkMedia(ctx, call.conn, &scoped, randomInt)
}

type artworkTypeTag struct {
	typeTag string
	dbid    int64
}

func sqlSampleArtworkMedia(
	ctx context.Context,
	db sqlQueryable,
	query *database.ArtworkSampleQuery,
	randomInt func(int) (int, error),
) ([]database.ArtworkSample, error) {
	empty := []database.ArtworkSample{}
	typeTags, err := resolveArtworkTypeTags(ctx, db, query.ImageTypes)
	if err != nil {
		return nil, err
	}
	if len(typeTags) == 0 || query.Limit <= 0 {
		return empty, nil
	}
	typeDBIDs := make([]any, len(typeTags))
	for i := range typeTags {
		typeDBIDs[i] = typeTags[i].dbid
	}

	ids, err := probeArtworkMedia(ctx, db, query, typeDBIDs, randomInt)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return empty, nil
	}
	samples, err := loadArtworkSamples(ctx, db, ids, typeTags, typeDBIDs)
	if err != nil {
		return nil, err
	}
	for i := len(samples) - 1; i > 0; i-- {
		j, randErr := randomInt(i + 1)
		if randErr != nil {
			return nil, fmt.Errorf("failed to shuffle artwork samples: %w", randErr)
		}
		samples[i], samples[j] = samples[j], samples[i]
	}
	return samples, nil
}

// resolveArtworkTypeTags maps requested property type tags to their Tags
// DBIDs in request order. Types no row has ever used are absent from Tags and
// are dropped: nothing can match them.
func resolveArtworkTypeTags(
	ctx context.Context, db sqlQueryable, imageTypes []string,
) ([]artworkTypeTag, error) {
	seen := make(map[string]bool, len(imageTypes))
	var conditions []string
	var args []any
	var ordered []string
	for _, typeTag := range imageTypes {
		tagType, value, ok := strings.Cut(typeTag, ":")
		if !ok || tagType == "" || value == "" {
			return nil, fmt.Errorf("artwork type tag %q is not in type:value format", typeTag)
		}
		if seen[typeTag] {
			continue
		}
		seen[typeTag] = true
		ordered = append(ordered, typeTag)
		conditions = append(conditions, "(tt.Type = ? AND t.Tag = ?)")
		args = append(args, tagType, value)
	}

	//nolint:gosec // conditions are fixed placeholder groups
	rows, err := db.QueryContext(ctx, `
		SELECT tt.Type, t.Tag, t.DBID
		FROM Tags t
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE `+strings.Join(conditions, " OR "), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve artwork type tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := make(map[string]int64, len(ordered))
	for rows.Next() {
		var tagType, value string
		var dbid int64
		if scanErr := rows.Scan(&tagType, &value, &dbid); scanErr != nil {
			return nil, fmt.Errorf("failed to scan artwork type tag: %w", scanErr)
		}
		found[tagType+":"+value] = dbid
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate artwork type tags: %w", rowsErr)
	}

	resolved := make([]artworkTypeTag, 0, len(ordered))
	for _, typeTag := range ordered {
		if dbid, ok := found[typeTag]; ok {
			resolved = append(resolved, artworkTypeTag{typeTag: typeTag, dbid: dbid})
		}
	}
	return resolved, nil
}

// artworkCondition is the row predicate for "has a requested type on the media
// row or on its title". Its arguments are typeDBIDs twice.
func artworkCondition(typeCount int) string {
	marks := prepareVariadic("?", ",", typeCount)
	return `(EXISTS (SELECT 1 FROM MediaProperties ap
			WHERE ap.MediaDBID = Media.DBID AND ap.TypeTagDBID IN (` + marks + `))
		OR EXISTS (SELECT 1 FROM MediaTitleProperties atp
			WHERE atp.MediaTitleDBID = Media.MediaTitleDBID AND atp.TypeTagDBID IN (` + marks + `)))`
}

// artworkFromClause joins Media to the ID source with CROSS JOIN so SQLite
// keeps the ID source as the outer loop. The property tables usually have no
// planner statistics (the scraper fills them after indexing's ANALYZE), and
// without the pin the planner may drive from a system index or a TypeTag index
// and visit every row of the scope instead.
func artworkFromClause(source string, query *database.MediaQuery) string {
	from := "FROM " + source + "\n\t\tCROSS JOIN Media ON Media.DBID = ids.id"
	if query.PathGlob != "" {
		from += "\n\t\tINNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID"
	}
	return from
}

// probeArtworkMedia returns eligible DBIDs in random order, or falls back to
// enumerating them when the random draws run out first.
func probeArtworkMedia(
	ctx context.Context,
	db sqlQueryable,
	query *database.ArtworkSampleQuery,
	typeDBIDs []any,
	randomInt func(int) (int, error),
) ([]int64, error) {
	var minDBID, maxDBID int64
	if err := db.QueryRowContext(ctx, artworkDBIDRangeSQL).Scan(&minDBID, &maxDBID); err != nil {
		return nil, fmt.Errorf("failed to read media DBID range: %w", err)
	}
	if maxDBID < minDBID || maxDBID <= 0 {
		return nil, nil
	}
	rangeSize := maxDBID - minDBID + 1
	if rangeSize <= 0 || rangeSize > int64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("media DBID range %d..%d is too large to sample", minDBID, maxDBID)
	}

	excluded := make(map[int64]bool, len(query.ExcludeDBIDs))
	for _, id := range query.ExcludeDBIDs {
		excluded[id] = true
	}
	batchSize := max(query.Limit*artworkProbeBatchFactor, artworkProbeBatchMin)
	whereClause, whereArgs := buildMediaCandidateQueryWhereClause(&query.MediaQuery)

	drawn := make(map[int64]bool)
	hits := make([]int64, 0, query.Limit)
	for range artworkProbeBatches {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("artwork sample cancelled: %w", err)
		}
		batch := make([]int64, 0, batchSize)
		for attempts := 0; len(batch) < batchSize && attempts < batchSize*artworkDrawAttemptFactor; attempts++ {
			if int64(len(drawn)) >= rangeSize {
				break
			}
			offset, err := randomInt(int(rangeSize))
			if err != nil {
				return nil, fmt.Errorf("failed to draw random media DBID: %w", err)
			}
			id := minDBID + int64(offset)
			if drawn[id] {
				continue
			}
			drawn[id] = true
			if !excluded[id] {
				batch = append(batch, id)
			}
		}
		if len(batch) > 0 {
			sqlText, args := artworkProbeQuery(&query.MediaQuery, whereClause, whereArgs, typeDBIDs, batch)
			matched, err := matchArtworkBatch(ctx, db, sqlText, args)
			if err != nil {
				return nil, err
			}
			// Keep draw order: the eligible rows of a uniformly random draw
			// sequence are a uniformly random sequence of eligible rows.
			for _, id := range batch {
				if matched[id] {
					hits = append(hits, id)
					if len(hits) == query.Limit {
						return hits, nil
					}
				}
			}
		}
		if int64(len(drawn)) >= rangeSize {
			// Every DBID was tried, so hits is every eligible row.
			return hits, nil
		}
	}

	ready, err := artworkFallbackIndexesPresent(ctx, db)
	if err != nil {
		return nil, err
	}
	if !ready {
		return hits, nil
	}
	return enumerateArtworkMedia(ctx, db, query, typeDBIDs, randomInt)
}

// artworkProbeQuery checks one batch of drawn DBIDs. The batch is the outer
// loop, so each draw costs a primary key lookup and the property probes.
func artworkProbeQuery(
	query *database.MediaQuery,
	whereClause string,
	whereArgs []any,
	typeDBIDs []any,
	batch []int64,
) (sqlText string, args []any) {
	source := "(SELECT column1 AS id FROM (VALUES " + prepareVariadic("(?)", ",", len(batch)) + ")) ids"
	sqlText = "SELECT Media.DBID " + artworkFromClause(source, query) + "\n" + whereClause +
		" AND " + artworkCondition(len(typeDBIDs))
	args = make([]any, 0, len(batch)+len(whereArgs)+2*len(typeDBIDs))
	for _, id := range batch {
		args = append(args, id)
	}
	args = append(args, whereArgs...)
	args = append(args, typeDBIDs...)
	args = append(args, typeDBIDs...)
	return sqlText, args
}

func matchArtworkBatch(
	ctx context.Context, db sqlQueryable, probeSQL string, args []any,
) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, probeSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to probe artwork media: %w", err)
	}
	defer func() { _ = rows.Close() }()
	matched := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("failed to scan artwork media probe: %w", scanErr)
		}
		matched[id] = true
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate artwork media probe: %w", rowsErr)
	}
	return matched, nil
}

func artworkFallbackIndexesPresent(ctx context.Context, db sqlQueryable) (bool, error) {
	args := make([]any, len(artworkFallbackIndexes))
	for i, name := range artworkFallbackIndexes {
		args[i] = name
	}
	var count int
	//nolint:gosec // placeholders only
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ("+
			prepareVariadic("?", ",", len(args))+")", args...,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check artwork sampling indexes: %w", err)
	}
	return count == len(artworkFallbackIndexes), nil
}

// enumerateArtworkMedia streams every eligible DBID from the TypeTag indexes
// through a reservoir, so memory stays at Limit IDs and the pick stays
// uniform however many rows match.
func enumerateArtworkMedia(
	ctx context.Context,
	db sqlQueryable,
	query *database.ArtworkSampleQuery,
	typeDBIDs []any,
	randomInt func(int) (int, error),
) ([]int64, error) {
	sqlText, args := artworkEnumerateQuery(query, typeDBIDs)
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate artwork media: %w", err)
	}
	defer func() { _ = rows.Close() }()
	reservoir := make([]int64, 0, query.Limit)
	seen := 0
	for rows.Next() {
		if seen%artworkFallbackCtxRows == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("artwork sample cancelled: %w", ctxErr)
			}
		}
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("failed to scan artwork media: %w", scanErr)
		}
		seen++
		if len(reservoir) < query.Limit {
			reservoir = append(reservoir, id)
			continue
		}
		j, randErr := randomInt(seen)
		if randErr != nil {
			return nil, fmt.Errorf("failed to sample artwork media: %w", randErr)
		}
		if j < query.Limit {
			reservoir[j] = id
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate artwork media: %w", rowsErr)
	}
	return reservoir, nil
}

// artworkEnumerateQuery lists every eligible DBID, starting from the TypeTag
// indexes so the cost follows the number of artwork rows, not media rows.
func artworkEnumerateQuery(
	query *database.ArtworkSampleQuery, typeDBIDs []any,
) (sqlText string, args []any) {
	whereClause, whereArgs := buildMediaCandidateQueryWhereClause(&query.MediaQuery)
	marks := prepareVariadic("?", ",", len(typeDBIDs))
	// Both branches start from the TypeTag index; the title branch then
	// reaches media through one of the MediaTitleDBID indexes. CROSS JOIN pins
	// that order for the same reason as in artworkFromClause.
	source := `(
			SELECT ep.MediaDBID AS id FROM MediaProperties ep
			WHERE ep.TypeTagDBID IN (` + marks + `)
			UNION
			SELECT em.DBID AS id FROM MediaTitleProperties etp
			CROSS JOIN Media em ON em.MediaTitleDBID = etp.MediaTitleDBID AND em.IsMissing = 0
			WHERE etp.TypeTagDBID IN (` + marks + `)
		) ids`
	sqlText = "SELECT Media.DBID " + artworkFromClause(source, &query.MediaQuery) + "\n" + whereClause
	args = make([]any, 0, 2*len(typeDBIDs)+len(whereArgs)+len(query.ExcludeDBIDs))
	args = append(args, typeDBIDs...)
	args = append(args, typeDBIDs...)
	args = append(args, whereArgs...)
	if len(query.ExcludeDBIDs) > 0 {
		sqlText += " AND Media.DBID NOT IN (" + prepareVariadic("?", ",", len(query.ExcludeDBIDs)) + ")"
		for _, id := range query.ExcludeDBIDs {
			args = append(args, id)
		}
	}
	return sqlText, args
}

// loadArtworkSamples reads the details and matching artwork types of ids,
// keeping the order of ids. A row deleted or emptied of artwork since it was
// picked is dropped.
func loadArtworkSamples(
	ctx context.Context,
	db sqlQueryable,
	ids []int64,
	typeTags []artworkTypeTag,
	typeDBIDs []any,
) ([]database.ArtworkSample, error) {
	idArgs := make([]any, len(ids))
	for i, id := range ids {
		idArgs[i] = id
	}
	idMarks := prepareVariadic("?", ",", len(ids))

	byID, err := loadArtworkRows(ctx, db, idMarks, idArgs)
	if err != nil {
		return nil, err
	}

	typeMarks := prepareVariadic("?", ",", len(typeDBIDs))
	typeArgs := make([]any, 0, 2*(len(ids)+len(typeDBIDs)))
	typeArgs = append(typeArgs, idArgs...)
	typeArgs = append(typeArgs, typeDBIDs...)
	typeArgs = append(typeArgs, idArgs...)
	typeArgs = append(typeArgs, typeDBIDs...)
	typeRows, err := db.QueryContext(ctx, `
		SELECT mp.MediaDBID, mp.TypeTagDBID FROM MediaProperties mp
		WHERE mp.MediaDBID IN (`+idMarks+`) AND mp.TypeTagDBID IN (`+typeMarks+`)
		UNION
		SELECT m.DBID, tp.TypeTagDBID FROM Media m
		CROSS JOIN MediaTitleProperties tp ON tp.MediaTitleDBID = m.MediaTitleDBID
		WHERE m.DBID IN (`+idMarks+`) AND tp.TypeTagDBID IN (`+typeMarks+`)`, typeArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to load artwork types: %w", err)
	}
	defer func() { _ = typeRows.Close() }()
	has := make(map[int64]map[int64]bool, len(ids))
	for typeRows.Next() {
		var mediaID, typeDBID int64
		if scanErr := typeRows.Scan(&mediaID, &typeDBID); scanErr != nil {
			return nil, fmt.Errorf("failed to scan artwork type: %w", scanErr)
		}
		if has[mediaID] == nil {
			has[mediaID] = make(map[int64]bool, len(typeTags))
		}
		has[mediaID][typeDBID] = true
	}
	if rowsErr := typeRows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate artwork types: %w", rowsErr)
	}

	samples := make([]database.ArtworkSample, 0, len(ids))
	for _, id := range ids {
		sample, ok := byID[id]
		if !ok {
			continue
		}
		for _, tt := range typeTags {
			if has[id][tt.dbid] {
				sample.ImageTypes = append(sample.ImageTypes, tt.typeTag)
			}
		}
		if len(sample.ImageTypes) == 0 {
			continue
		}
		samples = append(samples, *sample)
	}
	return samples, nil
}

// loadArtworkRows reads the details of the media rows in idArgs that still
// exist and are not missing, keyed by DBID.
func loadArtworkRows(
	ctx context.Context,
	db sqlQueryable,
	idMarks string,
	idArgs []any,
) (map[int64]*database.ArtworkSample, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT Media.DBID, Systems.SystemID, MediaTitles.Name, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = Media.SystemDBID
		WHERE Media.IsMissing = 0 AND Media.DBID IN (`+idMarks+`)`, idArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to load artwork media: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[int64]*database.ArtworkSample, len(idArgs))
	for rows.Next() {
		sample := &database.ArtworkSample{}
		if scanErr := rows.Scan(&sample.MediaID, &sample.SystemID, &sample.Name, &sample.Path); scanErr != nil {
			return nil, fmt.Errorf("failed to scan artwork media: %w", scanErr)
		}
		byID[sample.MediaID] = sample
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate artwork media: %w", rowsErr)
	}
	return byID, nil
}
