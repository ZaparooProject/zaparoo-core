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
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

const (
	// versionKeyMaxTitles is the most titles one key may join. A key shared
	// more widely than any real game's releases is a generic value, such as a
	// BIOS or a mislabelled parent, and joining on it would glue unrelated
	// games together.
	versionKeyMaxTitles = 64
	// versionGroupMaxTitles bounds a group built by chaining keys, for the
	// same reason.
	versionGroupMaxTitles = 128
	// versionTitleChunk keeps title lists well inside SQLite's variable limit.
	versionTitleChunk = 500
)

// The index holds only titles that are linked to another, so a library with no
// links stores nothing in it. A regroup makes sure it exists before it gives
// any title a group, and no read needs it until one has.
const (
	versionGroupIndexName = "mediatitles_version_group_idx"
	versionGroupIndexDDL  = "CREATE INDEX IF NOT EXISTS " + versionGroupIndexName +
		" ON MediaTitles(VersionGroup) WHERE VersionGroup IS NOT NULL"
)

// versionKeyRow is one key held by one title.
type versionKeyRow struct {
	keyType string
	key     string
	system  int64
	title   int64
}

// normalizeVersionKey gives the stored spellings of one identity one form.
// Romset names reach the database both as text properties and as tags, and a
// tag pads a short all-digit value, so both are compared unpadded.
func normalizeVersionKey(keyType, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if keyType == database.VersionKeyMAMESet {
		key = tags.UnpadTagValue(key)
	}
	return key
}

// versionKeyScope narrows a key read to some systems or some titles. The zero
// value reads every key.
type versionKeyScope struct {
	systems []int64
	titles  []int64
}

func (s versionKeyScope) condition(systemColumn, titleColumn string) (clause string, args []any) {
	switch {
	case len(s.titles) > 0:
		return " AND " + titleColumn + " IN (" + prepareVariadic("?", ",", len(s.titles)) + ")", int64Args(s.titles)
	case len(s.systems) > 0:
		return " AND " + systemColumn + " IN (" + prepareVariadic("?", ",", len(s.systems)) + ")", int64Args(s.systems)
	default:
		return "", nil
	}
}

// findTagDBID returns the DBID of one tag, or 0 when the library has never
// stored it.
func findTagDBID(ctx context.Context, db sqlQueryable, tagType, tag string) (int64, error) {
	var dbid int64
	err := db.QueryRowContext(ctx, `
		SELECT t.DBID FROM TagTypes tt CROSS JOIN Tags t ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND t.Tag = ? LIMIT 1`, tagType, tag).Scan(&dbid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find %s:%s tag: %w", tagType, tag, err)
	}
	return dbid, nil
}

