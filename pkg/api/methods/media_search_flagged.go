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
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	tagfilters "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
)

// flaggedSearchRow is one result of a search that requires a user flag. A
// multi-disc folder is one result however many of its discs hold the flag:
// it shows and launches the disc the folder launches in browse, and keeps
// its place in the order by its first matching disc, which does not move when
// another disc is played.
type flaggedSearchRow struct {
	// set is the disc set the row stands for, or nil for a single game.
	set *discSet
	// row is the matching row as read: the set's first matching disc.
	row database.SearchResultWithCursor
	// result is what the search shows.
	result database.SearchResultWithCursor
	// key is the row's place in the order, which its cursor carries.
	key database.SearchResultWithCursor
}

// discFolder names a multi-disc folder candidate of one system.
type discFolder struct {
	systemID string
	dir      string
}

// searchFlagged answers a search that requires a user flag, folding each disc
// set into one result. A name, filename or unsorted search reads its pages in
// order as any other search does and shows a set at its first matching disc.
// An added-desc search, addedFlag set to the flag it orders by, reads every
// match without tags, orders them and reads tags for the page it returns.
// limit is the page size plus one, as for every other search.
func searchFlagged(
	ctx context.Context,
	env *requests.RequestEnv,
	addedFlag database.MediaUserFlag,
	filters *database.SearchFilters,
	cursor *int64,
	sortCursor *database.SearchCursor,
	limit int,
) ([]flaggedSearchRow, error) {
	var rows []flaggedSearchRow
	var err error
	if addedFlag != "" {
		rows, err = searchFlaggedByAdded(ctx, env, addedFlag, filters, sortCursor, limit)
	} else {
		rows, err = searchFlaggedInOrder(ctx, env, filters, cursor, sortCursor, limit)
	}
	if err != nil {
		return nil, err
	}
	// A search may leave hidden games out by asking for it in its tags too,
	// and a set must then not launch a hidden disc.
	excludeHidden := filters.ExcludeHidden ||
		slices.Equal(tagfilters.ExcludeHidden(filters.Tags), filters.Tags)
	for i := range rows {
		rows[i].result = rows[i].row
		if rows[i].set != nil {
			rows[i].result = discSetResult(&rows[i].row, rows[i].set, excludeHidden)
		}
	}
	return rows, nil
}

// searchFlaggedInOrder reads pages after the cursor until limit results are
// folded or the matches run out. A disc of a set is shown only when it is the
// set's first matching disc in the order; a later one was or will be shown
// with it.
func searchFlaggedInOrder(
	ctx context.Context,
	env *requests.RequestEnv,
	filters *database.SearchFilters,
	cursor *int64,
	sortCursor *database.SearchCursor,
	limit int,
) ([]flaggedSearchRow, error) {
	read := *filters
	read.Cursor = cursor
	read.SortCursor = sortCursor
	read.Limit = limit
	sets := make(map[discFolder]*discSet)
	looked := make(map[discFolder]struct{})
	placed := make(map[*discSet]struct{})
	out := make([]flaggedSearchRow, 0, limit)
	for {
		rows, err := env.Database.MediaDB.SearchMediaWithFilters(ctx, &read)
		if err != nil {
			return nil, fmt.Errorf("error searching media with filters: %w", err)
		}
		if resolveErr := resolveRowDiscSets(env, rows, sets, looked); resolveErr != nil {
			return nil, resolveErr
		}
		firsts, err := firstMatchingDiscs(ctx, env, filters, rows, sets)
		if err != nil {
			return nil, err
		}
		for i := range rows {
			set := discSetOfRow(sets, &rows[i])
			if set != nil {
				if _, done := placed[set]; done {
					continue
				}
				if first, known := firsts[set]; known && first != rows[i].MediaID {
					continue
				}
				placed[set] = struct{}{}
			}
			out = append(out, flaggedSearchRow{row: rows[i], key: rows[i], set: set})
			if len(out) == limit {
				return out, nil
			}
		}
		if len(rows) < read.Limit {
			return out, nil
		}
		last := rows[len(rows)-1]
		if filters.Sort == "" {
			lastID := last.MediaID
			read.Cursor = &lastID
		} else {
			read.SortCursor = &database.SearchCursor{
				SortValue: last.SortValue, Sort: filters.Sort, LastID: last.MediaID,
			}
		}
	}
}

