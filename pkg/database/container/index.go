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

package container

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// ParentDir returns the immediate browse parent of an indexed media path,
// including the trailing slash. Virtual media collapses to its scheme prefix
// because those paths have no directory hierarchy.
//
// Indexed paths are normally stored forward-slashed, but one that reached the
// row from filepath.Join carries the host separator instead. Searching only
// for "/" would return nothing for those, and an empty parent resolves to no
// container at all, silently dropping folder artwork on Windows.
func ParentDir(path string) string {
	if idx := strings.Index(path, "://"); idx >= 0 {
		return path[:idx+3]
	}
	slashed := filepath.ToSlash(path)
	if lastSlash := strings.LastIndex(slashed, "/"); lastSlash >= 0 {
		return slashed[:lastSlash+1]
	}
	return ""
}

// Index answers container questions from a set of already-loaded media rows.
// Callers that hold a whole system's media in memory, such as scrapers, use it
// to resolve directories without issuing a query per candidate.
type Index struct {
	byParentDir map[string][]database.Media
	hasNested   map[string]struct{}
}

// NewIndex builds a container index over rows. Missing media is excluded so the
// result matches what the equivalent MediaDB queries would return.
func NewIndex(rows []database.Media) *Index {
	idx := &Index{
		byParentDir: make(map[string][]database.Media),
		hasNested:   make(map[string]struct{}),
	}
	for i := range rows {
		if rows[i].IsMissing {
			continue
		}
		dir := rows[i].ParentDir
		if dir == "" {
			dir = ParentDir(rows[i].Path)
		}
		if dir == "" {
			continue
		}
		dir = normalizeDir(dir)
		idx.byParentDir[dir] = append(idx.byParentDir[dir], rows[i])
	}
	// A directory holds nested media when some deeper directory under it holds
	// media of its own, so every strict ancestor of a populated directory is
	// disqualified from collapsing.
	for dir := range idx.byParentDir {
		for _, ancestor := range ancestorDirs(dir) {
			idx.hasNested[ancestor] = struct{}{}
		}
	}
	return idx
}

// Resolve returns the single logical launch target for dirPath, or nil when the
// directory holds nested media, holds nothing, or is ambiguous.
func (idx *Index) Resolve(dirPath string) *database.Media {
	if idx == nil || dirPath == "" {
		return nil
	}
	prefix := normalizeDir(dirPath)
	if _, nested := idx.hasNested[prefix]; nested {
		return nil
	}
	return SelectLaunchMedia(idx.byParentDir[prefix])
}

// HasMedia reports whether dirPath holds any direct media rows. It lets callers
// tell "not a directory we indexed" apart from "a directory that did not
// collapse".
func (idx *Index) HasMedia(dirPath string) bool {
	if idx == nil || dirPath == "" {
		return false
	}
	prefix := normalizeDir(dirPath)
	if len(idx.byParentDir[prefix]) > 0 {
		return true
	}
	_, nested := idx.hasNested[prefix]
	return nested
}

// normalizeDir gives a directory path its trailing slash without disturbing a
// virtual scheme root such as "steam://", whose own separator is part of it.
func normalizeDir(dir string) string {
	dir = filepath.ToSlash(dir)
	if strings.HasSuffix(dir, "/") {
		return dir
	}
	return dir + "/"
}

func ancestorDirs(dir string) []string {
	if strings.Contains(dir, "://") {
		return nil
	}
	rest := strings.TrimSuffix(dir, "/")
	var out []string
	for {
		idx := strings.LastIndex(rest, "/")
		if idx < 0 {
			break
		}
		out = append(out, rest[:idx+1])
		rest = rest[:idx]
		if rest == "" {
			break
		}
	}
	return out
}
