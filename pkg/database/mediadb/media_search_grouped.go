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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

// groupedSearchRowLimit bounds the media rows fetched for one page of title
// groups. It only guards against a pathological library; a page of titles
// normally holds a few rows each.
const groupedSearchRowLimit = 100_000

// titleGroupScope is one title-level statement of a grouped search: either a
// set of title DBIDs from the slug cache, or the slug LIKE conditions for one
// media type group.
type titleGroupScope struct {
	systems       []systemdefs.System
	variantGroups [][]string
	rawWords      []string
	titleDBIDs    []int64
	includeName   bool
}

type titleGroupKey struct {
	sortValue string
	titleDBID int64
}

// SearchMediaTitleGroups implements MediaDBI. It pages titles rather than
// media rows: a title is in the page when at least one of its present media
// rows matches the filters, and every matching row of each title in the page
// is returned with it. Limit counts titles. Sort accepts "", "name-asc" and
// "name-desc"; SortCursor.LastID is a title DBID.
func (db *MediaDB) SearchMediaTitleGroups(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.SearchTitleGroup, error) {
	return db.searchTitleGroupsWithRowLimit(ctx, filters, groupedSearchRowLimit)
}

func (db *MediaDB) searchTitleGroupsWithRowLimit(
	ctx context.Context,
	filters *database.SearchFilters,
	rowLimit int,
) ([]database.SearchTitleGroup, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	switch filters.Sort {
	case "", "name-asc", "name-desc":
	default:
		return nil, fmt.Errorf("grouped search does not support sort %q", filters.Sort)
	}
	if filters.SortCursor != nil && filters.SortCursor.Sort != filters.Sort {
		return nil, errors.New("search cursor sort does not match request")
	}
	if len(filters.Systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	scoped := *filters
	var visibilityErr error
	scoped.Tags, visibilityErr = discoveryTags(
		ctx, db.sql.Load(), filters.Tags, filters.ExcludeHidden, recursiveScope(filters.PathPrefix),
	)
	if visibilityErr != nil {
		return nil, visibilityErr
	}
	filters = &scoped

	scopes, err := db.titleGroupScopes(ctx, filters)
	if err != nil {
		return nil, err
	}

	// The title page and its rows are read from one snapshot, so a title
	// cannot lose its rows between the two and shorten the page.
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin grouped search read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	keys := make([]titleGroupKey, 0, filters.Limit)
	for i := range scopes {
		scopeKeys, scopeErr := sqlSearchTitleGroupKeys(ctx, tx, &scopes[i], filters)
		if scopeErr != nil {
			return nil, scopeErr
		}
		keys = append(keys, scopeKeys...)
	}
	if len(scopes) > 1 {
		slices.SortFunc(keys, func(a, b titleGroupKey) int {
			return compareTitleGroupKeys(a, b, filters.Sort)
		})
		keys = slices.CompactFunc(keys, func(a, b titleGroupKey) bool {
			return a.titleDBID == b.titleDBID
		})
	}
	if len(keys) > filters.Limit {
		keys = keys[:filters.Limit]
	}
	if len(keys) == 0 {
		return []database.SearchTitleGroup{}, nil
	}

	titleDBIDs := make([]int64, len(keys))
	for i := range keys {
		titleDBIDs[i] = keys[i].titleDBID
	}
	rowsStarted := time.Now()
	perQuery := max(sqliteMaxParams-titleDBIDQueryParamCount(0, filters), 1)
	var rows []database.SearchResultWithCursor
	for start := 0; start < len(titleDBIDs); start += perQuery {
		chunk, chunkErr := sqlSearchMediaByTitleDBIDsSorted(
			ctx, tx, titleDBIDs[start:min(start+perQuery, len(titleDBIDs))],
			filters.PathPrefix, filters.Tags, nil, nil, nil, "", rowLimit)
		if chunkErr != nil {
			return nil, chunkErr
		}
		if len(chunk) >= rowLimit {
			return nil, fmt.Errorf("grouped search page holds more than %d media rows", rowLimit-1)
		}
		rows = append(rows, chunk...)
	}

	byTitle := make(map[int64][]database.SearchResultWithCursor, len(keys))
	for i := range rows {
		byTitle[rows[i].MediaTitleID] = append(byTitle[rows[i].MediaTitleID], rows[i])
	}
	groups := make([]database.SearchTitleGroup, 0, len(keys))
	for _, key := range keys {
		titleRows := byTitle[key.titleDBID]
		if len(titleRows) == 0 {
			continue
		}
		groups = append(groups, database.SearchTitleGroup{
			SortValue: key.sortValue,
			TitleDBID: key.titleDBID,
			Rows:      titleRows,
		})
	}

	log.Debug().
		Int("scopes", len(scopes)).
		Int("titles", len(groups)).
		Int("rows", len(rows)).
		Str("sort", filters.Sort).
		Dur("rowsDuration", time.Since(rowsStarted)).
		Msg("grouped media search step timing")

	return groups, nil
}

// titleGroupScopes splits a grouped search into title-level statements the
// same way SearchMediaWithFilters splits a row search: the slug cache where it
// can answer, one LIKE statement per media type group where it cannot.
func (db *MediaDB) titleGroupScopes(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]titleGroupScope, error) {
	qWords := strings.Fields(filters.Query)
	if len(qWords) == 0 {
		return []titleGroupScope{{systems: filters.Systems}}, nil
	}

	searchSystems := filters.Systems
	if strings.Contains(filters.PathPrefix, "://") && requestedAllSystems(filters.Systems) {
		var err error
		searchSystems, err = mediaSearchSystemsForPathPrefix(ctx, db.sql.Load(), filters.PathPrefix)
		if err != nil {
			return nil, err
		}
		if len(searchSystems) == 0 {
			return nil, nil
		}
	}

	groups := buildMediaSearchTypeGroups(searchSystems, qWords)
	if !mediaSearchTypeGroupsCacheable(groups) {
		return likeTitleGroupScopes(groups, qWords), nil
	}

	systemIDs := make([]string, len(searchSystems))
	for i := range searchSystems {
		systemIDs[i] = searchSystems[i].ID
	}
	cache := db.slugSearchCache.Load()
	if cache != nil && cache.CanServeSystems(systemIDs) {
		return cachedTitleGroupScopes(cache, groups, nil, filters), nil
	}
	if cached, viaSQL, ok := cache.PartitionServableSystems(systemIDs); ok && len(cached) > 0 {
		cachedGroups := buildMediaSearchTypeGroups(systemsByIDs(cached), qWords)
		sqlGroups := buildMediaSearchTypeGroups(systemsByIDs(viaSQL), qWords)
		return cachedTitleGroupScopes(cache, cachedGroups, likeTitleGroupScopes(sqlGroups, qWords), filters), nil
	}
	return likeTitleGroupScopes(groups, qWords), nil
}

