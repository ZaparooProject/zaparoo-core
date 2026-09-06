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

// Unfiltered browse aggregates remain useful when no hidden preferences exist.
// Once one does, use scoped SQL instead: stale aggregate caches must never
// publish the pre-hide counts while a background rebuild catches up.
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

// Hidden-aware route counts must be exact; an unscoped presence probe cannot
// establish whether a particular route still contains visible media.
func sqlVisibleRouteCounts(ctx context.Context, db sqlQueryable, routes []string, systems []systemdefs.System) (
	map[string]database.BrowseRouteCount, error,
) {
	counts := make(map[string]database.BrowseRouteCount, len(routes))
	for _, route := range routes {
		condition, args := browsePathPrefixCondition("m.Path", browseRouteCacheKey(route))
		if systemClause, systemArgs := browseSystemFilterClause("s.SystemID", systems); systemClause != "" {
			condition += " AND " + systemClause
			args = append(args, systemArgs...)
		}
		var count int
		var ids sql.NullString
		err := db.QueryRowContext(ctx, `SELECT COUNT(*), GROUP_CONCAT(DISTINCT s.SystemID)
			FROM `+browseMediaSource(true)+` m JOIN Systems s ON s.DBID = m.SystemDBID
			WHERE m.IsMissing = 0 AND `+condition, args...).Scan(&count, &ids)
		if err != nil {
			return nil, fmt.Errorf("count visible route media: %w", err)
		}
		if count > 0 {
			counts[route] = database.BrowseRouteCount{
				Path: route, FileCount: count, SystemIDs: splitBrowseSystemIDs(ids.String),
			}
		}
	}
	return counts, nil
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
