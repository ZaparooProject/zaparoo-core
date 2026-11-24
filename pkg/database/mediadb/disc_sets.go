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
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
)

// FindDiscSets implements MediaDBI. The direct rows of every folder are read
// in the same chunked ParentDir IN queries alias resolution uses, and only a
// folder whose rows form a disc set pays for the probe that rules out media
// further down.
func (db *MediaDB) FindDiscSets(
	ctx context.Context, systemDBID int64, dirs []string,
) (map[string][]database.Media, error) {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil, ErrNullSQL
	}
	childDirs := make([]string, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		childDirs = append(childDirs, dir)
	}
	if len(childDirs) == 0 {
		return nil, nil //nolint:nilnil // no folder to look in
	}

	childDirRows := make(map[string][]database.Media, len(childDirs))
	for chunk := range slices.Chunk(childDirs, aliasCandidatesPerQuery) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("find disc sets interrupted: %w", ctxErr)
		}
		if _, err := db.scanContainerDirRows(ctx, systemDBID, chunk, childDirRows); err != nil {
			return nil, err
		}
	}

	sets := make(map[string][]database.Media)
	for dir, rows := range childDirRows {
		var sel container.LaunchSelector
		for i := range rows {
			sel.Add(&rows[i])
		}
		if !sel.MultiDisc() {
			continue
		}
		nested, err := containerHasNestedMedia(ctx, sqlDB, systemDBID, dir)
		if err != nil {
			return nil, err
		}
		if nested {
			continue
		}
		slices.SortFunc(rows, func(a, b database.Media) int {
			return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.DBID, b.DBID))
		})
		sets[dir] = rows
	}
	return sets, nil
}
