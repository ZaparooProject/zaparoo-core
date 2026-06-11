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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const optionalDBEnrichmentTimeout = 500 * time.Millisecond

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
	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	return mediaIDsByPath(ctx, env.Database.MediaDB, refs)
}

func optionalDBEnrichmentContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, optionalDBEnrichmentTimeout)
}

func mediaIDsByPath(ctx context.Context, db database.MediaDBI, refs []mediaPathRef) map[mediaPathRef]int64 {
	if db == nil || len(refs) == 0 {
		return nil
	}

	wanted := make(map[mediaPathRef]bool, len(refs))
	paths := make([]string, 0, len(refs))
	seenPaths := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.SystemID == "" || ref.Path == "" || wanted[ref] {
			continue
		}
		wanted[ref] = true
		if !seenPaths[ref.Path] {
			seenPaths[ref.Path] = true
			paths = append(paths, ref.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}

	started := time.Now()
	rows, err := db.FindMediaIDsByPaths(ctx, paths)
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve media IDs by path")
		return nil
	}

	mediaIDs := make(map[mediaPathRef]int64, len(rows))
	for _, row := range rows {
		if row.DBID <= 0 {
			continue
		}
		ref := mediaPathRef{SystemID: row.SystemID, Path: row.Path}
		if wanted[ref] {
			mediaIDs[ref] = row.DBID
		}
	}

	log.Debug().
		Int("refs", len(refs)).
		Int("paths", len(paths)).
		Int("resolved", len(mediaIDs)).
		Dur("duration", time.Since(started)).
		Msg("media ID enrichment timing")

	return mediaIDs
}
