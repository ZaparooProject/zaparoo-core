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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

func (db *MediaDB) GetMediaSourceRoots(ctx context.Context, systemID string) ([]string, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT DISTINCT ms.SourceRoot
		FROM MediaSources ms
		JOIN Media m ON m.DBID = ms.MediaDBID
		JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE s.SystemID = ? AND m.IsMissing = 0
		ORDER BY ms.SourceRoot`, systemID)
	if err != nil {
		return nil, fmt.Errorf("query media source roots: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("close media source root rows")
		}
	}()
	var roots []string
	for rows.Next() {
		var root string
		if scanErr := rows.Scan(&root); scanErr != nil {
			return nil, fmt.Errorf("scan media source root: %w", scanErr)
		}
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

func (db *MediaDB) GetMediaSourcesForScrape(
	ctx context.Context, systemID string, scope *database.ScrapeScope,
) ([]database.MediaSource, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	where := "m.IsMissing = 0 AND m.SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)"
	args := []any{systemID, systemID}
	if scope != nil {
		if scope.SystemID != systemID {
			return nil, fmt.Errorf("media source scope system %q does not match %q", scope.SystemID, systemID)
		}
		var err error
		where, args, err = scrapeScopePredicate(*scope)
		if err != nil {
			return nil, err
		}
		args = append([]any{systemID}, args...)
	}
	rows, err := db.sql.Load().QueryContext(ctx, `
		WITH source_rows AS (
			SELECT m.DBID, m.Path, m.SystemDBID, m.IsMissing,
			       ms.SourcePath, ms.SourceKey, ms.SourceRoot, ms.SourceKind,
			       COUNT(*) OVER (PARTITION BY ms.SourceKey) AS SourceCount
			FROM MediaSources ms
			JOIN Media m ON m.DBID = ms.MediaDBID
			JOIN Systems s ON s.DBID = m.SystemDBID
			WHERE s.SystemID = ? AND m.IsMissing = 0
		)
		SELECT m.DBID, m.Path, m.SystemDBID, m.SourcePath, m.SourceKey,
		       m.SourceRoot, m.SourceKind, m.SourceCount = 1
		FROM source_rows m
		WHERE `+where+`
		ORDER BY m.Path`, args...)
	if err != nil {
		return nil, fmt.Errorf("query media sources for scrape: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("close media source rows")
		}
	}()
	var result []database.MediaSource
	for rows.Next() {
		var source database.MediaSource
		if scanErr := rows.Scan(
			&source.MediaDBID, &source.MediaPath, &source.SystemDBID,
			&source.SourcePath, &source.SourceKey, &source.SourceRoot,
			&source.SourceKind, &source.Unique,
		); scanErr != nil {
			return nil, fmt.Errorf("scan media source: %w", scanErr)
		}
		result = append(result, source)
	}
	return result, rows.Err()
}