func likeTitleGroupScopes(groups []mediaSearchTypeGroup, rawWords []string) []titleGroupScope {
	scopes := make([]titleGroupScope, 0, len(groups))
	for i := range groups {
		scopes = append(scopes, titleGroupScope{
			systems:       groups[i].systems,
			variantGroups: groups[i].variantGroups,
			rawWords:      rawWords,
			includeName:   groups[i].includeName,
		})
	}
	return scopes
}

// cachedTitleGroupScopes turns the slug cache's title candidates into title
// DBID scopes, chunked to stay under the SQLite parameter limit. fallback is
// appended for systems the cache could not answer.
func cachedTitleGroupScopes(
	cache *SlugSearchCache,
	groups []mediaSearchTypeGroup,
	fallback []titleGroupScope,
	filters *database.SearchFilters,
) []titleGroupScope {
	candidates := searchMediaTypeGroupsInCache(cache, groups)
	scopes := make([]titleGroupScope, 0, len(fallback)+1)
	if len(candidates) > 0 {
		perQuery := max(sqliteMaxParams-titleDBIDQueryParamCount(0, filters), 1)
		for start := 0; start < len(candidates); start += perQuery {
			scopes = append(scopes, titleGroupScope{
				titleDBIDs: candidates[start:min(start+perQuery, len(candidates))],
			})
		}
	}
	return append(scopes, fallback...)
}

func titleGroupSortExpr(sortOrder string) string {
	switch sortOrder {
	case "name-asc", "name-desc":
		return "MediaTitles.Name COLLATE NOCASE"
	default:
		return "MediaTitles.DBID"
	}
}

func compareTitleGroupKeys(a, b titleGroupKey, sortOrder string) int {
	switch sortOrder {
	case "name-asc":
		return cmp.Or(sqliteNoCaseCompare(a.sortValue, b.sortValue), cmp.Compare(a.titleDBID, b.titleDBID))
	case "name-desc":
		return -cmp.Or(sqliteNoCaseCompare(a.sortValue, b.sortValue), cmp.Compare(a.titleDBID, b.titleDBID))
	default:
		return cmp.Compare(a.titleDBID, b.titleDBID)
	}
}

// selectsMedia reports whether a tag filter list contains a filter that
// selects media rather than only excluding it.
func selectsMedia(tags []zapscript.TagFilter) bool {
	for i := range tags {
		if tags[i].Operator != zapscript.TagOperatorNOT {
			return true
		}
	}
	return false
}

