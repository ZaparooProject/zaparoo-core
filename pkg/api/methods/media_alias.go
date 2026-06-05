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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

func singletonMediaAliasesEnabled(env *requests.RequestEnv) bool {
	return env != nil && env.Platform != nil && env.Platform.Settings().ZipsAsDirs
}

func resolveSingletonMediaPath(
	env *requests.RequestEnv,
	system database.System,
	mediaPath string,
) (*database.Media, error) {
	if !singletonMediaAliasesEnabled(env) {
		return nil, nil //nolint:nilnil // disabled aliasing has no singleton fallback
	}

	media, err := env.Database.MediaDB.FindSingleDescendantMedia(env.Context, system.DBID, mediaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve singleton media path: %w", err)
	}
	return media, nil
}

func equivalentMediaIDs(env *requests.RequestEnv, row *database.MediaFullRow) ([]int64, error) {
	if row == nil {
		return nil, nil
	}

	ids := []int64{row.DBID}
	if env == nil || env.Database == nil || env.Database.MediaDB == nil || !singletonMediaAliasesEnabled(env) {
		return ids, nil
	}
	seen := map[int64]bool{row.DBID: true}
	add := func(media *database.Media) {
		if media == nil || seen[media.DBID] {
			return
		}
		seen[media.DBID] = true
		ids = append(ids, media.DBID)
	}

	if helpers.IsZip(row.Path) {
		child, err := env.Database.MediaDB.FindSingleDescendantMedia(env.Context, row.System.DBID, row.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to find child alias media: %w", err)
		}
		add(child)
	}

	parentPath := strings.TrimSuffix(row.ParentDir, "/")
	if parentPath == "" || parentPath == row.Path || !helpers.IsZip(parentPath) {
		return ids, nil
	}

	onlyChild, err := env.Database.MediaDB.FindSingleDescendantMedia(env.Context, row.System.DBID, parentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to verify parent alias media: %w", err)
	}
	if onlyChild == nil || onlyChild.DBID != row.DBID {
		return ids, nil
	}

	parent, err := env.Database.MediaDB.FindMediaBySystemAndPath(env.Context, row.System.DBID, parentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find parent alias media: %w", err)
	}
	add(parent)

	return ids, nil
}

func mergeMediaTags(primary []database.TagInfo, aliases ...[]database.TagInfo) []database.TagInfo {
	if len(aliases) == 0 {
		return primary
	}
	merged := make([]database.TagInfo, 0, len(primary))
	seen := make(map[string]bool)
	appendUnique := func(tags []database.TagInfo) {
		for _, tag := range tags {
			key := tag.Type + "\x00" + tag.Tag
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, tag)
		}
	}
	appendUnique(primary)
	for _, tags := range aliases {
		appendUnique(tags)
	}
	return merged
}

func mergeMediaProperties(
	primary []database.MediaProperty,
	aliases ...[]database.MediaProperty,
) []database.MediaProperty {
	if len(aliases) == 0 {
		return primary
	}
	merged := make([]database.MediaProperty, 0, len(primary))
	seen := make(map[string]bool)
	appendUnique := func(props []database.MediaProperty) {
		for _, prop := range props {
			if seen[prop.TypeTag] {
				continue
			}
			seen[prop.TypeTag] = true
			merged = append(merged, prop)
		}
	}
	appendUnique(primary)
	for _, props := range aliases {
		appendUnique(props)
	}
	return merged
}
