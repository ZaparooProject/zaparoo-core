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

package methods

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

const (
	mediaVersionsCursorVersion = 1
	mediaVersionsDefaultLimit  = 100
)

// foldLinkedAliases drops rows that are symlink aliases of another row in the
// set. An alias is the same file under a second folder, not another version.
// An alias whose target is not in the set stands for it and stays.
func foldLinkedAliases(
	ctx context.Context, mediaDB database.MediaDBI, rows []database.SearchResultWithCursor,
) ([]database.SearchResultWithCursor, error) {
	if len(rows) < 2 {
		return rows, nil
	}
	ids := make([]int64, len(rows))
	present := make(map[int64]struct{}, len(rows))
	for i := range rows {
		ids[i] = rows[i].MediaID
		present[rows[i].MediaID] = struct{}{}
	}
	targets, err := mediaDB.GetMediaLinkTargets(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("look up linked media: %w", err)
	}
	if len(targets) == 0 {
		return rows, nil
	}
	folded := make([]database.SearchResultWithCursor, 0, len(rows))
	for i := range rows {
		if target, linked := targets[rows[i].MediaID]; linked {
			if _, ok := present[target]; ok {
				continue
			}
		}
		folded = append(folded, rows[i])
	}
	return folded, nil
}

// foldVersions reduces media rows to versions: the members of a container
// become the row it launches, and an alias gives way to its target. Rows keep
// their input order.
func foldVersions(
	ctx context.Context, mediaDB database.MediaDBI, rows []database.SearchResultWithCursor,
) ([]database.SearchResultWithCursor, error) {
	versions, err := foldContainerVersions(ctx, mediaDB, rows)
	if err != nil {
		return nil, err
	}
	return foldLinkedAliases(ctx, mediaDB, versions)
}

// versionCountFilters keeps the parts of a search that decide which versions
// a count may include: what is visible, and the tag and path conditions.
func versionCountFilters(filters *database.SearchFilters) *database.SearchFilters {
	if filters == nil {
		return &database.SearchFilters{}
	}
	return &database.SearchFilters{
		ExcludeHidden: filters.ExcludeHidden,
		Tags:          filters.Tags,
		PathPrefix:    filters.PathPrefix,
	}
}

// titleVersionCounts returns how many versions each title's game has: the
// versions of the title and of every title it shares a version group with.
// linkedOnly limits the work, and the result, to titles that share a group
// with another title, for a caller that already counted each title's own rows.
//
// Rows are counted in the database, and only a game holding a row that might
// fold into another (a container member or a linked alias) has its rows read
// and folded. On a device each row returned costs far more than the query
// that finds it, so most of a page is answered from one short row per title.
func titleVersionCounts(
	ctx context.Context,
	mediaDB database.MediaDBI,
	titleIDs []int64,
	filters *database.SearchFilters,
	linkedOnly bool,
) (map[int64]int, error) {
	if len(titleIDs) == 0 {
		return map[int64]int{}, nil
	}
	members, err := mediaDB.GetVersionGroupMembers(ctx, titleIDs)
	if err != nil {
		return nil, fmt.Errorf("look up version groups: %w", err)
	}
	if linkedOnly && len(members) == 0 {
		return map[int64]int{}, nil
	}

	// games holds each counted title's game: its version group, or itself.
	games := make(map[int64][]int64, len(titleIDs))
	seen := make(map[int64]struct{}, len(titleIDs))
	var countTitles []int64
	for _, id := range titleIDs {
		game, linked := members[id]
		if !linked {
			if linkedOnly {
				continue
			}
			game = []int64{id}
		}
		games[id] = game
		for _, member := range game {
			if _, ok := seen[member]; !ok {
				seen[member] = struct{}{}
				countTitles = append(countTitles, member)
			}
		}
	}

	countFilters := versionCountFilters(filters)
	rowCounts, err := mediaDB.CountTitleVersionRows(ctx, countTitles, countFilters)
	if err != nil {
		return nil, fmt.Errorf("count title media: %w", err)
	}

	// A fold needs two rows of a game and one of them foldable. Any other
	// game's versions are its rows.
	folded := make(map[int64]struct{})
	var readTitles []int64
	for _, id := range titleIDs {
		game, ok := games[id]
		if !ok {
			continue
		}
		rows, foldable := 0, 0
		for _, member := range game {
			rows += rowCounts[member].Rows
			foldable += rowCounts[member].Foldable
		}
		if rows < 2 || foldable == 0 {
			continue
		}
		for _, member := range game {
			if _, ok := folded[member]; !ok {
				folded[member] = struct{}{}
				readTitles = append(readTitles, member)
			}
		}
	}

	perTitle := make(map[int64]int, len(countTitles))
	for _, member := range countTitles {
		if _, ok := folded[member]; !ok {
			perTitle[member] = rowCounts[member].Rows
		}
	}
	if len(readTitles) > 0 {
		rows, rowsErr := mediaDB.GetTitleVersionRows(ctx, readTitles, countFilters)
		if rowsErr != nil {
			return nil, fmt.Errorf("read version rows: %w", rowsErr)
		}
		versions, foldErr := foldVersions(ctx, mediaDB, rows)
		if foldErr != nil {
			return nil, foldErr
		}
		for i := range versions {
			perTitle[versions[i].MediaTitleID]++
		}
	}

	counts := make(map[int64]int, len(games))
	for id, game := range games {
		total := 0
		for _, member := range game {
			total += perTitle[member]
		}
		counts[id] = total
	}
	return counts, nil
}