// titleGroupKeysQuery builds the title-level statement for one scope. Title
// conditions (system, query words, letter, cursor) apply to MediaTitles
// directly; media conditions (path, tags, visibility) apply to the title's
// present media rows.
//
// A filter that selects media (a path range or a positive tag) resolves the
// matching media once and tests each title against that set. Otherwise every
// title matches unless excluded, so a correlated probe that stops at the
// title's first matching row is cheaper.
func titleGroupKeysQuery(
	scope *titleGroupScope,
	filters *database.SearchFilters,
) (query string, args []any, err error) {
	conditions := make([]string, 0, 8)

	if len(scope.titleDBIDs) > 0 {
		conditions = append(conditions,
			"MediaTitles.DBID IN ("+prepareVariadic("?", ",", len(scope.titleDBIDs))+")")
		for _, id := range scope.titleDBIDs {
			args = append(args, id)
		}
	} else if len(scope.systems) > 0 && !requestedAllSystems(scope.systems) {
		conditions = append(conditions,
			"Systems.SystemID IN ("+prepareVariadic("?", ",", len(scope.systems))+")")
		for i := range scope.systems {
			args = append(args, scope.systems[i].ID)
		}
	}

	for wordIdx, variants := range scope.variantGroups {
		orConditions := make([]string, 0, len(variants)*2+1)
		for _, variant := range variants {
			orConditions = append(orConditions, "MediaTitles.Slug LIKE ?", "MediaTitles.SecondarySlug LIKE ?")
			args = append(args, "%"+variant+"%", "%"+variant+"%")
		}
		if scope.includeName && wordIdx < len(scope.rawWords) && scope.rawWords[wordIdx] != "" {
			orConditions = append(orConditions, "MediaTitles.Name LIKE ?")
			args = append(args, "%"+scope.rawWords[wordIdx]+"%")
		}
		if len(orConditions) > 0 {
			conditions = append(conditions, "("+strings.Join(orConditions, " OR ")+")")
		}
	}

	letterClauses, letterArgs := BuildLetterFilterSQL(filters.Letter, "MediaTitles.Name")
	conditions = append(conditions, letterClauses...)
	args = append(args, letterArgs...)

	sortExpr := titleGroupSortExpr(filters.Sort)
	switch {
	case filters.SortCursor != nil && filters.Sort == "":
		conditions = append(conditions, "MediaTitles.DBID > ?")
		args = append(args, filters.SortCursor.LastID)
	case filters.SortCursor != nil && filters.Sort == "name-desc":
		conditions = append(conditions,
			sortExpr+" <= ? AND ("+sortExpr+" < ? OR MediaTitles.DBID < ?)")
		args = append(args, filters.SortCursor.SortValue, filters.SortCursor.SortValue, filters.SortCursor.LastID)
	case filters.SortCursor != nil:
		conditions = append(conditions,
			sortExpr+" >= ? AND ("+sortExpr+" > ? OR MediaTitles.DBID > ?)")
		args = append(args, filters.SortCursor.SortValue, filters.SortCursor.SortValue, filters.SortCursor.LastID)
	case filters.Sort != "":
		// Same planner floor as searchSortFloorCondition: lets the name sort
		// walk mediatitles_name_sort_idx instead of sorting every title.
		conditions = append(conditions, sortExpr+" >= ?")
		args = append(args, "")
	}

	mediaConditions := []string{"gm.IsMissing = 0"}
	var mediaArgs []any
	if filters.PathPrefix != "" {
		pathClause, pathArgs := browsePathPrefixCondition(
			"gm.Path", mediaRecursivePathPrefix(filters.PathPrefix))
		mediaConditions = append(mediaConditions, pathClause)
		mediaArgs = append(mediaArgs, pathArgs...)
	}
	if filters.PathPrefix != "" || selectsMedia(filters.Tags) {
		tagClauses, tagArgs := buildTagFilterSQLForRef(filters.Tags, "gm")
		mediaConditions = append(mediaConditions, tagClauses...)
		mediaArgs = append(mediaArgs, tagArgs...)
		conditions = append(conditions,
			"MediaTitles.DBID IN (SELECT gm.MediaTitleDBID FROM Media gm WHERE "+
				strings.Join(mediaConditions, " AND ")+")")
	} else {
		tagClauses, tagArgs := buildCandidateTagFilterSQLForRef(filters.Tags, "gm")
		mediaConditions = append(mediaConditions, tagClauses...)
		mediaArgs = append(mediaArgs, tagArgs...)
		conditions = append(conditions,
			"EXISTS (SELECT 1 FROM Media gm WHERE gm.MediaTitleDBID = MediaTitles.DBID AND "+
				strings.Join(mediaConditions, " AND ")+")")
	}
	args = append(args, mediaArgs...)

	order := sortExpr + " ASC, MediaTitles.DBID ASC"
	if filters.Sort == "name-desc" {
		order = sortExpr + " DESC, MediaTitles.DBID DESC"
	}
	if filters.Sort == "" {
		order = "MediaTitles.DBID ASC"
	}

	query = `
		SELECT MediaTitles.DBID, ` + sortExpr + `
		FROM MediaTitles
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY ` + order + `
		LIMIT ?`
	args = append(args, filters.Limit)
	if len(args) > sqliteMaxParams {
		return "", nil, fmt.Errorf("grouped search needs %d query parameters", len(args))
	}
	return query, args, nil
}

func sqlSearchTitleGroupKeys(
	ctx context.Context,
	db sqlQueryable,
	scope *titleGroupScope,
	filters *database.SearchFilters,
) ([]titleGroupKey, error) {
	query, args, err := titleGroupKeysQuery(scope, filters)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query grouped search titles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := make([]titleGroupKey, 0, min(filters.Limit, 100))
	for rows.Next() {
		var key titleGroupKey
		if scanErr := rows.Scan(&key.titleDBID, &key.sortValue); scanErr != nil {
			return nil, fmt.Errorf("failed to scan grouped search title: %w", scanErr)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("grouped search title rows: %w", err)
	}
	return keys, nil
}
