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
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

// discSet is a multi-disc folder resolved the way browse lists it: its discs
// in path order and the entry that stands for them, launching the disc last
// played or else the first.
type discSet struct {
	discs []database.Media
	alias database.SingletonContainerAlias
}

func (s *discSet) members() []database.DiscSetMember {
	members := make([]database.DiscSetMember, 0, len(s.discs))
	for i := range s.discs {
		members = append(members, database.DiscSetMember{Path: s.discs[i].Path, MediaDBID: s.discs[i].DBID})
	}
	return members
}

func (s *discSet) mediaDBIDs() []int64 {
	ids := make([]int64, 0, len(s.discs))
	for i := range s.discs {
		ids = append(ids, s.discs[i].DBID)
	}
	return ids
}

// resolveDiscSets finds which of dirs are multi-disc folders of one system and
// resolves each to its browse entry. The map is keyed by folder with a
// trailing slash; folders that are not disc sets are absent.
func resolveDiscSets(
	env *requests.RequestEnv, systemID string, systemDBID int64, dirs []string,
) (map[string]*discSet, error) {
	if len(dirs) == 0 || env.Database == nil || env.Database.MediaDB == nil {
		return nil, nil //nolint:nilnil // no database holds no disc sets
	}
	found, err := env.Database.MediaDB.FindDiscSets(env.Context, systemDBID, dirs)
	if err != nil {
		return nil, fmt.Errorf("failed to find disc sets: %w", err)
	}
	if len(found) == 0 {
		return nil, nil //nolint:nilnil // none of dirs is a disc set
	}
	// Nested media was ruled out, so the direct discs are the whole folder.
	candidates := make([]database.SingletonAliasCandidate, 0, len(found))
	for dir, discs := range found {
		candidates = append(candidates, database.SingletonAliasCandidate{ChildDir: dir, FileCount: len(discs)})
	}
	slices.SortFunc(candidates, func(a, b database.SingletonAliasCandidate) int {
		return strings.Compare(a.ChildDir, b.ChildDir)
	})
	aliases, err := env.Database.MediaDB.ResolveSingletonContainerAliases(env.Context, systemDBID, candidates)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve disc sets: %w", err)
	}
	aliases = preferLastPlayedDiscs(env, systemID, systemDBID, candidates, aliases)
	sets := make(map[string]*discSet, len(aliases))
	for i := range aliases {
		discs, ok := found[aliases[i].ChildDir]
		if !ok || !aliases[i].MultiDisc {
			continue
		}
		sets[aliases[i].ChildDir] = &discSet{alias: aliases[i], discs: discs}
	}
	return sets, nil
}

// resolveDiscSetOf returns the disc set mediaPath is a disc of, or nil.
func resolveDiscSetOf(
	env *requests.RequestEnv, systemID string, systemDBID int64, mediaPath string,
) (*discSet, error) {
	if !container.MayHaveContainerTarget(mediaPath) {
		return nil, nil //nolint:nilnil // an ordinary ROM is no disc set
	}
	dir := container.ParentDir(mediaPath)
	sets, err := resolveDiscSets(env, systemID, systemDBID, []string{dir})
	if err != nil {
		return nil, err
	}
	set := sets[dir]
	if set == nil || !slices.ContainsFunc(set.discs, func(m database.Media) bool { return m.Path == mediaPath }) {
		return nil, nil //nolint:nilnil // mediaPath is not a disc of a set
	}
	return set, nil
}

// resolveFilteredDiscSetAliases collapses the disc set folders of a
// tag-filtered browse page. The page's folder counts are of matching files
// only, so unlike an unfiltered page they cannot rule out nested media and
// every candidate is checked as a disc set instead. Other folders stay
// folders, as they always have on a filtered page.
func resolveFilteredDiscSetAliases(
	env *requests.RequestEnv,
	path string,
	dirs []database.BrowseDirectoryResult,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) map[string]database.SingletonContainerAlias {
	if len(dirs) == 0 || env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}
	order, bySystem := groupSingletonAliasCandidates(path, dirs, systems)
	if len(order) == 0 || (len(systems) == 0 && len(order) > 1) {
		return nil
	}
	var aliases map[string]database.SingletonContainerAlias
	for _, systemID := range order {
		system, err := env.Database.MediaDB.FindSystemBySystemID(systemID)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("browse disc set system lookup failed")
			continue
		}
		childDirs := make([]string, 0, len(bySystem[systemID]))
		for _, candidate := range bySystem[systemID] {
			childDirs = append(childDirs, candidate.ChildDir)
		}
		sets, err := resolveDiscSets(env, systemID, system.DBID, childDirs)
		if err != nil {
			// One system failing leaves the rest of the page resolvable.
			log.Debug().Err(err).Str("path", path).Str("system", systemID).
				Msg("browse disc set resolution failed")
			continue
		}
		for dir, set := range sets {
			if !tagsMatchFilters(set.alias.Tags, tags) {
				continue
			}
			if aliases == nil {
				aliases = make(map[string]database.SingletonContainerAlias, len(sets))
			}
			aliases[dir] = set.alias
		}
	}
	return aliases
}

func tagsMatchFilters(mediaTags []database.TagInfo, filters []zapscript.TagFilter) bool {
	if len(filters) == 0 {
		return true
	}
	hasTag := func(f zapscript.TagFilter) bool {
		return slices.ContainsFunc(mediaTags, func(t database.TagInfo) bool {
			return t.Type == f.Type && t.Tag == f.Value
		})
	}
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(filters)
	for _, f := range andFilters {
		if !hasTag(f) {
			return false
		}
	}
	for _, f := range notFilters {
		if hasTag(f) {
			return false
		}
	}
	for _, group := range database.GroupORFiltersByType(orFilters) {
		if !slices.ContainsFunc(group, hasTag) {
			return false
		}
	}
	return true
}
