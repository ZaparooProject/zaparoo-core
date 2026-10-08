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
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

// A hidden folder is one HiddenDirectories row, never a hidden tag on each
// file under it: the per-file hidden set is loaded whole by every browse and
// is expected to stay small, and a folder such as an arcade alternatives tree
// holds thousands of files. The folder is applied by path instead, in the two
// ways per-file visibility already works:
//
//   - aggregates subtract the folder's media count, which drops the folder
//     from its parent's listing and shrinks every ancestor's count;
//   - recursive row queries (search, random, tagged system counts) exclude
//     the folder's path range.
//
// A listing of one directory's own files is never filtered by folder: those
// files sit under a hidden folder only when the directory is that folder or
// inside it, and a folder still browses by its own path.

// hiddenDirFilterType marks the internal NOT filter that carries one hidden
// folder through the tag filter list, the channel every recursive row query
// already takes its visibility from. The NUL keeps it apart from any real
// tag type, and its value is the system and path prefix joined by NUL.
const hiddenDirFilterType = "\x00hidden-directory"

// hiddenDir is one hidden folder as queries use it.
type hiddenDir struct {
	SystemID string
	// Prefix is what every media path under the folder starts with.
	Prefix string
	// Count is the folder's present, not individually hidden media: what an
	// aggregate that counted them has to give back.
	Count int
}

