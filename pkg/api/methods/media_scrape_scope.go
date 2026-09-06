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

package methods

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

func resolveScrapeScope(env *requests.RequestEnv, params models.MediaScrapeParams) (*database.ScrapeScope, error) {
	input := params.Scope
	if input == nil {
		return nil, nil //nolint:nilnil // Absence preserves the legacy systems selector.
	}
	forms := 0
	if input.MediaID != nil {
		forms++
	}
	if input.File != nil {
		forms++
	}
	if input.Subtree != nil {
		forms++
	}
	if forms != 1 || params.Systems != nil {
		return nil, models.ClientErrf("use one scope: mediaId, file, or subtree; cannot mix scope and systems")
	}

	var row *database.MediaFullRow
	var err error
	if input.MediaID != nil {
		if *input.MediaID <= 0 {
			return nil, models.ClientErrf("mediaId must be positive")
		}
		row, err = env.Database.MediaDB.GetMediaWithTitleAndSystem(env.Context, *input.MediaID)
	} else {
		selector := input.File
		if input.Subtree != nil {
			selector = input.Subtree
		}
		system, sysErr := systemdefs.LookupSystem(selector.System)
		if sysErr != nil {
			return nil, models.ClientErrf("invalid scope system: %s", selector.System)
		}
		path, pathErr := database.CanonicalScrapePath(selector.Path, input.Subtree != nil)
		if pathErr != nil {
			return nil, models.ClientErrf("invalid scope path: %w", pathErr)
		}
		dbSystem, sysErr := env.Database.MediaDB.FindSystemBySystemID(system.ID)
		if errors.Is(sysErr, sql.ErrNoRows) {
			return nil, models.ClientErrf("scope system is not indexed: %s", system.ID)
		}
		if sysErr != nil {
			return nil, fmt.Errorf("resolve scrape system: %w", sysErr)
		}
		if input.Subtree != nil {
			return &database.ScrapeScope{SystemID: dbSystem.SystemID, Path: path, Subtree: true}, nil
		}
		media, findErr := env.Database.MediaDB.FindMediaBySystemAndPath(env.Context, dbSystem.DBID, path)
		if findErr == nil && media == nil && !strings.Contains(path, "://") && filepath.FromSlash(path) != path {
			media, findErr = env.Database.MediaDB.FindMediaBySystemAndPath(
				env.Context, dbSystem.DBID, filepath.FromSlash(path),
			)
		}
		if findErr != nil {
			return nil, fmt.Errorf("resolve scrape file: %w", findErr)
		}
		if media == nil || media.IsMissing {
			return nil, models.ClientErrf("scope media not found")
		}
		row, err = env.Database.MediaDB.GetMediaWithTitleAndSystem(env.Context, media.DBID)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (row == nil || row.IsMissing)) {
		return nil, models.ClientErrf("scope media not found")
	}
	if err != nil {
		return nil, fmt.Errorf("resolve scrape media: %w", err)
	}
	path, err := database.CanonicalScrapePath(row.Path, false)
	if err != nil {
		return nil, models.ClientErrf("invalid indexed scrape path: %w", err)
	}
	scope := &database.ScrapeScope{SystemID: row.System.SystemID, Path: path, MediaID: row.DBID}
	if err := scope.Validate(); err != nil {
		return nil, models.ClientErrf("invalid indexed scrape identity: %w", err)
	}
	return scope, nil
}
