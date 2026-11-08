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
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// browseAnchorRow is the row an anchor resolved to: the keyset the listing
// orders it by.
type browseAnchorRow struct {
	sortValue string
	dbid      int64
}

// browseAnchorBelow returns the part of path below prefix. An anchor outside the
// prefix, or equal to it, has no place in the listing.
func browseAnchorBelow(path, prefix string) (rest string, ok bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest = strings.TrimPrefix(path, prefix)
	if strings.TrimRight(rest, "/") == "" {
		return "", false
	}
	return rest, true
}

// browseAnchorDirName is the directory of the listing that holds rest: its first
// segment. A saved path may name the folder itself or a file deeper below it,
// because a folder collapsed to a single game launches a file but stays listed
// as the folder.
func browseAnchorDirName(rest string) string {
	name, _, _ := strings.Cut(rest, "/")
	return name
}

// sqlBrowseAnchorPosition locates a path in the listing opts describes. A file
// comes first because a path holding one is what most anchors are, and it costs
// one row lookup and one ranged count against reading the directory listing.
func sqlBrowseAnchorPosition(
	ctx context.Context, db sqlQueryable, opts *database.BrowseAnchorOptions,
) (database.BrowseAnchorResult, error) {
	var (
		position int
		found    bool
		err      error
		dirNames []string
	)
	if sources := browseOverlaySources(opts.Overlay); len(sources) > 0 {
		position, found, err = sqlBrowseOverlayFileAnchor(ctx, db, opts)
		for i := range sources {
			if rest, ok := browseAnchorBelow(opts.Path, sources[i].PathPrefix); ok && sources[i].IncludeDirs {
				dirNames = append(dirNames, browseAnchorDirName(rest))
			}
		}
	} else {
		rest, ok := browseAnchorBelow(opts.Path, opts.PathPrefix)
		if !ok {
			return database.BrowseAnchorResult{}, nil
		}
		position, found, err = sqlBrowseFileAnchor(ctx, db, opts)
		if opts.ListDirectories {
			dirNames = []string{browseAnchorDirName(rest)}
		}
	}
	if err != nil {
		return database.BrowseAnchorResult{}, err
	}
	if found {
		return database.BrowseAnchorResult{Found: true, Position: position}, nil
	}
	if len(dirNames) == 0 {
		return database.BrowseAnchorResult{}, nil
	}
	return sqlBrowseDirectoryAnchor(ctx, db, opts, dirNames)
}

// sqlBrowseDirectoryAnchor numbers the listing's directories the way the listing
// does: every directory, through the routing and hidden-folder rules that decide
// what it shows, in its own order. A merged listing shows one entry per name
// whichever route supplied it, so the match is by name.
func sqlBrowseDirectoryAnchor(
	ctx context.Context, db sqlQueryable, opts *database.BrowseAnchorOptions, names []string,
) (database.BrowseAnchorResult, error) {
	dirs, err := sqlBrowseDirectoryRows(ctx, db, database.BrowseDirectoriesOptions{
		ExcludeHidden: opts.ExcludeHidden,
		PathPrefix:    opts.PathPrefix,
		Overlay:       opts.Overlay,
		Systems:       opts.Systems,
	})
	if err != nil {
		return database.BrowseAnchorResult{}, fmt.Errorf("browse anchor directories: %w", err)
	}
	for i := range dirs {
		if slices.Contains(names, dirs[i].Name) {
			return database.BrowseAnchorResult{Found: true, IsDirectory: true, Position: i}, nil
		}
	}
	return database.BrowseAnchorResult{}, nil
}

// browseAnchorFilesOptions is the listing's own file options for an anchor.
func browseAnchorFilesOptions(opts *database.BrowseAnchorOptions, prefix string) *database.BrowseFilesOptions {
	return &database.BrowseFilesOptions{
		ExcludeHidden: opts.ExcludeHidden,
		PathPrefix:    prefix,
		Sort:          opts.Sort,
		Systems:       opts.Systems,
		Tags:          opts.Tags,
	}
}