// versionGroupTitles returns the titles whose rows are versions of titleID's
// game: its version group, or the title alone.
func versionGroupTitles(ctx context.Context, mediaDB database.MediaDBI, titleID int64) ([]int64, error) {
	members, err := mediaDB.GetVersionGroupMembers(ctx, []int64{titleID})
	if err != nil {
		return nil, fmt.Errorf("look up version group: %w", err)
	}
	if group := members[titleID]; len(group) > 0 {
		return group, nil
	}
	return []int64{titleID}, nil
}

// attachBrowseVersionCounts sets each media entry's versionCount from the
// counts the last regroup stored. Only a game holding a hidden version, which
// the stored count includes, is counted live. An entry whose system has not
// been regrouped since it was indexed gets no count. It is an enrichment like
// cover colours: a slow or failed lookup leaves the field out rather than
// failing the page.
func attachBrowseVersionCounts(
	env *requests.RequestEnv,
	entries []models.BrowseEntry,
	files []database.SearchResultWithCursor,
) {
	if env == nil || env.Database == nil || env.Database.MediaDB == nil || len(files) == 0 {
		return
	}
	titleByMedia := make(map[int64]int64, len(files))
	titleIDs := make([]int64, 0, len(files))
	seen := make(map[int64]struct{}, len(files))
	for i := range files {
		titleID := files[i].MediaTitleID
		if files[i].MediaID <= 0 || titleID <= 0 {
			continue
		}
		titleByMedia[files[i].MediaID] = titleID
		if _, ok := seen[titleID]; !ok {
			seen[titleID] = struct{}{}
			titleIDs = append(titleIDs, titleID)
		}
	}
	if len(titleIDs) == 0 {
		return
	}
	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	stored, err := env.Database.MediaDB.GetStoredVersionCounts(ctx, titleIDs, env.ExcludeHidden)
	if err != nil {
		log.Debug().Err(err).Msg("could not enrich browse version counts")
		return
	}
	counts := stored.Counts
	if len(stored.Recount) > 0 {
		live, liveErr := titleVersionCounts(
			ctx, env.Database.MediaDB, stored.Recount, &database.SearchFilters{ExcludeHidden: true}, false)
		if liveErr != nil {
			log.Debug().Err(liveErr).Msg("could not count browse versions with hidden members")
		} else {
			for _, titleID := range stored.Recount {
				counts[titleID] = live[titleID]
			}
		}
	}
	for i := range entries {
		titleID, ok := titleByMedia[entries[i].MediaID]
		if !ok || entries[i].Type != "media" {
			continue
		}
		known, ok := counts[titleID]
		if !ok {
			continue
		}
		// An entry is always one of its own game's versions, even when the
		// count could not see it, such as a hidden file browsed to directly.
		count := max(known, 1)
		entries[i].VersionCount = &count
	}
}

// versionSources returns, for each title, the kinds of key it shares with
// another title of the group, sorted.
func versionSources(keys map[int64][]database.VersionKey) map[int64][]string {
	holders := make(map[database.VersionKey]int)
	for _, titleKeys := range keys {
		for _, key := range titleKeys {
			holders[key]++
		}
	}
	sources := make(map[int64][]string, len(keys))
	for title, titleKeys := range keys {
		for _, key := range titleKeys {
			if holders[key] > 1 && !slices.Contains(sources[title], key.Type) {
				sources[title] = append(sources[title], key.Type)
			}
		}
		slices.Sort(sources[title])
	}
	return sources
}

// orderVersions puts the versions that share the asked-about title first, led
// by the one a launch of that title would pick, then the other titles' by
// name. Within a title, rows are ordered by path.
func orderVersions(
	env *requests.RequestEnv, versions []database.SearchResultWithCursor, titleID int64,
) []database.SearchResultWithCursor {
	var defaultID int64
	own := make([]database.SearchResultWithCursor, 0, len(versions))
	for i := range versions {
		if versions[i].MediaTitleID == titleID {
			own = append(own, versions[i])
		}
	}
	if len(own) > 1 {
		defaultID = titles.SelectDefaultVersion(own, env.Config, systemLaunchers(env, own[0].SystemID)).MediaID
	}
	rank := func(row *database.SearchResultWithCursor) int {
		switch {
		case row.MediaTitleID != titleID:
			return 2
		case row.MediaID == defaultID:
			return 0
		default:
			return 1
		}
	}
	ordered := slices.Clone(versions)
	slices.SortStableFunc(ordered, func(a, b database.SearchResultWithCursor) int {
		if order := cmp.Compare(rank(&a), rank(&b)); order != 0 {
			return order
		}
		if a.MediaTitleID != b.MediaTitleID {
			return cmp.Or(
				cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
				cmp.Compare(a.MediaTitleID, b.MediaTitleID),
			)
		}
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.MediaID, b.MediaID))
	})
	return ordered
}

