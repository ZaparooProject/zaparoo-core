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
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
)

// A title's stored version count is the number of versions its game has: the
// present media of the title and of every title in its version group, with a
// container's members folded into the row it launches and a symlink alias
// folded into its target. NULL stands for one version, so a library of
// single-version games stores nothing. Systems.VersionCountsState says
// whether the counts of a system's titles have been worked out: until a
// regroup has run over a system, its titles have no count. A reconcile of the
// system's media or a change to its groups marks its counts stale, and only
// those systems are counted again: reading a system's media to count it costs
// tens of seconds on a large library on SD, which a scrape that changed
// nothing must not pay.

// markVersionCountsStaleSQL marks counted systems stale (state 1 to 2; see the
// migration adding VersionCountsState). Append the condition
// choosing the systems.
const markVersionCountsStaleSQL = `UPDATE Systems SET VersionCountsState = 2
	WHERE VersionCountsState = 1 AND `

// versionCountRow is one present media row read to count versions.
type versionCountRow struct {
	path      string
	parentDir string
	dbid      int64
	title     int64
}

// versionCountsFromRows works out the version count of every title of one
// system that has more than one version. groups maps a title to its version
// group, links an alias row to its target row.
func versionCountsFromRows(
	rows []versionCountRow, groups map[int64]int64, links map[int64]int64,
) map[int64]int {
	gameOf := func(title int64) int64 {
		if group, ok := groups[title]; ok {
			return group
		}
		return title
	}

	// A directory folds its members only when the same game holds two of
	// them, so only those directories need a launch row.
	type dirGame struct {
		dir  string
		game int64
	}
	members := make(map[dirGame]int)
	for i := range rows {
		if !container.MayHaveContainerTarget(rows[i].path) {
			continue
		}
		if dir := container.ParentDir(rows[i].path); dir != "" {
			members[dirGame{dir: dir, game: gameOf(rows[i].title)}]++
		}
	}
	candidates := make(map[string]struct{})
	for key, count := range members {
		if count >= 2 {
			candidates[strings.TrimRight(key.dir, "/")+"/"] = struct{}{}
		}
	}

	launchByDir := make(map[string]int64, len(candidates))
	if len(candidates) > 0 {
		// A directory with media in a folder below it is browsed, not
		// launched, so it folds nothing.
		nested := make(map[string]struct{})
		dirRows := make(map[string][]database.Media, len(candidates))
		for i := range rows {
			parent := rows[i].parentDir
			if _, ok := candidates[parent]; ok {
				dirRows[parent] = append(dirRows[parent], database.Media{
					DBID:           rows[i].dbid,
					MediaTitleDBID: rows[i].title,
					Path:           rows[i].path,
					ParentDir:      parent,
				})
			}
			for j := range len(parent) - 1 {
				if parent[j] != '/' {
					continue
				}
				if _, ok := candidates[parent[:j+1]]; ok {
					nested[parent[:j+1]] = struct{}{}
				}
			}
		}
		for dir, dirMedia := range dirRows {
			if _, ok := nested[dir]; ok {
				continue
			}
			slices.SortFunc(dirMedia, func(a, b database.Media) int {
				return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.DBID, b.DBID))
			})
			if launch := container.SelectLaunchMedia(dirMedia); launch != nil {
				launchByDir[dir] = launch.DBID
			}
		}
	}

	gameRows := make(map[int64][]int, len(rows))
	for i := range rows {
		game := gameOf(rows[i].title)
		gameRows[game] = append(gameRows[game], i)
	}

	counts := make(map[int64]int)
	present := make(map[int64]struct{})
	for game, indexes := range gameRows {
		if len(indexes) < 2 {
			continue
		}
		clear(present)
		for _, i := range indexes {
			present[rows[i].dbid] = struct{}{}
		}
		// A member folds into its directory's launch row only when the game
		// holds that row; otherwise the members stay apart, as in a search.
		kept := make([]int, 0, len(indexes))
		for _, i := range indexes {
			if container.MayHaveContainerTarget(rows[i].path) {
				dir := strings.TrimRight(container.ParentDir(rows[i].path), "/") + "/"
				if members[dirGame{dir: container.ParentDir(rows[i].path), game: game}] >= 2 {
					if launch, ok := launchByDir[dir]; ok && launch != rows[i].dbid {
						if _, held := present[launch]; held {
							continue
						}
					}
				}
			}
			kept = append(kept, i)
		}
		clear(present)
		for _, i := range kept {
			present[rows[i].dbid] = struct{}{}
		}
		count := 0
		for _, i := range kept {
			if target, linked := links[rows[i].dbid]; linked {
				if _, held := present[target]; held {
					continue
				}
			}
			count++
		}
		if count < 2 {
			continue
		}
		for _, i := range indexes {
			counts[rows[i].title] = count
		}
	}
	return counts
}

