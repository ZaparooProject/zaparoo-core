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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

func resolveMediaBySystemAndPath(
	env *requests.RequestEnv,
	systemID string,
	mediaPath string,
) (*database.MediaFullRow, error) {
	db := env.Database.MediaDB
	system, err := db.FindSystemBySystemID(systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ClientErrf("system not found: %s", systemID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve system: %w", err)
	}

	media, err := db.FindMediaBySystemAndPath(env.Context, system.DBID, mediaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find media: %w", err)
	}
	if media != nil {
		return getMediaFullRow(env, media.DBID, systemID, mediaPath)
	}

	media, err = resolveRelativeMediaPath(env, system, mediaPath)
	if err != nil {
		return nil, err
	}
	if media == nil {
		media, err = resolveSingletonMediaPath(env, system, mediaPath)
		if err != nil {
			return nil, err
		}
	}
	if media == nil {
		return nil, models.ClientErrf("media not found: %s/%s", systemID, mediaPath)
	}

	return getMediaFullRow(env, media.DBID, systemID, media.Path)
}

func getMediaFullRow(
	env *requests.RequestEnv,
	mediaID int64,
	systemID string,
	mediaPath string,
) (*database.MediaFullRow, error) {
	row, err := env.Database.MediaDB.GetMediaWithTitleAndSystem(env.Context, mediaID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media: %w", err)
	}
	if row == nil {
		return nil, models.ClientErrf("media not found: %s/%s", systemID, mediaPath)
	}
	return row, nil
}

func resolveRelativeMediaPath(
	env *requests.RequestEnv,
	system database.System,
	mediaPath string,
) (*database.Media, error) {
	if helpers.ReURI.MatchString(mediaPath) || env.LauncherCache == nil || env.Platform == nil {
		return nil, nil //nolint:nilnil // no relative fallback is available
	}

	remainder, ok := relativeMediaPathRemainder(system.SystemID, mediaPath)
	if !ok || remainder == "" {
		return nil, nil //nolint:nilnil // unsupported relative shape
	}

	var matches []*database.Media
	seenMedia := make(map[int64]bool)
	for _, candidate := range relativeMediaPathCandidates(env, system.SystemID, remainder) {
		media, err := env.Database.MediaDB.FindMediaBySystemAndPath(env.Context, system.DBID, candidate)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve relative media path: %w", err)
		}
		if media == nil || seenMedia[media.DBID] {
			continue
		}
		seenMedia[media.DBID] = true
		matches = append(matches, media)
	}

	switch len(matches) {
	case 0:
		return nil, nil //nolint:nilnil // no relative candidate matched
	case 1:
		return matches[0], nil
	default:
		return nil, models.ClientErrf("ambiguous relative path; use canonical path")
	}
}

func relativeMediaPathRemainder(systemID, mediaPath string) (string, bool) {
	cleaned := filepath.ToSlash(filepath.Clean(mediaPath))
	parts := strings.SplitN(cleaned, "/", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], systemID) {
		return "", false
	}
	return parts[1], true
}

func relativeMediaPathCandidates(env *requests.RequestEnv, systemID, remainder string) []string {
	launchers := env.LauncherCache.GetLaunchersBySystem(systemID)
	if len(launchers) == 0 {
		return nil
	}

	var rootDirs []string
	if env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}

	var candidates []string
	seen := make(map[string]bool)
	addCandidate := func(parts ...string) {
		candidate := filepath.ToSlash(filepath.Clean(filepath.Join(parts...)))
		if !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}

	for i := range launchers {
		launcher := launchers[i]
		if launcher.SkipFilesystemScan {
			continue
		}
		for _, folder := range launcher.Folders {
			if filepath.IsAbs(folder) {
				addCandidate(folder, remainder)
				continue
			}
			for _, root := range rootDirs {
				addCandidate(root, folder, remainder)
			}
		}
	}

	return candidates
}
