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
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

type mediaPathRef struct {
	SystemID string
	Path     string
}

func mediaResponseRelativePath(env *requests.RequestEnv, systemID, path string) *string {
	if env == nil || env.LauncherCache == nil || env.Platform == nil {
		return nil
	}

	rootDirs := env.Platform.RootDirs(env.Config)
	rel := env.LauncherCache.ToRelativePath(rootDirs, systemID, path)
	if rel == path {
		return nil
	}
	return &rel
}

func mediaResponseMediaIDs(env *requests.RequestEnv, refs []mediaPathRef) map[mediaPathRef]int64 {
	if env == nil || env.Database == nil {
		return nil
	}
	return mediaIDsByPath(env.Context, env.Database.MediaDB, refs)
}

func mediaIDsByPath(ctx context.Context, db database.MediaDBI, refs []mediaPathRef) map[mediaPathRef]int64 {
	if db == nil || len(refs) == 0 {
		return nil
	}

	pathsBySystem := make(map[string][]string)
	seen := make(map[mediaPathRef]bool)
	for _, ref := range refs {
		if ref.SystemID == "" || ref.Path == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		pathsBySystem[ref.SystemID] = append(pathsBySystem[ref.SystemID], ref.Path)
	}

	mediaIDs := make(map[mediaPathRef]int64)
	for systemID, paths := range pathsBySystem {
		system, err := db.FindSystemBySystemID(systemID)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("could not resolve media IDs for system")
			continue
		}

		mediaByPath, err := db.FindMediaBySystemAndPaths(ctx, system.DBID, paths)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("could not resolve media IDs by path")
			continue
		}

		for path, media := range mediaByPath {
			if media.DBID > 0 {
				mediaIDs[mediaPathRef{SystemID: systemID, Path: path}] = media.DBID
			}
		}
	}

	return mediaIDs
}