// sqlLoadVersionKeys reads every version key in scope. Stored keys come from
// MediaVersionKeys; the rest are identities scrapers already keep as tags and
// properties, read where they live so they are neither stored twice nor
// missing from a library scraped before version groups existed.
//
// Each read starts from the small side: a stored key, a property type or a
// tag type is held by few rows next to the library, so a system with none of
// them costs an index probe.
func sqlLoadVersionKeys(ctx context.Context, db sqlQueryable, scope versionKeyScope) ([]versionKeyRow, error) {
	var keys []versionKeyRow
	read := func(keyType, query string, args ...any) error {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query %s version keys: %w", keyType, err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			row := versionKeyRow{keyType: keyType}
			var storedType sql.NullString
			if err := rows.Scan(&row.system, &row.title, &storedType, &row.key); err != nil {
				return fmt.Errorf("scan %s version key: %w", keyType, err)
			}
			if storedType.Valid {
				row.keyType = storedType.String
			}
			row.key = normalizeVersionKey(row.keyType, row.key)
			if !database.ValidVersionKey(database.VersionKey{Type: row.keyType, Key: row.key}) {
				continue
			}
			keys = append(keys, row)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate %s version keys: %w", keyType, err)
		}
		return nil
	}

	clause, args := scope.condition("k.SystemDBID", "m.MediaTitleDBID")
	stored := `SELECT k.SystemDBID, m.MediaTitleDBID, k.KeyType, k.Key
		FROM MediaVersionKeys k CROSS JOIN Media m ON m.DBID = k.MediaDBID
		WHERE m.IsMissing = 0` + clause
	if len(scope.titles) > 0 {
		stored = `SELECT k.SystemDBID, m.MediaTitleDBID, k.KeyType, k.Key
			FROM Media m CROSS JOIN MediaVersionKeys k ON k.MediaDBID = m.DBID
			WHERE m.IsMissing = 0` + clause
	}
	if err := read("", stored, args...); err != nil {
		return nil, err
	}

	gameID, err := findTagDBID(ctx, db, string(tags.TagTypeProperty), string(tags.TagPropertyGameDatabaseID))
	if err != nil {
		return nil, err
	}
	if gameID != 0 {
		clause, args = scope.condition("t.SystemDBID", "t.DBID")
		// A scoped read walks the scope's titles and probes each for the
		// property; an unscoped one walks the property instead.
		// A title whose media is all missing holds no key, as with every
		// other identity, so it cannot push a shared ID past the size bound.
		const present = ` AND EXISTS (SELECT 1 FROM Media m
			WHERE m.MediaTitleDBID = t.DBID AND m.IsMissing = 0)`
		query := `SELECT t.SystemDBID, t.DBID, NULL, p.Text
			FROM MediaTitles t CROSS JOIN MediaTitleProperties p
			  ON p.MediaTitleDBID = t.DBID AND p.TypeTagDBID = ?
			WHERE p.Text != ''` + present + clause
		if clause == "" {
			query = `SELECT t.SystemDBID, t.DBID, NULL, p.Text
				FROM MediaTitleProperties p CROSS JOIN MediaTitles t ON t.DBID = p.MediaTitleDBID
				WHERE p.TypeTagDBID = ? AND p.Text != ''` + present
		}
		readErr := read(database.VersionKeyGameDatabaseID, query, append([]any{gameID}, args...)...)
		if readErr != nil {
			return nil, readErr
		}
	}

	setName, err := findTagDBID(ctx, db, string(tags.TagTypeProperty), string(tags.TagPropertyMAMESetName))
	if err != nil {
		return nil, err
	}
	clause, args = scope.condition("m.SystemDBID", "m.MediaTitleDBID")
	if setName != 0 {
		query := `SELECT m.SystemDBID, m.MediaTitleDBID, NULL, p.Text
			FROM MediaProperties p CROSS JOIN Media m ON m.DBID = p.MediaDBID
			WHERE p.TypeTagDBID = ? AND p.Text != '' AND m.IsMissing = 0` + clause
		if len(scope.titles) > 0 {
			query = `SELECT m.SystemDBID, m.MediaTitleDBID, NULL, p.Text
				FROM Media m CROSS JOIN MediaProperties p ON p.MediaDBID = m.DBID AND p.TypeTagDBID = ?
				WHERE p.Text != '' AND m.IsMissing = 0` + clause
		}
		readErr := read(database.VersionKeyMAMESet, query, append([]any{setName}, args...)...)
		if readErr != nil {
			return nil, readErr
		}
	}

	parents := `SELECT m.SystemDBID, m.MediaTitleDBID, NULL, t.Tag
		FROM TagTypes tt CROSS JOIN Tags t ON t.TypeDBID = tt.DBID
		CROSS JOIN MediaTags mt ON mt.TagDBID = t.DBID
		CROSS JOIN Media m ON m.DBID = mt.MediaDBID
		WHERE tt.Type = ? AND m.IsMissing = 0` + clause
	readErr := read(database.VersionKeyMAMESet, parents,
		append([]any{string(tags.TagTypeMameParent)}, args...)...)
	if readErr != nil {
		return nil, readErr
	}
	return keys, nil
}