// browseAnchorSystemsJoin joins Systems only when the filter reads it, so a count
// over the title sort stays covered by idx_media_browse_sort and never touches
// the table.
func browseAnchorSystemsJoin(opts *database.BrowseFilesOptions) string {
	if len(opts.Systems) == 0 {
		return ""
	}
	return ` INNER JOIN Systems s ON m.SystemDBID = s.DBID`
}

// browseFileAnchorRowQuery selects one path through the listing's own filter.
// Path can repeat across systems, so the first in listing order stands for all
// of them. The unary plus keeps ParentDir away from the planner: the path index
// finds this row directly, while idx_media_browse_sort would read the partition.
func browseFileAnchorRowQuery(
	opts *database.BrowseFilesOptions, path, sortExpr, order string, tagPlan browseTagPlan,
) (query string, args []any) {
	where, filterArgs := browseFilesFilterCondition(opts, false, tagPlan)
	args = append(args, path, opts.PathPrefix)
	args = append(args, filterArgs...)
	return `SELECT m.DBID, ` + sortExpr + `
		FROM Media m` + browseAnchorSystemsJoin(opts) + `
		WHERE m.Path = ? AND +m.ParentDir = ? AND ` + where + `
		ORDER BY ` + order + `
		LIMIT 1`, args
}

func scanBrowseAnchorRow(
	ctx context.Context, db sqlQueryable, query string, args []any,
) (row browseAnchorRow, found bool, err error) {
	err = db.QueryRowContext(ctx, query, args...).Scan(&row.dbid, &row.sortValue)
	if errors.Is(err, sql.ErrNoRows) {
		return browseAnchorRow{}, false, nil
	}
	if err != nil {
		return browseAnchorRow{}, false, fmt.Errorf("browse anchor row: %w", err)
	}
	return row, true, nil
}

// browseBeforeCondition selects the rows a listing returns ahead of a keyset:
// browseCursorCondition facing the other way, with the same redundant bound so
// the count is an index range rather than a read of the partition.
func browseBeforeCondition(sortOrder string) string {
	expr := browseSortExpr(sortOrder)
	switch sortOrder {
	case "name-desc", "filename-desc", browseSortRankPrefixDesc, browseSortDatePrefixDesc:
		return browseOverlayCursorCondition(expr, ">")
	default:
		return browseOverlayCursorCondition(expr, "<")
	}
}

// browseFileAnchorCountQuery counts the listed files ahead of a keyset.
func browseFileAnchorCountQuery(
	opts *database.BrowseFilesOptions, sortMode string, row browseAnchorRow, tagPlan browseTagPlan,
) (query string, args []any) {
	where, args := browseFilesBaseCondition(opts, tagPlan)
	args = append(args, row.sortValue, row.sortValue, row.dbid)
	return `SELECT COUNT(*)
		FROM Media m` + browseAnchorSystemsJoin(opts) + `
		WHERE ` + where + browseBeforeCondition(sortMode), args
}

// sqlBrowseFileAnchor finds a file's position among a single directory's files.
func sqlBrowseFileAnchor(
	ctx context.Context, db sqlQueryable, opts *database.BrowseAnchorOptions,
) (position int, found bool, err error) {
	filesOpts := browseAnchorFilesOptions(opts, opts.PathPrefix)
	tagPlan := browsePrefixTagPlan(ctx, db, opts.PathPrefix, opts.Systems, opts.Tags)
	sortMode := resolveBrowseSortMode(ctx, db, filesOpts)
	query, args := browseFileAnchorRowQuery(
		filesOpts, opts.Path, browseSortExpr(sortMode), browseSortClause(sortMode), tagPlan,
	)
	row, found, err := scanBrowseAnchorRow(ctx, db, query, args)
	if err != nil || !found {
		return 0, false, err
	}
	query, args = browseFileAnchorCountQuery(filesOpts, sortMode, row, tagPlan)
	if err = db.QueryRowContext(ctx, query, args...).Scan(&position); err != nil {
		return 0, false, fmt.Errorf("browse anchor count: %w", err)
	}
	return position, true, nil
}

