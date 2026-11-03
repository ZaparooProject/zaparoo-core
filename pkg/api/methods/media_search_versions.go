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
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

const (
	groupedSearchCursorVersion = 3
	searchGroupByTitle         = "title"
	// titleVersionsLimit bounds the rows read for one title's version list,
	// which is folded and paged in memory.
	titleVersionsLimit = 100_000
)

func encodeGroupedSearchCursor(
	group *database.SearchTitleGroup, sortOrder string, visibility searchVisibility,
) (string, error) {
	return encodeMediaSearchCursorData(cursorData{
		Version:             groupedSearchCursorVersion,
		Sort:                sortOrder,
		SortValue:           group.SortValue,
		LastID:              group.TitleDBID,
		IncludeHidden:       &visibility.IncludeHidden,
		PreferencesRevision: visibility.Revision,
	})
}

func decodeGroupedSearchCursor(cursor, sortOrder string) (*database.SearchCursor, error) {
	data, err := decodeCursorData(cursor)
	if err != nil || data == nil {
		return nil, err
	}
	if data.Version != groupedSearchCursorVersion || data.Sort != sortOrder {
		return nil, models.ClientErrf("cursor does not match grouped search sort %q", sortOrder)
	}
	return &database.SearchCursor{
		SortValue: data.SortValue,
		Sort:      data.Sort,
		LastID:    data.LastID,
	}, nil
}

// foldContainerVersions collapses a title's rows that launch through one
// container, such as a .cue and its .bin tracks or a folder of discs, into
// the row the container launches. A title launch promotes a container's
// member to that same row, so the members are one version, not several.
// Rows keep their input order.
func foldContainerVersions(
	ctx context.Context, mediaDB database.MediaDBI, rows []database.SearchResultWithCursor,
) []database.SearchResultWithCursor {
	type dirKey struct {
		systemID string
		dir      string
	}
	members := make(map[dirKey]int)
	for i := range rows {
		if !container.MayHaveContainerTarget(rows[i].Path) {
			continue
		}
		if dir := container.ParentDir(rows[i].Path); dir != "" {
			members[dirKey{systemID: rows[i].SystemID, dir: dir}]++
		}
	}

	present := make(map[int64]struct{}, len(rows))
	for i := range rows {
		present[rows[i].MediaID] = struct{}{}
	}
	launchByDir := make(map[dirKey]int64)
	for key, count := range members {
		if count < 2 {
			continue
		}
		launch, err := mediaDB.FindSingleContainerLaunchMediaBySystemID(ctx, key.systemID, key.dir)
		if err != nil {
			log.Debug().Err(err).Str("dir", key.dir).Msg("container lookup failed, keeping versions apart")
			continue
		}
		// A launch row the search filtered out (hidden, or outside the tag
		// filter) cannot stand for its members, so they stay apart.
		if launch == nil {
			continue
		}
		if _, ok := present[launch.DBID]; ok {
			launchByDir[key] = launch.DBID
		}
	}
	if len(launchByDir) == 0 {
		return rows
	}

	folded := make([]database.SearchResultWithCursor, 0, len(rows))
	for i := range rows {
		if container.MayHaveContainerTarget(rows[i].Path) {
			key := dirKey{systemID: rows[i].SystemID, dir: container.ParentDir(rows[i].Path)}
			if launchID, ok := launchByDir[key]; ok && rows[i].MediaID != launchID {
				continue
			}
		}
		folded = append(folded, rows[i])
	}
	return folded
}

func systemLaunchers(env *requests.RequestEnv, systemID string) []platforms.Launcher {
	if env.LauncherCache == nil {
		return nil
	}
	return env.LauncherCache.GetLaunchersBySystem(systemID)
}

// defaultTitleVersions folds each group's versions and picks the one a title
// launch would prefer. counts[i] is the number of versions behind results[i].
func defaultTitleVersions(
	env *requests.RequestEnv, groups []database.SearchTitleGroup,
) (results []database.SearchResultWithCursor, counts []int) {
	results = make([]database.SearchResultWithCursor, 0, len(groups))
	counts = make([]int, 0, len(groups))
	for i := range groups {
		versions := foldContainerVersions(env.Context, env.Database.MediaDB, groups[i].Rows)
		chosen := versions[0]
		if len(versions) > 1 {
			chosen = titles.SelectDefaultVersion(versions, env.Config, systemLaunchers(env, chosen.SystemID))
		}
		if len(versions) > database.SlugSearchLimit {
			chosen = launchedTitleVersion(env, &chosen, versions)
		}
		results = append(results, chosen)
		counts = append(counts, len(versions))
	}
	return results, counts
}

// resolveTitle is the resolver a title launch uses, replaceable in tests.
var resolveTitle = titles.ResolveTitle

// launchedTitleVersion returns the version chosen's zapScript launches. A
// launch only weighs the first database.SlugSearchLimit rows of a title, so
// for a larger title it can settle on a different version than ranking every
// version does. The default must be the version its zapScript launches.
func launchedTitleVersion(
	env *requests.RequestEnv, chosen *database.SearchResultWithCursor, versions []database.SearchResultWithCursor,
) database.SearchResultWithCursor {
	mediaType := slugs.MediaTypeGame
	if system, err := systemdefs.GetSystem(chosen.SystemID); err == nil {
		mediaType = system.GetMediaType()
	}
	resolved, err := resolveTitle(env.Context, &titles.ResolveParams{
		SystemID:  chosen.SystemID,
		GameName:  strings.TrimPrefix(chosen.ZapScript(), "@"+chosen.SystemID+"/"),
		MediaDB:   env.Database.MediaDB,
		Cfg:       env.Config,
		Launchers: systemLaunchers(env, chosen.SystemID),
		MediaType: mediaType,
	})
	if err != nil {
		log.Debug().Err(err).Str("zapScript", chosen.ZapScript()).Msg("could not resolve title default")
		return *chosen
	}
	for i := range versions {
		if versions[i].MediaID == resolved.Result.MediaID {
			return versions[i]
		}
	}
	return *chosen
}

// compareTitleVersions orders one title's versions the way the search SQL
// orders rows. Every version shares the title's name, so a name sort reduces
// to the DBID tie-break.
func compareTitleVersions(a, b *database.SearchResultWithCursor, sortOrder string) int {
	order := cmp.Compare(a.MediaID, b.MediaID)
	if sortOrder == "filename-asc" || sortOrder == "filename-desc" {
		order = cmp.Or(cmp.Compare(a.Path, b.Path), order)
	}
	if sortOrder == "name-desc" || sortOrder == "filename-desc" {
		return -order
	}
	return order
}

// pageTitleVersions returns the folded versions after the cursor, up to
// limit rows.
func pageTitleVersions(
	versions []database.SearchResultWithCursor,
	sortOrder string,
	cursor *int64,
	sortCursor *database.SearchCursor,
	limit int,
) []database.SearchResultWithCursor {
	slices.SortStableFunc(versions, func(a, b database.SearchResultWithCursor) int {
		return compareTitleVersions(&a, &b, sortOrder)
	})
	var after *database.SearchResultWithCursor
	switch {
	case sortCursor != nil:
		after = &database.SearchResultWithCursor{Path: sortCursor.SortValue, MediaID: sortCursor.LastID}
	case cursor != nil:
		after = &database.SearchResultWithCursor{MediaID: *cursor}
	}
	start := 0
	if after != nil {
		start = len(versions)
		for i := range versions {
			if compareTitleVersions(&versions[i], after, sortOrder) > 0 {
				start = i
				break
			}
		}
	}
	return versions[start:min(start+limit, len(versions))]
}