// versionGroupsFromKeys joins titles that share a key, within one system, and
// returns the group of every title that ends up joined to another: the lowest
// title DBID of its group. A title absent from the result is its own group.
func versionGroupsFromKeys(keys []versionKeyRow) map[int64]int64 {
	type keyID struct {
		keyType string
		key     string
		system  int64
	}
	holders := make(map[keyID][]int64)
	for i := range keys {
		id := keyID{system: keys[i].system, keyType: keys[i].keyType, key: keys[i].key}
		held := holders[id]
		// A key past the bound is ignored whatever else holds it, so its
		// holders stop being tracked, and the check stays a short scan.
		if len(held) > versionKeyMaxTitles || slices.Contains(held, keys[i].title) {
			continue
		}
		holders[id] = append(held, keys[i].title)
	}
	ids := make([]keyID, 0, len(holders))
	for id, titles := range holders {
		if len(titles) < 2 {
			continue
		}
		if len(titles) > versionKeyMaxTitles {
			log.Debug().Str("keyType", id.keyType).Str("key", id.key).Int("maxTitles", versionKeyMaxTitles).
				Msg("version key shared by too many titles, ignored")
			continue
		}
		slices.Sort(titles)
		ids = append(ids, id)
	}
	// The narrowest keys join first, so when the size bound refuses a join it
	// is the widest and least certain one. The rest of the order only makes
	// the outcome repeatable.
	slices.SortFunc(ids, func(a, b keyID) int {
		return cmp.Or(
			cmp.Compare(len(holders[a]), len(holders[b])),
			cmp.Compare(a.system, b.system),
			cmp.Compare(a.keyType, b.keyType),
			cmp.Compare(a.key, b.key),
		)
	})

	parent := make(map[int64]int64)
	size := make(map[int64]int)
	var find func(int64) int64
	find = func(title int64) int64 {
		root, ok := parent[title]
		if !ok {
			parent[title] = title
			size[title] = 1
			return title
		}
		if root == title {
			return title
		}
		root = find(root)
		parent[title] = root
		return root
	}
	for _, id := range ids {
		// A key joins all its holders' groups at once or, when that would
		// pass the size bound, all but the largest: the titles the bound
		// keeps out of an overgrown group still belong with each other.
		var roots []int64
		total := 0
		for _, title := range holders[id] {
			if root := find(title); !slices.Contains(roots, root) {
				roots = append(roots, root)
				total += size[root]
			}
		}
		slices.SortFunc(roots, func(a, b int64) int {
			return cmp.Or(cmp.Compare(size[b], size[a]), cmp.Compare(a, b))
		})
		for total > versionGroupMaxTitles && len(roots) > 1 {
			log.Debug().Str("keyType", id.keyType).Str("key", id.key).
				Msg("version group would grow too large, key joined without it")
			total -= size[roots[0]]
			roots = roots[1:]
		}
		if len(roots) < 2 {
			continue
		}
		// The lowest DBID becomes the root, so a group's value is its lowest
		// title and does not depend on the order keys were read in.
		root := slices.Min(roots)
		for _, other := range roots {
			if other != root {
				parent[other] = root
			}
		}
		size[root] = total
	}

	groups := make(map[int64]int64)
	for title := range parent {
		if root := find(title); size[root] > 1 {
			groups[title] = root
		}
	}
	return groups
}

func resolveSystemDBIDs(ctx context.Context, conn sqlQueryable, systemIDs []string) ([]int64, error) {
	args := make([]any, 0, len(systemIDs))
	for _, id := range systemIDs {
		args = append(args, id)
	}
	dbids, err := queryInt64s(ctx, conn, `SELECT DBID FROM Systems WHERE SystemID IN (`+
		prepareVariadic("?", ",", len(systemIDs))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve systems: %w", err)
	}
	return dbids, nil
}

