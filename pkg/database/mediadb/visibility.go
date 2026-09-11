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
	"strings"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// Visibility is a file-level preference, never inherited from a title. Keep
// its SQL predicate independent of metadata tags or launch resolution.
const hiddenMediaIDsSQL = `SELECT mt.MediaDBID FROM MediaTags mt
	JOIN Tags t ON t.DBID = mt.TagDBID
	JOIN TagTypes tt ON tt.DBID = t.TypeDBID
	WHERE tt.Type = 'user' AND t.Tag = 'hidden'`

// Only non-missing rows, because every browse aggregate this set is subtracted
// from already counts non-missing media alone.
const hiddenMediaRowsSQL = `SELECT m.Path, s.SystemID FROM MediaTags mt
	JOIN Tags t ON t.DBID = mt.TagDBID
	JOIN TagTypes tt ON tt.DBID = t.TypeDBID
	JOIN Media m ON m.DBID = mt.MediaDBID
	JOIN Systems s ON s.DBID = m.SystemDBID
	WHERE tt.Type = 'user' AND t.Tag = 'hidden' AND m.IsMissing = 0`

// MediaPreferencesRevision changes atomically with the tag projection. The
// UserDB revision alone cannot distinguish reads between intent and projection
// writes, which can overlap HTTP requests on different database connections.
func (db *MediaDB) MediaPreferencesRevision(ctx context.Context) (string, error) {
	conn := db.sql.Load()
	if conn == nil {
		return "", ErrNullSQL
	}
	var revision string
	err := conn.QueryRowContext(ctx, `SELECT Value FROM DBConfig WHERE Name = ?`,
		database.DeviceStateKeyMediaPreferencesRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read media projection revision: %w", err)
	}
	return revision, nil
}

func browseVisibilityCondition(column string, excludeHidden bool) string {
	if !excludeHidden {
		return ""
	}
	return " AND " + column + " NOT IN (" + hiddenMediaIDsSQL + ")"
}

func browseMediaSource(excludeHidden bool) string {
	if !excludeHidden {
		return "Media"
	}
	return "(SELECT * FROM Media WHERE DBID NOT IN (" + hiddenMediaIDsSQL + "))"
}

// hiddenMediaEntry locates one hidden row well enough to decide which browse
// scopes it was counted in.
type hiddenMediaEntry struct {
	Path     string
	SystemID string
}

// hiddenMedia is the set of hidden, non-missing media rows. Hiding is a
// deliberate per-file act, so this set stays small next to the library. Browse
// keeps its cached aggregates and subtracts this set from them; recomputing
// those aggregates from Media instead costs tens of seconds on a real library,
// because the caches are the only reason a browse page is not a table scan.
type hiddenMedia struct {
	entries []hiddenMediaEntry
}

