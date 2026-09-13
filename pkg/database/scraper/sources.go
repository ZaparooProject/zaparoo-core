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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

// SourceIndex provides temporary lookup keys over source rows already selected
// from MediaDB. It never discovers launcher configuration or broadens scope.
type SourceIndex struct {
	byMedia map[string]database.MediaSource
	byPath  map[string]database.MediaSource
}

func NewSourceIndex(sources []database.MediaSource) *SourceIndex {
	index := &SourceIndex{
		byMedia: make(map[string]database.MediaSource, len(sources)),
		byPath:  make(map[string]database.MediaSource, len(sources)),
	}
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
		if !source.Unique {
			index.byPath[source.SourceKey] = database.MediaSource{}
			continue
		}
		if previous, exists := index.byPath[source.SourceKey]; exists && previous.MediaDBID != source.MediaDBID {
			index.byPath[source.SourceKey] = database.MediaSource{}
		} else if !exists {
			index.byPath[source.SourceKey] = source
		}
	}
	return index
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
	source := s.byPath[strings.ToLower(filepath.ToSlash(filepath.Clean(path)))]
	return source, source.MediaDBID != 0
}