// RecomputeVersionGroups rebuilds MediaTitles.VersionGroup for the given
// systems, or every system when none are given, and writes only the titles
// whose group changed.
func (db *MediaDB) RecomputeVersionGroups(ctx context.Context, systemIDs []string) (retErr error) {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	defer func() { db.NoteCorruption(retErr) }()

	var scope versionKeyScope
	if len(systemIDs) > 0 {
		dbids, err := resolveSystemDBIDs(ctx, conn, systemIDs)
		if err != nil {
			return fmt.Errorf("recompute version groups: %w", err)
		}
		if len(dbids) == 0 {
			return nil
		}
		scope.systems = dbids
	}

	keys, err := sqlLoadVersionKeys(ctx, conn, scope)
	if err != nil {
		return fmt.Errorf("recompute version groups: %w", err)
	}
	wanted := versionGroupsFromKeys(keys)

	current, err := sqlStoredVersionGroups(ctx, conn, scope)
	if err != nil {
		return fmt.Errorf("recompute version groups: %w", err)
	}

	changed, err := sqlStoreVersionGroups(ctx, conn, wanted, current)
	if err != nil {
		return fmt.Errorf("recompute version groups: %w", err)
	}
	log.Debug().Int("keys", len(keys)).Int("grouped", len(wanted)).Int("changed", changed).
		Msg("recomputed version groups")

	// Counts follow groups and media: a system is counted again when an
	// index reconciled its media or a group above changed.
	started := time.Now()
	counted, written, err := sqlRecomputeVersionCounts(ctx, conn, scope.systems)
	if err != nil {
		return fmt.Errorf("recompute version counts: %w", err)
	}
	if counted > 0 {
		log.Info().Int("systems", counted).Int("changed", written).Dur("elapsed", time.Since(started)).
			Msg("recomputed version counts")
	}
	return nil
}