// loadHiddenMedia returns nil when visibility filtering is off or nothing is
// hidden, so callers can keep their unfiltered path with a single nil check.
func loadHiddenMedia(ctx context.Context, db sqlQueryable, excludeHidden bool) (*hiddenMedia, error) {
	if !excludeHidden {
		return nil, nil //nolint:nilnil // Absence of a set is the "nothing to subtract" signal.
	}
	rows, err := db.QueryContext(ctx, hiddenMediaRowsSQL)
	if err != nil {
		return nil, fmt.Errorf("query hidden media: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var entries []hiddenMediaEntry
	for rows.Next() {
		var entry hiddenMediaEntry
		if scanErr := rows.Scan(&entry.Path, &entry.SystemID); scanErr != nil {
			return nil, fmt.Errorf("scan hidden media: %w", scanErr)
		}
		entries = append(entries, entry)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("read hidden media: %w", rowsErr)
	}
	if len(entries) == 0 {
		return nil, nil //nolint:nilnil // See above.
	}
	return &hiddenMedia{entries: entries}, nil
}

func (h *hiddenMedia) empty() bool {
	return h == nil || len(h.entries) == 0
}

func hiddenMatchesSystems(systemID string, systems []systemdefs.System) bool {
	if len(systems) == 0 {
		return true
	}
	for i := range systems {
		if systems[i].ID == systemID {
			return true
		}
	}
	return false
}

// countUnder reports how many hidden rows a browse scope counted, matching the
// plain string-prefix bounds browsePathPrefixCondition builds.
func (h *hiddenMedia) countUnder(prefix string, systems []systemdefs.System) int {
	if h.empty() {
		return 0
	}
	count := 0
	for i := range h.entries {
		if strings.HasPrefix(h.entries[i].Path, prefix) &&
			hiddenMatchesSystems(h.entries[i].SystemID, systems) {
			count++
		}
	}
	return count
}

// countForSystem reports the hidden rows a per-system total counted.
func (h *hiddenMedia) countForSystem(systemID string) int {
	if h.empty() {
		return 0
	}
	count := 0
	for i := range h.entries {
		if h.entries[i].SystemID == systemID {
			count++
		}
	}
	return count
}

// childCounts groups hidden rows by the immediate child directory of prefix
// they sit beneath. Direct files under prefix have no child segment and are
// left out, because they change no directory's count.
func (h *hiddenMedia) childCounts(prefix string, systems []systemdefs.System) map[string]int {
	if h.empty() {
		return nil
	}
	counts := make(map[string]int)
	for i := range h.entries {
		if !strings.HasPrefix(h.entries[i].Path, prefix) ||
			!hiddenMatchesSystems(h.entries[i].SystemID, systems) {
			continue
		}
		rest := h.entries[i].Path[len(prefix):]
		slash := strings.Index(rest, "/")
		if slash <= 0 {
			continue
		}
		counts[rest[:slash]]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// childNameCount counts the distinct child directories across prefixes that
// hold hidden media, which bounds how many rows a page can lose.
func (h *hiddenMedia) childNameCount(prefixes []string, systems []systemdefs.System) int {
	if h.empty() {
		return 0
	}
	names := make(map[string]struct{})
	for _, prefix := range prefixes {
		for name := range h.childCounts(prefix, systems) {
			names[name] = struct{}{}
		}
	}
	return len(names)
}

// applyHiddenToDirectories subtracts hidden rows from cached directory counts
// and drops directories left with nothing visible. An overlay row carries the
// winning route's path, which is the only route its count came from; a plain
// listing joins the browsed prefix to the child name instead.
func applyHiddenToDirectories(
	results []database.BrowseDirectoryResult,
	hidden *hiddenMedia,
	pathPrefix string,
	systems []systemdefs.System,
) []database.BrowseDirectoryResult {
	if hidden.empty() {
		return results
	}
	filtered := results[:0]
	for i := range results {
		prefix := results[i].Path
		if prefix == "" {
			prefix = pathPrefix + results[i].Name
		}
		results[i].FileCount -= hidden.countUnder(prefix+"/", systems)
		if results[i].FileCount <= 0 {
			continue
		}
		filtered = append(filtered, results[i])
	}
	return filtered
}

// browseHiddenFetchLimit over-fetches by the number of directories hidden media
// could empty, so a page still fills once they are dropped.
func browseHiddenFetchLimit(limit, dropCandidates int) int {
	if limit <= 0 || dropCandidates == 0 {
		return limit
	}
	return limit + dropCandidates
}

func browseTruncateDirectories(
	results []database.BrowseDirectoryResult, limit int,
) []database.BrowseDirectoryResult {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

// applyHiddenToRouteCounts removes hidden media from cached route totals and
// drops routes left empty, so a fully hidden route stops being a browse root.
func applyHiddenToRouteCounts(
	counts map[string]database.BrowseRouteCount, hidden *hiddenMedia, systems []systemdefs.System,
) map[string]database.BrowseRouteCount {
	if hidden.empty() {
		return counts
	}
	for route, count := range counts {
		count.FileCount -= hidden.countUnder(browseRouteCacheKey(route), systems)
		if count.FileCount <= 0 {
			delete(counts, route)
			continue
		}
		counts[route] = count
	}
	return counts
}

// applyHiddenToVirtualSchemes removes hidden media from cached scheme totals.
func applyHiddenToVirtualSchemes(
	schemes []database.BrowseVirtualScheme, hidden *hiddenMedia, systems []systemdefs.System,
) []database.BrowseVirtualScheme {
	if hidden.empty() {
		return schemes
	}
	filtered := schemes[:0]
	for i := range schemes {
		schemes[i].FileCount -= hidden.countUnder(schemes[i].Scheme, systems)
		if schemes[i].FileCount <= 0 {
			continue
		}
		filtered = append(filtered, schemes[i])
	}
	return filtered
}

// sqlNeedsVisibilityFilter is the presence probe row-level queries use before
// paying for a NOT filter. Aggregates do not go through it: they keep their
// cached counts and subtract the hidden set instead.
func sqlNeedsVisibilityFilter(ctx context.Context, db sqlQueryable, excludeHidden bool) (bool, error) {
	if !excludeHidden {
		return false, nil
	}
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS("+hiddenMediaIDsSQL+")").Scan(&exists); err != nil {
		return false, fmt.Errorf("check hidden media: %w", err)
	}
	return exists, nil
}

// sqlAnyVisibleMedia reports whether a subtree still holds media once hidden
// rows are removed. Reading in path order stops at the first visible row, so
// the scan is bounded by the hidden run at the start of the subtree.
func sqlAnyVisibleMedia(
	ctx context.Context, db sqlQueryable, prefix string, systems []systemdefs.System,
) (bool, error) {
	pathCondition, args := browsePathPrefixCondition("m.Path", prefix)
	query := `SELECT EXISTS(SELECT 1 FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE m.IsMissing = 0 AND ` + pathCondition
	if systemClause, systemArgs := browseSystemFilterClause("s.SystemID", systems); systemClause != "" {
		query += ` AND ` + systemClause
		args = append(args, systemArgs...)
	}
	query += browseVisibilityCondition("m.DBID", true) + ` LIMIT 1)`
	var exists bool
	if err := db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("probe visible media: %w", err)
	}
	return exists, nil
}

// discoveryExcludesHidden is the shared rule for whether a query counts as
// ordinary discovery. A required user:hidden or user:favorite filter is an
// explicit ask for those entries and opts out of the exclusion, matching what
// browse and search do with the same filters.
func discoveryExcludesHidden(tags []zapscript.TagFilter) bool {
	return !filters.IncludesHidden(tags, false)
}

func discoveryTags(ctx context.Context, db sqlQueryable, tags []zapscript.TagFilter, excludeHidden bool) (
	[]zapscript.TagFilter, error,
) {
	needed, err := sqlNeedsVisibilityFilter(ctx, db, excludeHidden)
	if err != nil {
		return nil, err
	}
	if needed {
		return filters.ExcludeHidden(tags), nil
	}
	return tags, nil
}
