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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

// MediaSource ties a virtual launcher target to its configured game directory.
// It supplies metadata context only; it never changes indexing or launch paths.
type MediaSource struct {
	MediaPath string
	Directory string
}

// Sources supplements filesystem launcher roots for metadata-only discovery.
type Sources struct {
	Roots []string
	Media []MediaSource
}

// SourceIndex resolves configured identities without guessing from display names.
// Conflicting identities and directories remain ambiguous even when only one of
// their media rows is selected or has not already been scraped.
type SourceIndex struct {
	byMedia map[string]MediaSource
	byDir   map[string]MediaSource
}

func NewSourceIndex(sources []MediaSource) *SourceIndex {
	index := &SourceIndex{byMedia: make(map[string]MediaSource), byDir: make(map[string]MediaSource)}
	for _, source := range sources {
		key := VirtualMediaKey(source.MediaPath)
		if key == "" || !filepath.IsAbs(source.Directory) {
			continue
		}
		source.Directory = filepath.Clean(source.Directory)
		if previous, exists := index.byMedia[key]; exists && previous.Directory != source.Directory {
			index.byMedia[key] = MediaSource{}
		} else if !exists {
			index.byMedia[key] = source
		}
		dirKey := sourceDirectoryKey(source.Directory)
		if previous, exists := index.byDir[dirKey]; exists && VirtualMediaKey(previous.MediaPath) != key {
			index.byDir[dirKey] = MediaSource{}
		} else if !exists {
			index.byDir[dirKey] = source
		}
	}
	return index
}

// VirtualMediaKey ignores the mutable display name but preserves target ID case.
func VirtualMediaKey(path string) string {
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil || parsed.ID == "" {
		return ""
	}
	return virtualpath.CreateVirtualPath(strings.ToLower(parsed.Scheme), parsed.ID, "")
}

func (s *SourceIndex) ForMedia(path string) (MediaSource, bool) {
	source := s.byMedia[VirtualMediaKey(path)]
	return source, source.Directory != ""
}

func (s *SourceIndex) HasMedia(path string) bool {
	_, exists := s.byMedia[VirtualMediaKey(path)]
	return exists
}

func (s *SourceIndex) ForDirectory(path string) (MediaSource, bool) {
	source := s.byDir[sourceDirectoryKey(path)]
	canonical, ok := s.ForMedia(source.MediaPath)
	return canonical, ok && canonical.Directory == source.Directory
}

func sourceDirectoryKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}