func hiddenDirPrefix(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

// hidesFrom reports whether the folder hides anything from a query scoped to
// scope, a slash-terminated path prefix or "" for the whole library. A scope
// that is the folder or inside it addresses the folder directly, which hiding
// does not prevent; a scope elsewhere holds none of its media.
func (d *hiddenDir) hidesFrom(scope string) bool {
	return d.Prefix != scope && strings.HasPrefix(d.Prefix, scope)
}

// within reports whether the folder is the listed entry at prefix or sits
// below it, so that the entry's count includes the folder's media. An entry
// inside the folder is not affected: it is only listed by browsing the
// folder itself.
func (d *hiddenDir) within(prefix string) bool {
	return strings.HasPrefix(d.Prefix, prefix)
}

const hiddenDirsSQL = `SELECT SystemID, Path FROM HiddenDirectories ORDER BY SystemID, Path`

// loadHiddenDirs returns the hidden folders without their counts.
func loadHiddenDirs(ctx context.Context, db sqlQueryable) ([]hiddenDir, error) {
	rows, err := db.QueryContext(ctx, hiddenDirsSQL)
	if err != nil {
		return nil, fmt.Errorf("query hidden directories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var dirs []hiddenDir
	for rows.Next() {
		var systemID, path string
		if scanErr := rows.Scan(&systemID, &path); scanErr != nil {
			return nil, fmt.Errorf("scan hidden directory: %w", scanErr)
		}
		dirs = append(dirs, hiddenDir{SystemID: systemID, Prefix: hiddenDirPrefix(path)})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("read hidden directories: %w", rowsErr)
	}
	return dirs, nil
}

// Served by media_system_present_path_idx, so the count reads only the
// folder's own index range. Individually hidden files are left out because
// the per-file set already gives them back.
const hiddenDirCountSQL = `SELECT COUNT(*) FROM Media m
	WHERE m.IsMissing = 0
	AND m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)
	AND m.Path >= ? AND m.Path < ?
	AND m.DBID NOT IN (` + hiddenMediaIDsSQL + `)`

// loadHiddenDirCounts fills in each folder's Count.
func loadHiddenDirCounts(ctx context.Context, db sqlQueryable, dirs []hiddenDir) error {
	for i := range dirs {
		upper := stringPrefixUpperBound(dirs[i].Prefix)
		if upper == "" {
			continue
		}
		if err := db.QueryRowContext(ctx, hiddenDirCountSQL, dirs[i].SystemID, dirs[i].Prefix, upper).
			Scan(&dirs[i].Count); err != nil {
			return fmt.Errorf("count hidden directory media: %w", err)
		}
	}
	return nil
}

// dirScope says how a row query relates to hidden folders.
type dirScope struct {
	prefix    string
	recursive bool
}

// directListing is a listing of one directory's own files, which no hidden
// folder filters.
var directListing = dirScope{}

// recursiveScope is a query over everything at or below pathPrefix, or over
// the whole library when it is empty.
func recursiveScope(pathPrefix string) dirScope {
	return dirScope{prefix: mediaRecursivePathPrefix(pathPrefix), recursive: true}
}

// hiddenDirFilters returns the internal NOT filters that exclude the hidden
// folders a query over scope would otherwise read into.
func hiddenDirFilters(ctx context.Context, db sqlQueryable, scope dirScope) ([]zapscript.TagFilter, error) {
	if !scope.recursive {
		return nil, nil
	}
	dirs, err := loadHiddenDirs(ctx, db)
	if err != nil {
		return nil, err
	}
	var filters []zapscript.TagFilter
	for i := range dirs {
		if !dirs[i].hidesFrom(scope.prefix) {
			continue
		}
		filters = append(filters, zapscript.TagFilter{
			Type:     hiddenDirFilterType,
			Value:    dirs[i].SystemID + "\x00" + dirs[i].Prefix,
			Operator: zapscript.TagOperatorNOT,
		})
	}
	return filters, nil
}

// hiddenDirFilterSQL is the row predicate for one internal hidden folder
// filter: the media is not in that system's range of paths under the folder.
// ok is false for any other filter.
func hiddenDirFilterSQL(f zapscript.TagFilter, mediaRef string) (clause string, args []any, ok bool) {
	if f.Type != hiddenDirFilterType {
		return "", nil, false
	}
	systemID, prefix, _ := strings.Cut(f.Value, "\x00")
	pathClause, pathArgs := browsePathPrefixCondition(mediaRef+".Path", prefix)
	clause = "NOT (" + mediaRef + ".SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?) AND " +
		pathClause + ")"
	return clause, append([]any{systemID}, pathArgs...), true
}

// hiddenDirsCondition is the row predicate that keeps mediaRef out of every
// hidden folder, for a query with no path scope of its own. It is empty when
// no folder is hidden.
func hiddenDirsCondition(
	ctx context.Context, db sqlQueryable, mediaRef string,
) (condition string, args []any, err error) {
	filters, err := hiddenDirFilters(ctx, db, recursiveScope(""))
	if err != nil {
		return "", nil, err
	}
	var sb strings.Builder
	for _, f := range filters {
		clause, clauseArgs, _ := hiddenDirFilterSQL(f, mediaRef)
		_, _ = sb.WriteString(" AND " + clause)
		args = append(args, clauseArgs...)
	}
	return sb.String(), args, nil
}

// hiddenDirCovers reports whether a directory row is a hidden folder for
// every system it lists media for. A folder is hidden per system, so a row
// that also holds another system's media stays, with that media. The row's
// own systems decide; a listing that reports none falls back to the systems
// it was scoped to, and an unscoped one to any system.
func hiddenDirCovers(dirs []hiddenDir, prefix string, rowSystems []string, scope []systemdefs.System) bool {
	if len(rowSystems) == 0 {
		for i := range scope {
			rowSystems = append(rowSystems, scope[i].ID)
		}
	}
	hiddenFor := func(systemID string) bool {
		for i := range dirs {
			if dirs[i].Prefix == prefix && (systemID == "" || dirs[i].SystemID == systemID) {
				return true
			}
		}
		return false
	}
	if len(rowSystems) == 0 {
		return hiddenFor("")
	}
	for _, systemID := range rowSystems {
		if !hiddenFor(systemID) {
			return false
		}
	}
	return true
}

// ReplaceHiddenDirectories implements MediaDBI.
func (db *MediaDB) ReplaceHiddenDirectories(
	ctx context.Context, dirs []database.HiddenDirectory,
) (bool, error) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return false, ErrNullSQL
	}
	if db.inTransaction {
		return false, ErrTransactionActive
	}

	want := make([]database.HiddenDirectory, 0, len(dirs))
	for _, dir := range dirs {
		dir.Path = pathutil.CanonicalMediaPath(dir.Path)
		if dir.SystemID == "" || dir.Path == "" {
			continue
		}
		want = append(want, dir)
	}
	slices.SortFunc(want, func(a, b database.HiddenDirectory) int {
		if c := strings.Compare(a.SystemID, b.SystemID); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	want = slices.Compact(want)

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin hidden directories transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := loadHiddenDirs(ctx, tx)
	if err != nil {
		return false, err
	}
	have := make([]database.HiddenDirectory, 0, len(current))
	for i := range current {
		have = append(have, database.HiddenDirectory{
			SystemID: current[i].SystemID, Path: strings.TrimSuffix(current[i].Prefix, "/"),
		})
	}
	if slices.Equal(have, want) {
		return false, nil
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM HiddenDirectories`); err != nil {
		return false, fmt.Errorf("clear hidden directories: %w", err)
	}
	for _, dir := range want {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO HiddenDirectories(SystemID, Path) VALUES (?, ?)`, dir.SystemID, dir.Path,
		); err != nil {
			return false, fmt.Errorf("write hidden directory: %w", err)
		}
	}
	// Cached random counts and every browse and search cursor describe the
	// library as it was listed before this change.
	if _, err = tx.ExecContext(ctx, "DELETE FROM MediaCountCache"); err != nil {
		return false, fmt.Errorf("invalidate media count cache: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO DBConfig(Name, Value) VALUES (?, '1')
		ON CONFLICT(Name) DO UPDATE SET Value = CAST(DBConfig.Value AS INTEGER) + 1
	`, database.DeviceStateKeyMediaPreferencesRevision); err != nil {
		return false, fmt.Errorf("advance media projection revision: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit hidden directories: %w", err)
	}
	committed = true
	return true, nil
}

// HasMediaUnderDirectory implements MediaDBI.
func (db *MediaDB) HasMediaUnderDirectory(ctx context.Context, systemDBID int64, path string) (bool, error) {
	conn := db.sql.Load()
	if conn == nil {
		return false, ErrNullSQL
	}
	pathClause, args := browsePathPrefixCondition("Path", hiddenDirPrefix(pathutil.CanonicalMediaPath(path)))
	var exists bool
	//nolint:gosec // pathClause is fixed SQL over placeholders.
	query := `SELECT EXISTS(SELECT 1 FROM Media WHERE IsMissing = 0 AND SystemDBID = ? AND ` + pathClause + `)`
	if err := conn.QueryRowContext(ctx, query, append([]any{systemDBID}, args...)...).Scan(&exists); err != nil {
		return false, fmt.Errorf("probe directory media: %w", err)
	}
	return exists, nil
}