// firstMatchingDiscs finds, for each set a row of rows is a disc of, which of
// its discs matching the search comes first in the order. A row that comes
// before every other disc of its set is first without a lookup; the others
// are settled by one search of their sets' titles, which every disc of a set
// shares, so the title-level parts of the search match them all alike.
func firstMatchingDiscs(
	ctx context.Context,
	env *requests.RequestEnv,
	filters *database.SearchFilters,
	rows []database.SearchResultWithCursor,
	sets map[discFolder]*discSet,
) (map[*discSet]int64, error) {
	firsts := make(map[*discSet]int64)
	var titleIDs []int64
	pending := make(map[*discSet]struct{})
	for i := range rows {
		set := discSetOfRow(sets, &rows[i])
		if set == nil {
			continue
		}
		if _, seen := firsts[set]; seen {
			continue
		}
		if _, seen := pending[set]; seen {
			continue
		}
		if discLeadsSet(filters.Sort, &rows[i], set) {
			firsts[set] = rows[i].MediaID
			continue
		}
		pending[set] = struct{}{}
		if !slices.Contains(titleIDs, rows[i].MediaTitleID) {
			titleIDs = append(titleIDs, rows[i].MediaTitleID)
		}
	}
	if len(pending) == 0 {
		return firsts, nil
	}

	members := *filters
	members.TitleDBID = 0
	members.TitleDBIDs = titleIDs
	members.SkipTags = true
	matches, err := readWholeSearch(ctx, env.Database.MediaDB, &members, filters.Sort)
	if err != nil {
		return nil, err
	}
	best := make(map[*discSet]*database.SearchResultWithCursor, len(pending))
	for set := range pending {
		for i := range matches {
			if !slices.ContainsFunc(set.discs, func(m database.Media) bool { return m.DBID == matches[i].MediaID }) {
				continue
			}
			if current := best[set]; current == nil ||
				mediadb.CompareSearchResults(&matches[i], current, filters.Sort) < 0 {
				best[set] = &matches[i]
			}
		}
		if found := best[set]; found != nil {
			firsts[set] = found.MediaID
		}
	}
	return firsts, nil
}

// discLeadsSet reports whether row comes before every other disc of set in
// the search order. The discs share a title, so a name order falls to their
// IDs.
func discLeadsSet(sortOrder string, row *database.SearchResultWithCursor, set *discSet) bool {
	for i := range set.discs {
		disc := database.SearchResultWithCursor{SortValue: row.SortValue, MediaID: set.discs[i].DBID}
		if sortOrder == "filename-asc" || sortOrder == "filename-desc" {
			disc.SortValue = set.discs[i].Path
		}
		if disc.MediaID != row.MediaID && mediadb.CompareSearchResults(&disc, row, sortOrder) < 0 {
			return false
		}
	}
	return true
}