// sqlStoreVersionGroups writes the titles whose version group differs from
// the stored one and returns how many it wrote.
func sqlStoreVersionGroups(
	ctx context.Context, conn *sql.DB, wanted, current map[int64]int64,
) (int, error) {
	type change struct {
		group *int64
		title int64
	}
	var changes []change
	for title, group := range wanted {
		if had, ok := current[title]; !ok || had != group {
			changes = append(changes, change{title: title, group: &group})
		}
	}
	for title := range current {
		if _, ok := wanted[title]; !ok {
			changes = append(changes, change{title: title})
		}
	}
	if len(changes) == 0 {
		return 0, nil
	}
	slices.SortFunc(changes, func(a, b change) int { return cmp.Compare(a.title, b.title) })

	if _, err := conn.ExecContext(ctx, versionGroupIndexDDL); err != nil {
		return 0, fmt.Errorf("ensure index: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx, `UPDATE MediaTitles SET VersionGroup = ? WHERE DBID = ?`)
	if err != nil {
		return 0, fmt.Errorf("prepare update: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for i := range changes {
		if _, err = stmt.ExecContext(ctx, changes[i].group, changes[i].title); err != nil {
			return 0, fmt.Errorf("update title %d: %w", changes[i].title, err)
		}
	}
	// A title's version count includes its group's media, so a system whose
	// groups changed is counted again. Written with the groups, so a crash
	// cannot leave the new groups with counts marked current.
	titles := make([]int64, len(changes))
	for i := range changes {
		titles[i] = changes[i].title
	}
	err = forEachTitleChunk(titles, func(chunk []int64) error {
		//nolint:gosec // Safe: the statement holds only fixed SQL and placeholders
		_, execErr := tx.ExecContext(ctx, markVersionCountsStaleSQL+
			`DBID IN (SELECT DISTINCT SystemDBID FROM MediaTitles WHERE DBID IN (`+
			prepareVariadic("?", ",", len(chunk))+`))`, int64Args(chunk)...)
		if execErr != nil {
			return fmt.Errorf("mark version counts stale: %w", execErr)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return len(changes), nil
}

// sqlStoredVersionGroups reads the group of every title in scope that has one.
func sqlStoredVersionGroups(ctx context.Context, db sqlQueryable, scope versionKeyScope) (map[int64]int64, error) {
	clause, args := scope.condition("SystemDBID", "DBID")
	//nolint:gosec // Safe: the clause holds only fixed column names and placeholders
	rows, err := db.QueryContext(ctx,
		`SELECT DBID, VersionGroup FROM MediaTitles WHERE VersionGroup IS NOT NULL`+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("read current groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	current := make(map[int64]int64)
	for rows.Next() {
		var title, group int64
		if err := rows.Scan(&title, &group); err != nil {
			return nil, fmt.Errorf("scan current group: %w", err)
		}
		current[title] = group
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current groups: %w", err)
	}
	return current, nil
}

// forEachTitleChunk runs fn over titleDBIDs a statement's worth at a time.
func forEachTitleChunk(titleDBIDs []int64, fn func(chunk []int64) error) error {
	for start := 0; start < len(titleDBIDs); start += versionTitleChunk {
		if err := fn(titleDBIDs[start:min(start+versionTitleChunk, len(titleDBIDs))]); err != nil {
			return err
		}
	}
	return nil
}

// GetTitleVersionGroups returns the version group of each given title.
func (db *MediaDB) GetTitleVersionGroups(ctx context.Context, titleDBIDs []int64) (map[int64]int64, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	groups := make(map[int64]int64, len(titleDBIDs))
	err := forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
		rows, err := conn.QueryContext(ctx,
			`SELECT DBID, COALESCE(VersionGroup, DBID) FROM MediaTitles WHERE DBID IN (`+
				prepareVariadic("?", ",", len(chunk))+`)`, int64Args(chunk)...)
		if err != nil {
			return fmt.Errorf("query title version groups: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var title, group int64
			if err := rows.Scan(&title, &group); err != nil {
				return fmt.Errorf("scan title version group: %w", err)
			}
			groups[title] = group
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate title version groups: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// versionGroupMembersSQL reads the titles sharing a group with the given ones.
// Restating that the member's group is set lets the planner use the partial
// index, which holds only linked titles.
const versionGroupMembersSQL = `SELECT t.DBID, o.DBID
	FROM MediaTitles t CROSS JOIN MediaTitles o
	  ON o.VersionGroup = t.VersionGroup AND o.VersionGroup IS NOT NULL
	WHERE t.VersionGroup IS NOT NULL AND t.DBID IN (`

// GetVersionGroupMembers returns every title of the group each given title
// shares with another title.
func (db *MediaDB) GetVersionGroupMembers(ctx context.Context, titleDBIDs []int64) (map[int64][]int64, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	members := make(map[int64][]int64)
	err := forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		rows, err := conn.QueryContext(ctx,
			versionGroupMembersSQL+prepareVariadic("?", ",", len(chunk))+`) ORDER BY t.DBID, o.DBID`,
			int64Args(chunk)...)
		if err != nil {
			return fmt.Errorf("query version group members: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var title, member int64
			if err := rows.Scan(&title, &member); err != nil {
				return fmt.Errorf("scan version group member: %w", err)
			}
			members[title] = append(members[title], member)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate version group members: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return members, nil
}

// GetVersionKeysByTitleDBIDs returns the version keys each given title holds.
func (db *MediaDB) GetVersionKeysByTitleDBIDs(
	ctx context.Context, titleDBIDs []int64,
) (map[int64][]database.VersionKey, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	byTitle := make(map[int64][]database.VersionKey)
	err := forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		keys, err := sqlLoadVersionKeys(ctx, conn, versionKeyScope{titles: chunk})
		if err != nil {
			return err
		}
		for i := range keys {
			key := database.VersionKey{Type: keys[i].keyType, Key: keys[i].key}
			if !slices.Contains(byTitle[keys[i].title], key) {
				byTitle[keys[i].title] = append(byTitle[keys[i].title], key)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return byTitle, nil
}

// containerMediaSQL is true for a row whose extension lets a container launch
// in its place. It reads the path, so on a device it is most of the count's
// cost.
var containerMediaSQL = func() string {
	byLen := make(map[int][]string)
	for _, ext := range container.ContainerTargetExts() {
		byLen[len(ext)] = append(byLen[len(ext)], "'"+ext+"'")
	}
	lengths := make([]int, 0, len(byLen))
	for n := range byLen {
		lengths = append(lengths, n)
	}
	slices.Sort(lengths)
	parts := make([]string, 0, len(lengths))
	for _, n := range lengths {
		parts = append(parts, fmt.Sprintf("lower(substr(Media.Path, -%d)) IN (%s)", n, strings.Join(byLen[n], ",")))
	}
	return strings.Join(parts, " OR ")
}()

// linkedMediaSQL is true for a row that is a linked alias.
const linkedMediaSQL = "EXISTS (SELECT 1 FROM MediaLinks WHERE MediaLinks.MediaDBID = Media.DBID)"

// titleVersionRowCountSQL counts a title's rows in one grouped pass, so a
// page of titles returns one short row per title instead of one per file.
// A library with no links skips the alias probe on every row.
func titleVersionRowCountSQL(titles int, extra []string, hasLinks bool) string {
	foldable := containerMediaSQL
	if hasLinks {
		foldable += " OR " + linkedMediaSQL
	}
	where := ""
	if len(extra) > 0 {
		where = " AND " + strings.Join(extra, " AND ")
	}
	return `SELECT Media.MediaTitleDBID, COUNT(*), SUM(CASE WHEN ` + foldable + ` THEN 1 ELSE 0 END)
		FROM Media
		WHERE Media.MediaTitleDBID IN (` + prepareVariadic("?", ",", titles) + `) AND Media.IsMissing = 0` + where + `
		GROUP BY Media.MediaTitleDBID`
}

// CountTitleVersionRows counts the present rows of each given title that the
// filters' visibility, tag and path conditions let through, the same rows
// GetTitleVersionRows would read.
func (db *MediaDB) CountTitleVersionRows(
	ctx context.Context, titleDBIDs []int64, filters *database.SearchFilters,
) (map[int64]database.TitleVersionRowCount, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	var scoped database.SearchFilters
	if filters != nil {
		scoped = *filters
	}
	tagFilters, err := discoveryTags(ctx, conn, scoped.Tags, scoped.ExcludeHidden, recursiveScope(scoped.PathPrefix))
	if err != nil {
		return nil, err
	}
	var extra []string
	var extraArgs []any
	if scoped.PathPrefix != "" {
		pathClause, pathArgs := browsePathPrefixCondition("Media.Path", mediaRecursivePathPrefix(scoped.PathPrefix))
		extra = append(extra, pathClause)
		extraArgs = append(extraArgs, pathArgs...)
	}
	tagClauses, tagArgs := buildCandidateTagFilterSQL(tagFilters)
	extra = append(extra, tagClauses...)
	extraArgs = append(extraArgs, tagArgs...)
	hasLinks, err := sqlSystemHasMediaLinks(ctx, conn, nil)
	if err != nil {
		return nil, err
	}

	counts := make(map[int64]database.TitleVersionRowCount, len(titleDBIDs))
	err = forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		args := append(int64Args(chunk), extraArgs...)
		//nolint:gosec // Safe: the query is built from fixed SQL and placeholders
		rows, queryErr := conn.QueryContext(ctx, titleVersionRowCountSQL(len(chunk), extra, hasLinks), args...)
		if queryErr != nil {
			return fmt.Errorf("count title version rows: %w", queryErr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var title int64
			var count database.TitleVersionRowCount
			if scanErr := rows.Scan(&title, &count.Rows, &count.Foldable); scanErr != nil {
				return fmt.Errorf("scan title version row count: %w", scanErr)
			}
			counts[title] = count
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return fmt.Errorf("iterate title version row counts: %w", rowsErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}

// GetTitleVersionRows returns the present media rows of the given titles that
// the filters' visibility, tag and path conditions let through.
func (db *MediaDB) GetTitleVersionRows(
	ctx context.Context, titleDBIDs []int64, filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	var scoped database.SearchFilters
	if filters != nil {
		scoped = *filters
	}
	tagFilters, err := discoveryTags(ctx, conn, scoped.Tags, scoped.ExcludeHidden, recursiveScope(scoped.PathPrefix))
	if err != nil {
		return nil, err
	}
	var results []database.SearchResultWithCursor
	err = forEachTitleChunk(titleDBIDs, func(chunk []int64) error {
		rows, rowsErr := sqlSearchMediaRowsByTitleDBIDs(
			ctx, conn, chunk, scoped.PathPrefix, tagFilters, nil, nil, nil, "", math.MaxInt32, false)
		if rowsErr != nil {
			return rowsErr
		}
		results = append(results, rows...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetMediaLinkTargets returns the target of each given media row that is a
// linked alias.
func (db *MediaDB) GetMediaLinkTargets(ctx context.Context, mediaDBIDs []int64) (map[int64]int64, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	targets := make(map[int64]int64)
	if len(mediaDBIDs) == 0 {
		return targets, nil
	}
	// Most libraries hold no links at all, and they answer with one probe.
	hasLinks, err := sqlSystemHasMediaLinks(ctx, conn, nil)
	if err != nil || !hasLinks {
		return targets, err
	}
	err = forEachTitleChunk(mediaDBIDs, func(chunk []int64) error {
		//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders
		rows, queryErr := conn.QueryContext(ctx,
			`SELECT MediaDBID, TargetMediaDBID FROM MediaLinks WHERE MediaDBID IN (`+
				prepareVariadic("?", ",", len(chunk))+`)`, int64Args(chunk)...)
		if queryErr != nil {
			return fmt.Errorf("query media link targets: %w", queryErr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var media, target int64
			if scanErr := rows.Scan(&media, &target); scanErr != nil {
				return fmt.Errorf("scan media link target: %w", scanErr)
			}
			targets[media] = target
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return fmt.Errorf("iterate media link targets: %w", rowsErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// replaceScrapeVersionKeys makes the stored keys of each target's source match
// what its write asserts. Targets that name no source are left alone. A row
// whose keys already match is not rewritten, so a repeated scrape of an
// unchanged library writes nothing here.
func replaceScrapeVersionKeys(ctx context.Context, tx sqlQueryable, targets []database.ScrapeWriteTarget) error {
	type owner struct {
		source string
		media  int64
	}
	wanted := make(map[owner][]database.VersionKey)
	var media []int64
	for i := range targets {
		write := targets[i].Write
		if write == nil || write.VersionKeySource == "" {
			continue
		}
		id := owner{media: targets[i].MediaDBID, source: write.VersionKeySource}
		if _, seen := wanted[id]; !seen {
			media = append(media, id.media)
		}
		keys := make([]database.VersionKey, 0, len(write.VersionKeys))
		for _, key := range write.VersionKeys {
			key.Key = normalizeVersionKey(key.Type, key.Key)
			if database.ValidVersionKey(key) && !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
		slices.SortFunc(keys, func(a, b database.VersionKey) int {
			return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Key, b.Key))
		})
		wanted[id] = keys
	}
	if len(wanted) == 0 {
		return nil
	}

	stored := make(map[owner][]database.VersionKey)
	err := forEachTitleChunk(media, func(chunk []int64) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT MediaDBID, Source, KeyType, Key FROM MediaVersionKeys WHERE MediaDBID IN (`+
				prepareVariadic("?", ",", len(chunk))+`) ORDER BY MediaDBID, Source, KeyType, Key`,
			int64Args(chunk)...)
		if err != nil {
			return fmt.Errorf("query stored version keys: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id owner
			var key database.VersionKey
			if err := rows.Scan(&id.media, &id.source, &key.Type, &key.Key); err != nil {
				return fmt.Errorf("scan stored version key: %w", err)
			}
			stored[id] = append(stored[id], key)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate stored version keys: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for id, keys := range wanted {
		if slices.Equal(stored[id], keys) {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM MediaVersionKeys WHERE MediaDBID = ? AND Source = ?`, id.media, id.source); err != nil {
			return fmt.Errorf("delete version keys: %w", err)
		}
		for _, key := range keys {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO MediaVersionKeys (SystemDBID, KeyType, Key, MediaDBID, Source)
				SELECT SystemDBID, ?, ?, DBID, ? FROM Media WHERE DBID = ?`,
				key.Type, key.Key, id.source, id.media); err != nil {
				return fmt.Errorf("insert version key: %w", err)
			}
		}
	}
	return nil
}