// pageVersions returns the versions after the cursor's row, up to limit, and
// whether more follow.
func pageVersions(
	versions []database.SearchResultWithCursor, cursor string, limit int,
) (page []database.SearchResultWithCursor, hasNext bool, err error) {
	start := 0
	data, err := decodeCursorData(cursor)
	if err != nil {
		return nil, false, models.ClientErrf("invalid cursor: %w", err)
	}
	if data != nil {
		if data.Version != mediaVersionsCursorVersion {
			return nil, false, models.ClientErrf("invalid cursor: not a media.versions cursor")
		}
		index := slices.IndexFunc(versions, func(row database.SearchResultWithCursor) bool {
			return row.MediaID == data.LastID
		})
		if index < 0 {
			return nil, false, models.ClientErrf("invalid cursor: the version it points at is gone")
		}
		start = index + 1
	}
	end := min(start+limit, len(versions))
	return versions[start:end], end < len(versions), nil
}

// HandleMediaVersions lists the versions of one media item's game: the rows
// of its title and of every title a source declared to be the same game.
//
//nolint:gocritic // single-use parameter in API handler
func HandleMediaVersions(env requests.RequestEnv) (any, error) {
	var params models.MediaVersionsParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	ref := mediaRefParam{MediaID: params.MediaID, System: params.System, Path: params.Path}
	if err := validateMediaRef(ref); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil, errors.New("media database is not available")
	}
	limit := mediaVersionsDefaultLimit
	if params.MaxResults != nil {
		limit = *params.MaxResults
	}
	var cursor string
	if params.Cursor != nil {
		cursor = *params.Cursor
	}

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}
	current := resolved[0].Row
	ctx := env.Context
	mediaDB := env.Database.MediaDB

	titleIDs, err := versionGroupTitles(ctx, mediaDB, current.Title.DBID)
	if err != nil {
		return nil, err
	}
	rows, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		TitleDBIDs:    titleIDs,
		ExcludeHidden: params.IncludeHidden == nil || !*params.IncludeHidden,
		Limit:         titleVersionsLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("error reading versions: %w", err)
	}
	if len(rows) >= titleVersionsLimit {
		return nil, fmt.Errorf("media %d has more than %d versions", current.DBID, titleVersionsLimit-1)
	}
	versions, err := foldVersions(ctx, mediaDB, rows)
	if err != nil {
		return nil, fmt.Errorf("error folding versions: %w", err)
	}

	// An alias folds into its target, so asking about one marks the target.
	currentID := current.DBID
	if !slices.ContainsFunc(versions, func(row database.SearchResultWithCursor) bool {
		return row.MediaID == currentID
	}) {
		if target, linked, linkErr := mediaDB.GetMediaLinkTarget(ctx, currentID); linkErr != nil {
			return nil, fmt.Errorf("error reading linked media: %w", linkErr)
		} else if linked {
			currentID = target
		}
	}

	var sources map[int64][]string
	if len(titleIDs) > 1 {
		keys, keysErr := mediaDB.GetVersionKeysByTitleDBIDs(ctx, titleIDs)
		if keysErr != nil {
			return nil, fmt.Errorf("error reading version keys: %w", keysErr)
		}
		sources = versionSources(keys)
	}

	ordered := orderVersions(&env, versions, current.Title.DBID)
	page, hasNext, err := pageVersions(ordered, cursor, limit)
	if err != nil {
		return nil, err
	}

	results := newSearchResultBuilder(&env).build(ctx, page)
	response := models.MediaVersionsResponse{
		Versions: make([]models.MediaVersion, 0, len(results)),
		Total:    len(ordered),
	}
	for i := range results {
		version := models.MediaVersion{
			SearchResultMedia: results[i],
			Reason:            models.VersionReasonTitle,
			Current:           page[i].MediaID == currentID,
		}
		if page[i].MediaTitleID != current.Title.DBID {
			version.Reason = models.VersionReasonLink
			version.Sources = sources[page[i].MediaTitleID]
		}
		response.Versions = append(response.Versions, version)
	}
	if len(page) > 0 {
		response.Pagination = &models.PaginationInfo{HasNextPage: hasNext, PageSize: limit}
		if hasNext {
			next, cursorErr := encodeMediaSearchCursorData(cursorData{
				Version: mediaVersionsCursorVersion, LastID: page[len(page)-1].MediaID,
			})
			if cursorErr != nil {
				return nil, fmt.Errorf("failed to generate next page cursor: %w", cursorErr)
			}
			response.Pagination.NextCursor = &next
		}
	}
	return response, nil
}