// browseOverlayFileAnchorRowQuery is browseFileAnchorRowQuery for one route of a
// merged listing. The route's merge conditions apply, so a file the merge drops
// in favor of a higher-priority route is not listed and reports no position.
func browseOverlayFileAnchorRowQuery(
	opts *database.BrowseAnchorOptions, index int, sortExpr, direction string, tagPlan browseTagPlan,
) (query string, args []any) {
	sources := browseOverlaySources(opts.Overlay)
	route := browseOverlayRouteSort(sortExpr, sources[index].PathPrefix, nil)
	query, args = browseFileAnchorRowQuery(
		browseAnchorFilesOptions(opts, sources[index].PathPrefix),
		opts.Path, sortExpr, route.orderExpr+` `+direction+`, m.DBID `+direction, tagPlan,
	)
	merge, mergeArgs := browseOverlayRouteMergeConditions(sources, index, opts.Systems, opts.ExcludeHidden)
	if len(merge) > 0 {
		query = strings.Replace(query, `
		ORDER BY `, `
		AND `+strings.Join(merge, `
		AND `)+`
		ORDER BY `, 1)
		args = append(args, mergeArgs...)
	}
	return query, args
}

// browseOverlayRouteAnchorCountQuery counts one route's surviving rows ahead of
// a keyset. Summed over the routes this is the row's position in the merge, since
// the merge is an ordered union of what each route keeps.
func browseOverlayRouteAnchorCountQuery(
	opts *database.BrowseAnchorOptions,
	index int,
	sortExpr, beforeOp string,
	row browseAnchorRow,
	tagPlan browseTagPlan,
) (query string, args []any) {
	sources := browseOverlaySources(opts.Overlay)
	filesOpts := browseAnchorFilesOptions(opts, sources[index].PathPrefix)
	where, args := browseFilesFilterCondition(filesOpts, true, tagPlan)
	route := browseOverlayRouteSort(sortExpr, sources[index].PathPrefix, &database.BrowseCursor{
		SortValue: row.sortValue,
	})
	where += route.rangeClause
	args = append(args, route.rangeArgs...)
	merge, mergeArgs := browseOverlayRouteMergeConditions(sources, index, opts.Systems, opts.ExcludeHidden)
	if len(merge) > 0 {
		where += `
		AND ` + strings.Join(merge, `
		AND `)
		args = append(args, mergeArgs...)
	}
	where += browseOverlayCursorCondition(route.orderExpr, beforeOp)
	args = append(args, route.cursorValue, route.cursorValue, row.dbid)
	return `SELECT COUNT(*)
		FROM Media m` + browseAnchorSystemsJoin(filesOpts) + `
		WHERE ` + where, args
}

// sqlBrowseOverlayFileAnchor finds a file's position in a merged listing. It
// looks in every route the path could belong to, highest priority first.
func sqlBrowseOverlayFileAnchor(
	ctx context.Context, db sqlQueryable, opts *database.BrowseAnchorOptions,
) (position int, found bool, err error) {
	sources := browseOverlaySources(opts.Overlay)
	sortExpr, direction, cursorOp := browseOverlaySort(opts.Sort)
	beforeOp := "<"
	if cursorOp == "<" {
		beforeOp = ">"
	}
	tagPlan := browseOverlayRoutesTagPlan(ctx, db, opts.Overlay, opts.Systems, opts.Tags)

	var row browseAnchorRow
	for i := range sources {
		if _, below := browseAnchorBelow(opts.Path, sources[i].PathPrefix); !below {
			continue
		}
		query, args := browseOverlayFileAnchorRowQuery(opts, i, sortExpr, direction, tagPlan)
		row, found, err = scanBrowseAnchorRow(ctx, db, query, args)
		if err != nil {
			return 0, false, err
		}
		if found {
			break
		}
	}
	if !found {
		return 0, false, nil
	}
	for i := range sources {
		var count int
		query, args := browseOverlayRouteAnchorCountQuery(opts, i, sortExpr, beforeOp, row, tagPlan)
		if err = db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return 0, false, fmt.Errorf("browse overlay anchor count: %w", err)
		}
		position += count
	}
	return position, true, nil
}