// sqlRecomputeVersionCounts stores the version count of every title of the
// given systems, or of every system when none are given, whose counts are
// missing or stale. It writes only the titles whose count changed, marks the
// systems' counts current, and returns how many systems it counted and how
// many titles it wrote.
func sqlRecomputeVersionCounts(
	ctx context.Context, conn *sql.DB, systemDBIDs []int64,
) (counted, written int, err error) {
	query := `SELECT DBID FROM Systems WHERE VersionCountsState <> 1`
	args := make([]any, 0, len(systemDBIDs))
	if len(systemDBIDs) > 0 {
		query += ` AND DBID IN (` + prepareVariadic("?", ",", len(systemDBIDs)) + `)`
		args = append(args, int64Args(systemDBIDs)...)
	}
	systems, err := queryInt64s(ctx, conn, query+` ORDER BY DBID`, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("list systems to count: %w", err)
	}
	if len(systems) == 0 {
		return 0, 0, nil
	}
	for _, system := range systems {
		n, err := sqlRecomputeSystemVersionCounts(ctx, conn, system)
		if err != nil {
			return counted, written, err
		}
		counted++
		written += n
	}
	return counted, written, nil
}

func sqlRecomputeSystemVersionCounts(ctx context.Context, conn *sql.DB, system int64) (int, error) {
	rows, err := sqlVersionCountRows(ctx, conn, system)
	if err != nil {
		return 0, err
	}
	scope := versionKeyScope{systems: []int64{system}}
	groups, err := sqlStoredVersionGroups(ctx, conn, scope)
	if err != nil {
		return 0, err
	}
	links, err := sqlSystemMediaLinks(ctx, conn, system)
	if err != nil {
		return 0, err
	}
	wanted := versionCountsFromRows(rows, groups, links)
	current, err := sqlStoredVersionCounts(ctx, conn, system)
	if err != nil {
		return 0, err
	}

	type change struct {
		count *int
		title int64
	}
	var changes []change
	for title, count := range wanted {
		if had, ok := current[title]; !ok || had != count {
			changes = append(changes, change{title: title, count: &count})
		}
	}
	for title := range current {
		if _, ok := wanted[title]; !ok {
			changes = append(changes, change{title: title})
		}
	}
	slices.SortFunc(changes, func(a, b change) int { return cmp.Compare(a.title, b.title) })

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store version counts: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if len(changes) > 0 {
		stmt, prepErr := tx.PrepareContext(ctx, `UPDATE MediaTitles SET VersionCount = ? WHERE DBID = ?`)
		if prepErr != nil {
			return 0, fmt.Errorf("store version counts: prepare update: %w", prepErr)
		}
		defer func() { _ = stmt.Close() }()
		for i := range changes {
			if _, err = stmt.ExecContext(ctx, changes[i].count, changes[i].title); err != nil {
				return 0, fmt.Errorf("store version count of title %d: %w", changes[i].title, err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE Systems SET VersionCountsState = 1 WHERE DBID = ?`, system,
	); err != nil {
		return 0, fmt.Errorf("mark version counts ready: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("store version counts: commit: %w", err)
	}
	committed = true
	return len(changes), nil
}

// sqlVersionCountRows reads every present media row of a system with its
// title. Both reads are answered from an index alone: a row's path from the
// system's path index and its title from the title index, walked in title
// order. Reading the Media table itself would fetch every row of the system
// from its own page, which on an SD card costs far more than the counting.
const storedVersionCountsSQL = `SELECT DBID, VersionCount FROM MediaTitles
	WHERE SystemDBID = ? AND VersionCount IS NOT NULL`

func sqlVersionCountRows(ctx context.Context, conn sqlQueryable, system int64) ([]versionCountRow, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT DBID, MediaTitleDBID, Path, ParentDir FROM Media WHERE SystemDBID = ? AND IsMissing = 0`, system)
	if err != nil {
		return nil, fmt.Errorf("read media to count versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []versionCountRow
	for rows.Next() {
		var row versionCountRow
		if err := rows.Scan(&row.dbid, &row.title, &row.path, &row.parentDir); err != nil {
			return nil, fmt.Errorf("scan media to count versions: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media to count versions: %w", err)
	}
	return out, nil
}

func sqlSystemMediaLinks(ctx context.Context, conn sqlQueryable, system int64) (map[int64]int64, error) {
	hasLinks, err := sqlSystemHasMediaLinks(ctx, conn, []int64{system})
	if err != nil || !hasLinks {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT l.MediaDBID, l.TargetMediaDBID
		FROM MediaLinks l JOIN Media m ON m.DBID = l.MediaDBID
		WHERE m.SystemDBID = ?`, system)
	if err != nil {
		return nil, fmt.Errorf("read media links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	links := make(map[int64]int64)
	for rows.Next() {
		var alias, target int64
		if err := rows.Scan(&alias, &target); err != nil {
			return nil, fmt.Errorf("scan media link: %w", err)
		}
		links[alias] = target
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media links: %w", err)
	}
	return links, nil
}

func sqlStoredVersionCounts(ctx context.Context, conn sqlQueryable, system int64) (map[int64]int, error) {
	rows, err := conn.QueryContext(ctx, storedVersionCountsSQL, system)
	if err != nil {
		return nil, fmt.Errorf("read stored version counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	current := make(map[int64]int)
	for rows.Next() {
		var title int64
		var count int
		if err := rows.Scan(&title, &count); err != nil {
			return nil, fmt.Errorf("scan stored version count: %w", err)
		}
		current[title] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored version counts: %w", err)
	}
	return current, nil
}

// GetStoredVersionCounts returns the stored version count of each given title
// whose system has counts. With excludeHidden, a title whose game holds a
// hidden version is left out of Counts and listed in Recount instead: its
// stored count includes the hidden version, so it has to be counted live.
func (db *MediaDB) GetStoredVersionCounts(
	ctx context.Context, titleDBIDs []int64, excludeHidden bool,
) (database.StoredVersionCounts, error) {
	conn := db.sql.Load()
	if conn == nil {
		return database.StoredVersionCounts{}, ErrNullSQL
	}
	result := database.StoredVersionCounts{Counts: make(map[int64]int, len(titleDBIDs))}
	err := forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
		rows, err := conn.QueryContext(ctx, `SELECT t.DBID, COALESCE(t.VersionCount, 1)
			FROM MediaTitles t CROSS JOIN Systems s ON s.DBID = t.SystemDBID
			WHERE s.VersionCountsState <> 0 AND t.DBID IN (`+prepareVariadic("?", ",", len(chunk))+`)`,
			int64Args(chunk)...)
		if err != nil {
			return fmt.Errorf("read stored version counts: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var title int64
			var count int
			if err := rows.Scan(&title, &count); err != nil {
				return fmt.Errorf("scan stored version count: %w", err)
			}
			result.Counts[title] = count
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate stored version counts: %w", err)
		}
		return nil
	})
	if err != nil {
		return database.StoredVersionCounts{}, err
	}
	if !excludeHidden || len(result.Counts) == 0 {
		return result, nil
	}

	recount, err := db.titlesWithHiddenVersions(ctx, conn, slices.Collect(maps.Keys(result.Counts)))
	if err != nil {
		return database.StoredVersionCounts{}, err
	}
	for _, title := range recount {
		delete(result.Counts, title)
	}
	result.Recount = recount
	return result, nil
}

// hiddenTitlesSQL lists the titles holding a hidden present media row,
// starting from the hidden tag's rows as hiddenMediaRowsSQL does.
const hiddenTitlesSQL = `SELECT DISTINCT m.MediaTitleDBID FROM TagTypes tt
	CROSS JOIN Tags t ON t.TypeDBID = tt.DBID
	CROSS JOIN MediaTags mt ON mt.TagDBID = t.DBID
	CROSS JOIN Media m ON m.DBID = mt.MediaDBID
	WHERE tt.Type = 'user' AND t.Tag = 'hidden' AND m.IsMissing = 0`

// titlesWithHiddenVersions returns which of the titles belong to a game with
// a hidden version: a hidden file, or a file under a hidden folder, in the
// title or any title of its version group.
func (db *MediaDB) titlesWithHiddenVersions(
	ctx context.Context, conn sqlQueryable, titleDBIDs []int64,
) ([]int64, error) {
	hiddenTitles, err := queryInt64s(ctx, conn, hiddenTitlesSQL)
	if err != nil {
		return nil, fmt.Errorf("read titles with hidden media: %w", err)
	}
	dirs, err := loadHiddenDirs(ctx, conn)
	if err != nil {
		return nil, err
	}
	if len(hiddenTitles) == 0 && len(dirs) == 0 {
		return nil, nil
	}

	members, err := db.GetVersionGroupMembers(ctx, titleDBIDs)
	if err != nil {
		return nil, err
	}
	gameOf := make(map[int64][]int64, len(titleDBIDs))
	var all []int64
	for _, title := range titleDBIDs {
		game := members[title]
		if len(game) == 0 {
			game = []int64{title}
		}
		gameOf[title] = game
		all = append(all, game...)
	}
	slices.Sort(all)
	all = slices.Compact(all)

	hidden := make(map[int64]struct{}, len(hiddenTitles))
	for _, title := range hiddenTitles {
		hidden[title] = struct{}{}
	}
	for i := range dirs {
		pathClause, pathArgs := browsePathPrefixCondition("Path", dirs[i].Prefix)
		err = forEachTitleChunk(all, func(chunk []int64) error {
			//nolint:gosec // Safe: the clause holds fixed column names and placeholders
			found, queryErr := queryInt64s(ctx, conn, `SELECT DISTINCT MediaTitleDBID FROM Media
				WHERE IsMissing = 0 AND MediaTitleDBID IN (`+prepareVariadic("?", ",", len(chunk))+`)
				AND `+pathClause, append(int64Args(chunk), pathArgs...)...)
			if queryErr != nil {
				return fmt.Errorf("read titles under hidden folder: %w", queryErr)
			}
			for _, title := range found {
				hidden[title] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var recount []int64
	for _, title := range titleDBIDs {
		for _, member := range gameOf[title] {
			if _, ok := hidden[member]; ok {
				recount = append(recount, title)
				break
			}
		}
	}
	slices.Sort(recount)
	return recount, nil
}
