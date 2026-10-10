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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// A hidden folder is one entry in the DBConfigHiddenDirectories list, never a
// hidden tag on each file under it: the per-file hidden set is loaded whole
// by every browse and is expected to stay small, and a folder such as an
// arcade alternatives tree holds thousands of files. The folder is applied by
// path instead, in the two ways per-file visibility already works:
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
	// Outer is the Prefix of the nearest hidden folder of the same system
	// that this one sits inside, or empty.
	Outer string
	// Count is the folder's present, not individually hidden media: what an
	// aggregate that counted them has to give back.
	Count int
}

// countedUnder reports whether an aggregate over prefix has to give this
// folder's media back. A folder inside another hidden folder is left to the
// outer one, whose count includes it, unless the aggregate starts inside the
// outer folder and so never counted the rest of it.
func (d *hiddenDir) countedUnder(prefix string) bool {
	return d.within(prefix) && (d.Outer == "" || !strings.HasPrefix(d.Outer, prefix))
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

// storedHiddenDir is one folder of the DBConfigHiddenDirectories list.
type storedHiddenDir struct {
	SystemID string `json:"systemId"`
	Path     string `json:"path"`
}

const hiddenDirsSQL = `SELECT Value FROM DBConfig WHERE Name = ?`

// loadStoredHiddenDirs reads the projected list. No value means no folder is
// hidden, and one this build cannot read is treated the same: the projection
// is rewritten from UserDB at the next sync.
func loadStoredHiddenDirs(ctx context.Context, db sqlQueryable) ([]storedHiddenDir, error) {
	var value string
	err := db.QueryRowContext(ctx, hiddenDirsSQL, DBConfigHiddenDirectories).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && value == "") {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query hidden directories: %w", err)
	}
	var stored []storedHiddenDir
	if jsonErr := json.Unmarshal([]byte(value), &stored); jsonErr != nil {
		log.Warn().Err(jsonErr).Msg("ignoring unreadable hidden directories projection")
		return nil, nil
	}
	return stored, nil
}

// loadHiddenDirs returns the hidden folders without their counts.
func loadHiddenDirs(ctx context.Context, db sqlQueryable) ([]hiddenDir, error) {
	stored, err := loadStoredHiddenDirs(ctx, db)
	if err != nil {
		return nil, err
	}
	dirs := make([]hiddenDir, 0, len(stored))
	for i := range stored {
		dirs = append(dirs, hiddenDir{SystemID: stored[i].SystemID, Prefix: hiddenDirPrefix(stored[i].Path)})
	}
	for i := range dirs {
		for j := range dirs {
			outer := dirs[j].Prefix
			if i != j && dirs[j].SystemID == dirs[i].SystemID && outer != dirs[i].Prefix &&
				strings.HasPrefix(dirs[i].Prefix, outer) && len(outer) > len(dirs[i].Outer) {
				dirs[i].Outer = outer
			}
		}
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

// hiddenDirCountCache keeps each hidden folder's count per database handle.
// Counting a folder reads its whole index range, and every browse and system
// listing needs every folder's count, so a hidden system of tens of thousands
// of files would otherwise be recounted on each of those reads. A count stays
// good until media rows change (invalidateCaches) or visibility does (the
// preferences revision, which moves with every hide and unhide).
type hiddenDirCountCache struct {
	counts     map[hiddenDirCountKey]int
	revision   string
	generation uint64
}

type hiddenDirCountKey struct {
	systemID string
	prefix   string
}

var (
	hiddenDirCountCacheMu  syncutil.Mutex
	hiddenDirCountCacheMap map[sqlQueryable]*hiddenDirCountCache
)

// clearHiddenDirCountCacheFor forgets a handle's counts after its media rows
// changed. A count that was being taken across the change is not stored.
func clearHiddenDirCountCacheFor(db sqlQueryable) {
	if db == nil {
		return
	}
	hiddenDirCountCacheMu.Lock()
	defer hiddenDirCountCacheMu.Unlock()
	if entry := hiddenDirCountCacheMap[db]; entry != nil {
		entry.generation++
		entry.counts = nil
	}
}

func forgetHiddenDirCountCacheFor(db sqlQueryable) {
	if db == nil {
		return
	}
	hiddenDirCountCacheMu.Lock()
	defer hiddenDirCountCacheMu.Unlock()
	delete(hiddenDirCountCacheMap, db)
}

// loadHiddenDirCounts fills in each folder's Count.
func loadHiddenDirCounts(ctx context.Context, db sqlQueryable, dirs []hiddenDir) error {
	if len(dirs) == 0 {
		return nil
	}
	var revision string
	err := db.QueryRowContext(ctx, `SELECT Value FROM DBConfig WHERE Name = ?`,
		database.DeviceStateKeyMediaPreferencesRevision).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read media projection revision: %w", err)
	}

	handle := cacheHandle(db)
	hiddenDirCountCacheMu.Lock()
	if hiddenDirCountCacheMap == nil {
		hiddenDirCountCacheMap = make(map[sqlQueryable]*hiddenDirCountCache)
	}
	entry := hiddenDirCountCacheMap[handle]
	if entry == nil {
		entry = &hiddenDirCountCache{}
		hiddenDirCountCacheMap[handle] = entry
	}
	if entry.revision != revision {
		entry.revision = revision
		entry.counts = nil
	}
	generation := entry.generation
	var missing []int
	for i := range dirs {
		count, ok := entry.counts[hiddenDirCountKey{systemID: dirs[i].SystemID, prefix: dirs[i].Prefix}]
		if !ok {
			missing = append(missing, i)
			continue
		}
		dirs[i].Count = count
	}
	hiddenDirCountCacheMu.Unlock()

	for _, i := range missing {
		upper := stringPrefixUpperBound(dirs[i].Prefix)
		if upper == "" {
			continue
		}
		if err := db.QueryRowContext(ctx, hiddenDirCountSQL, dirs[i].SystemID, dirs[i].Prefix, upper).
			Scan(&dirs[i].Count); err != nil {
			return fmt.Errorf("count hidden directory media: %w", err)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	hiddenDirCountCacheMu.Lock()
	defer hiddenDirCountCacheMu.Unlock()
	if entry.generation != generation || entry.revision != revision {
		return nil
	}
	if entry.counts == nil {
		entry.counts = make(map[hiddenDirCountKey]int, len(dirs))
	}
	for _, i := range missing {
		entry.counts[hiddenDirCountKey{systemID: dirs[i].SystemID, prefix: dirs[i].Prefix}] = dirs[i].Count
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

	want := make([]storedHiddenDir, 0, len(dirs))
	for _, dir := range dirs {
		dir.Path = pathutil.CanonicalMediaPath(dir.Path)
		if dir.SystemID == "" || dir.Path == "" {
			continue
		}
		want = append(want, storedHiddenDir{SystemID: dir.SystemID, Path: dir.Path})
	}
	slices.SortFunc(want, func(a, b storedHiddenDir) int {
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

	have, err := loadStoredHiddenDirs(ctx, tx)
	if err != nil {
		return false, err
	}
	if slices.Equal(have, want) {
		return false, nil
	}

	value, err := json.Marshal(want)
	if err != nil {
		return false, fmt.Errorf("encode hidden directories: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO DBConfig(Name, Value) VALUES (?, ?)
		ON CONFLICT(Name) DO UPDATE SET Value = excluded.Value
	`, DBConfigHiddenDirectories, string(value)); err != nil {
		return false, fmt.Errorf("write hidden directories: %w", err)
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