// searchFlaggedByAdded reads every match without tags, folds each set into one
// row, orders the rows by when they got flag and returns the page after the
// cursor with its tags.
func searchFlaggedByAdded(
	ctx context.Context,
	env *requests.RequestEnv,
	flag database.MediaUserFlag,
	filters *database.SearchFilters,
	sortCursor *database.SearchCursor,
	limit int,
) ([]flaggedSearchRow, error) {
	read := *filters
	read.SkipTags = true
	all, err := readWholeSearch(ctx, env.Database.MediaDB, &read, "name-asc")
	if err != nil {
		return nil, err
	}
	merged, err := mergeDiscSetResults(env, all)
	if err != nil {
		return nil, err
	}
	if merged, err = orderByAdded(env.Database, flag, merged); err != nil {
		return nil, err
	}
	start := 0
	if sortCursor != nil {
		cursorAt, cursorName, parseErr := parseAddedSortValue(sortCursor.SortValue)
		if parseErr != nil {
			return nil, parseErr
		}
		start = slices.IndexFunc(merged, func(row flaggedSearchRow) bool {
			at, name, _ := parseAddedSortValue(row.key.SortValue)
			return compareAdded(at, name, row.key.MediaID, cursorAt, cursorName, sortCursor.LastID) > 0
		})
	}
	if start < 0 {
		return nil, nil
	}
	page := merged[start:min(start+limit, len(merged))]
	tagged := make([]database.SearchResultWithCursor, len(page))
	for i := range page {
		tagged[i] = page[i].row
	}
	if err := env.Database.MediaDB.AttachSearchTags(ctx, tagged); err != nil {
		return nil, fmt.Errorf("failed to read search tags: %w", err)
	}
	for i := range page {
		page[i].row = tagged[i]
	}
	return page, nil
}

// readWholeSearch reads every match of filters in sortOrder, in pages, up to
// the bound the added-desc search keeps.
func readWholeSearch(
	ctx context.Context, mediaDB database.MediaDBI, filters *database.SearchFilters, sortOrder string,
) ([]database.SearchResultWithCursor, error) {
	read := *filters
	read.Sort = sortOrder
	read.Cursor = nil
	read.SortCursor = nil
	read.Limit = addedSearchPage
	rows := make([]database.SearchResultWithCursor, 0)
	for page := 0; ; page++ {
		results, err := mediaDB.SearchMediaWithFilters(ctx, &read)
		if err != nil {
			return nil, fmt.Errorf("error searching media with filters: %w", err)
		}
		rows = append(rows, results...)
		if len(results) < read.Limit {
			return rows, nil
		}
		if page+1 >= addedSearchMaxPages {
			log.Warn().Int("rows", len(rows)).Msg("flagged search stopped at its read limit")
			return rows, nil
		}
		last := results[len(results)-1]
		if sortOrder == "" {
			lastID := last.MediaID
			read.Cursor = &lastID
		} else {
			read.SortCursor = &database.SearchCursor{SortValue: last.SortValue, Sort: sortOrder, LastID: last.MediaID}
		}
	}
}

// resolveRowDiscSets resolves the disc sets of the folders of rows not looked
// at yet, adding them to sets.
func resolveRowDiscSets(
	env *requests.RequestEnv,
	rows []database.SearchResultWithCursor,
	sets map[discFolder]*discSet,
	looked map[discFolder]struct{},
) error {
	var systemOrder []string
	dirsBySystem := make(map[string][]string)
	for i := range rows {
		if !container.MayHaveContainerTarget(rows[i].Path) {
			continue
		}
		key := discFolder{systemID: rows[i].SystemID, dir: container.ParentDir(rows[i].Path)}
		if _, seen := looked[key]; seen {
			continue
		}
		looked[key] = struct{}{}
		if _, seen := dirsBySystem[key.systemID]; !seen {
			systemOrder = append(systemOrder, key.systemID)
		}
		dirsBySystem[key.systemID] = append(dirsBySystem[key.systemID], key.dir)
	}
	for _, systemID := range systemOrder {
		system, err := env.Database.MediaDB.FindSystemBySystemID(systemID)
		if err != nil {
			return fmt.Errorf("failed to resolve system %s: %w", systemID, err)
		}
		found, err := resolveDiscSets(env, systemID, system.DBID, dirsBySystem[systemID])
		if err != nil {
			return err
		}
		for dir, set := range found {
			sets[discFolder{systemID: systemID, dir: dir}] = set
		}
	}
	return nil
}

