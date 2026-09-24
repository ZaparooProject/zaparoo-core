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

package scraper

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

// SourceIndex provides temporary lookup keys over source rows already selected
// from MediaDB. It never discovers launcher configuration or broadens scope.
type SourceIndex struct {
	byMedia  map[string]database.MediaSource
	byPath   map[string]database.MediaSource
	byGroup  map[string][]database.MediaSource
	byParent map[string][]database.MediaSource
}

// NewSourceIndex indexes sources for lookup. A directory shared by variants of
// one game, such as ScummVM's one target per language of a multilingual disc,
// is a group: metadata for the directory applies to every target on it. Any
// other shared source is ambiguous and matches nothing by path.
func NewSourceIndex(sources []database.MediaSource) *SourceIndex {
	index := &SourceIndex{
		byMedia:  make(map[string]database.MediaSource, len(sources)),
		byPath:   make(map[string]database.MediaSource, len(sources)),
		byGroup:  make(map[string][]database.MediaSource),
		byParent: make(map[string][]database.MediaSource),
	}
	var keys []string
	byKey := make(map[string][]database.MediaSource, len(sources))
	for _, source := range sources {
		mediaKey := VirtualMediaKey(source.MediaPath)
		if mediaKey == "" || source.SourceKey == "" {
			continue
		}
		if previous, exists := index.byMedia[mediaKey]; exists && previous.MediaDBID != source.MediaDBID {
			index.byMedia[mediaKey] = database.MediaSource{}
		} else if !exists {
			index.byMedia[mediaKey] = source
		}
		existing, seen := byKey[source.SourceKey]
		if !seen {
			keys = append(keys, source.SourceKey)
		}
		duplicate := func(s database.MediaSource) bool { return s.MediaDBID == source.MediaDBID }
		if !slices.ContainsFunc(existing, duplicate) {
			byKey[source.SourceKey] = append(existing, source)
		}
	}
	for _, key := range keys {
		group := byKey[key]
		if len(group) == 1 && group[0].Unique {
			index.byPath[key] = group[0]
			index.addParent(&group[0])
			continue
		}
		index.byPath[key] = database.MediaSource{}
		if !groupable(group) {
			continue
		}
		index.byGroup[key] = group
		for i := range group {
			index.addParent(&group[i])
		}
	}
	return index
}

// groupable reports whether every source is a directory the database found to
// hold one game. A scoped run can select a single member of a group, which is
// still a group because the database compared it with the others.
func groupable(group []database.MediaSource) bool {
	for i := range group {
		if group[i].SourceKind != "directory" || !group[i].SharedGame {
			return false
		}
	}
	return true
}

// addParent records a directory source under the folder that holds it. A
// source sitting directly in its root has no such folder: the root is the
// collection, not a game.
func (s *SourceIndex) addParent(source *database.MediaSource) {
	if source.SourceKind != "directory" {
		return
	}
	parent := sourcePathKey(filepath.Dir(source.SourcePath))
	if parent == sourcePathKey(source.SourceRoot) {
		return
	}
	s.byParent[parent] = append(s.byParent[parent], *source)
}

// sourcePathKey must fold a path exactly as helpers.NormalizePathForComparison
// does, because that is what the scanner wrote into SourceKey. Calling it
// directly would import a cycle through pkg/launchables.
func sourcePathKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

// VirtualMediaKey ignores mutable display text while preserving scheme and ID.
func VirtualMediaKey(path string) string {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil || parsed.ID == "" {
		return ""
	}
	return virtualpath.CreateVirtualPath(strings.ToLower(parsed.Scheme), parsed.ID, "")
}

func (s *SourceIndex) ForMedia(path string) (database.MediaSource, bool) {
	if s == nil {
		return database.MediaSource{}, false
	}
	source := s.byMedia[VirtualMediaKey(path)]
	return source, source.MediaDBID != 0
}

func (s *SourceIndex) HasMedia(path string) bool {
	_, exists := s.ForMedia(path)
	return exists
}

func (s *SourceIndex) ForPath(path string) (database.MediaSource, bool) {
	if s == nil {
		return database.MediaSource{}, false
	}
	source := s.byPath[sourcePathKey(path)]
	return source, source.MediaDBID != 0
}

// Group returns every target configured on a directory shared by variants of
// one game, or nil when path is not such a directory.
func (s *SourceIndex) Group(path string) []database.MediaSource {
	if s == nil {
		return nil
	}
	return slices.Clone(s.byGroup[sourcePathKey(path)])
}

// Grouped reports whether source is one of the targets sharing a directory
// that Group returns.
func (s *SourceIndex) Grouped(source *database.MediaSource) bool {
	if s == nil {
		return false
	}
	return slices.ContainsFunc(s.byGroup[source.SourceKey], func(member database.MediaSource) bool {
		return member.MediaDBID == source.MediaDBID
	})
}

// UnderParent returns the directory sources held directly inside path, unique
// or grouped, so metadata describing a game folder can reach the variants in
// it.
func (s *SourceIndex) UnderParent(path string) []database.MediaSource {
	if s == nil {
		return nil
	}
	return slices.Clone(s.byParent[sourcePathKey(path)])
}
