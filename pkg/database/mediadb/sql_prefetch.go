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

	"github.com/rs/zerolog/log"
)

// prefetchSearchPages reads the tables that the search path joins so that their
// pages are resident in the OS buffer cache when the first user search arrives.
// The SQLite page cache resets after indexing (cache_size shrinks from 32 MB
// back to 8 MB), and freshly-rebuilt indexes have cold on-disk pages. Even a
// single COUNT(*) forces SQLite to walk the btree leaf layer, populating the
// OS cache for subsequent reads.
var prefetchTables = []string{
	"Tags", "TagTypes", "Systems",
	"MediaTitles", "Media",
	"MediaTags", "MediaTitleTags",
}

func (db *MediaDB) prefetchSearchPages(ctx context.Context) error {
	if db.sql == nil {
		return ErrNullSQL
	}
	for _, table := range prefetchTables {
		var count int64
		//nolint:gosec // table names are hardcoded literals, not user input
		if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			log.Warn().Err(err).Str("table", table).Msg("page prefetch failed, skipping")
			continue
		}
		log.Trace().Str("table", table).Int64("rows", count).Msg("prefetch page read complete")
	}
	log.Debug().Int("tables", len(prefetchTables)).Msg("search page prefetch complete")
	return nil
}