// discSetOfRow returns the set row is a disc of, or nil.
func discSetOfRow(sets map[discFolder]*discSet, row *database.SearchResultWithCursor) *discSet {
	if !container.MayHaveContainerTarget(row.Path) {
		return nil
	}
	set := sets[discFolder{systemID: row.SystemID, dir: container.ParentDir(row.Path)}]
	if set == nil || !slices.ContainsFunc(set.discs, func(m database.Media) bool { return m.DBID == row.MediaID }) {
		return nil
	}
	return set
}

// mergeDiscSetResults folds the discs of each multi-disc folder in rows, every
// match of a search, into one row at the place of the first of them, keeping
// rows' order.
func mergeDiscSetResults(
	env *requests.RequestEnv, rows []database.SearchResultWithCursor,
) ([]flaggedSearchRow, error) {
	sets := make(map[discFolder]*discSet)
	if err := resolveRowDiscSets(env, rows, sets, make(map[discFolder]struct{})); err != nil {
		return nil, err
	}
	merged := make([]flaggedSearchRow, 0, len(rows))
	placed := make(map[*discSet]struct{})
	for i := range rows {
		set := discSetOfRow(sets, &rows[i])
		if set != nil {
			if _, done := placed[set]; done {
				continue
			}
			placed[set] = struct{}{}
		}
		merged = append(merged, flaggedSearchRow{row: rows[i], key: rows[i], set: set})
	}
	return merged, nil
}

// discSetResult is the result a disc set shows, built on one of its matching
// rows. A launch disc the search may not show, because it is hidden, leaves
// the row's own disc in place with the set's user tags.
func discSetResult(
	row *database.SearchResultWithCursor, set *discSet, excludeHidden bool,
) database.SearchResultWithCursor {
	result := *row
	alias := &set.alias
	if excludeHidden && mediaTagsHidden(alias.Tags) {
		result.Tags = database.MergeDiscSetUserTags(row.Tags, alias.Tags)
		return result
	}
	result.Path = alias.Row.Path
	result.MediaID = alias.Row.DBID
	result.MediaTitleID = alias.Row.Title.DBID
	result.DisambiguationTypes = alias.Row.Title.DisambiguationTypes
	result.Tags = alias.Tags
	result.ZapScriptTags = alias.ZapScriptTags
	result.HasCover = alias.HasCover
	return result
}

// orderByAdded orders rows read in name order by when each got flag, newest
// first. A disc set counts from the first of its discs to get it. Each row's
// key takes the added-desc sort value its cursor carries.
func orderByAdded(
	db *database.Database, flag database.MediaUserFlag, rows []flaggedSearchRow,
) ([]flaggedSearchRow, error) {
	if db.UserDB == nil {
		return nil, errors.New("user database is not available")
	}
	times, err := db.UserDB.ListMediaUserFlagTimes(flag)
	if err != nil {
		return nil, fmt.Errorf("failed to read when games were added: %w", err)
	}
	setAt := make(map[string]int64, len(times))
	for i := range times {
		setAt[times[i].SystemID+"\x00"+pathutil.CanonicalMediaPath(times[i].Path)] = times[i].SetAt
	}
	at := make([]int64, len(rows))
	for i := range rows {
		paths := []string{rows[i].key.Path}
		if rows[i].set != nil {
			for j := range rows[i].set.discs {
				paths = append(paths, rows[i].set.discs[j].Path)
			}
		}
		for _, path := range paths {
			if t := setAt[rows[i].key.SystemID+"\x00"+path]; t > 0 && (at[i] == 0 || t < at[i]) {
				at[i] = t
			}
		}
		rows[i].key.SortValue = addedSortValue(at[i], rows[i].key.SortValue)
	}
	slices.SortStableFunc(rows, func(a, b flaggedSearchRow) int {
		aAt, aName, _ := parseAddedSortValue(a.key.SortValue)
		bAt, bName, _ := parseAddedSortValue(b.key.SortValue)
		return compareAdded(aAt, aName, a.key.MediaID, bAt, bName, b.key.MediaID)
	})
	return rows, nil
}
