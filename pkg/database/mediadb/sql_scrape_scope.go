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
	"runtime"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// scrapeScopePredicate uses a literal range rather than LIKE: wildcard bytes
// remain literal, and the trailing separator excludes neighboring names. Paths
// follow indexed spelling, with native Windows separators normalized.
func scrapeScopePredicate(scope database.ScrapeScope) (predicate string, values []any, err error) {
	if err := scope.Validate(); err != nil {
		return "", nil, fmt.Errorf("invalid scrape scope: %w", err)
	}
	where := "m.IsMissing = 0 AND m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)"
	args := []any{scope.SystemID}
	pathExpr := "m.Path"
	path := scope.Path
	if runtime.GOOS == "windows" && !strings.Contains(path, "://") {
		pathExpr = "replace(m.Path, char(92), '/')"
	}
	if scope.Subtree {
		prefix := strings.TrimRight(path, "/") + "/"
		// '/' is ASCII 47 and '0' is 48, so this range contains exactly
		// descendants of prefix, including filesystem/volume roots.
		where += " AND " + pathExpr + " >= ? AND " + pathExpr + " < ?"
		args = append(args, prefix, strings.TrimSuffix(prefix, "/")+"0")
	} else {
		where += " AND m.DBID = ? AND " + pathExpr + " = ?"
		args = append(args, scope.MediaID, path)
	}
	return where, args, nil
}

func (db *MediaDB) GetScrapeMedia(ctx context.Context, scope database.ScrapeScope) ([]database.MediaFullRow, error) {
	where, args, err := scrapeScopePredicate(scope)
	if err != nil {
		return nil, err
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SystemDBID, m.SortName,
		       t.DBID, t.SystemDBID, t.Slug, t.Name, s.DBID, s.SystemID
		FROM Media m
		JOIN MediaTitles t ON t.DBID = m.MediaTitleDBID
		JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE `+where+` ORDER BY m.Path`, args...)
	if err != nil {
		return nil, fmt.Errorf("query scoped scrape media: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warn().Err(err).Msg("close scoped scrape media rows")
		}
	}()
	result := make([]database.MediaFullRow, 0)
	for rows.Next() {
		var row database.MediaFullRow
		if err := rows.Scan(&row.DBID, &row.Path, &row.ParentDir, &row.MediaTitleDBID,
			&row.SystemDBID, &row.SortName, &row.Title.DBID, &row.Title.SystemDBID,
			&row.Title.Slug, &row.Title.Name, &row.System.DBID, &row.System.SystemID); err != nil {
			return nil, fmt.Errorf("scan scoped scrape media: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (db *MediaDB) GetScopedScrapeMediaIDs(
	ctx context.Context, scope database.ScrapeScope, scraperID, runID string,
) (map[int64]struct{}, error) {
	where, args, err := scrapeScopePredicate(scope)
	if err != nil {
		return nil, err
	}
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	tagType, tag := string(tags.ScraperType(scraperID)), string(tags.TagScraperScraped)
	if runID != "" {
		tagType, tag = string(tags.ScraperRunType(scraperID)), runID
	}
	args = append(args, tagType, tag)
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT m.DBID FROM Media m WHERE `+where+` AND EXISTS (
			SELECT 1 FROM MediaTags mt JOIN Tags t ON t.DBID = mt.TagDBID
			JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE mt.MediaDBID = m.DBID AND tt.Type = ? AND t.Tag = ?
		)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query scoped scrape markers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warn().Err(err).Msg("close scoped scrape marker rows")
		}
	}()
	result := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan scoped scrape marker: %w", err)
		}
		result[id] = struct{}{}
	}
	return result, rows.Err()
}
