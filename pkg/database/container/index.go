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

// SourceVirtualScheme matches platforms.SourceScheme (pkg/platforms/source_roots.go).
// Duplicated here as a literal instead of imported, to avoid an import cycle:
// pkg/platforms -> pkg/database/scraper -> pkg/database/container.
const SourceVirtualScheme = "source"

// ParentDir returns the immediate browse parent of an indexed media path,
// including the trailing slash. Most virtual media collapses to its scheme
// prefix because those paths have no directory hierarchy (e.g. an Android app
// path is scheme://package:variant/Name, always exactly one level deep). A
// source root path is the one virtual scheme with real nested folders
// (scheme://id/dir/.../file, walked by mediascanner's source root indexing),
// so it gets the same last-segment parent a filesystem path gets instead.
//
// Indexed paths are normally stored forward-slashed, but one that reached the
// row from filepath.Join carries the host separator instead. Searching only
// for "/" would return nothing for those, and an empty parent resolves to no
// container at all, silently dropping folder artwork on Windows.
func ParentDir(path string) string {
	if idx := strings.Index(path, "://"); idx >= 0 {
		if path[:idx] == SourceVirtualScheme {
			if lastSlash := strings.LastIndex(path, "/"); lastSlash > idx+2 {
				return path[:lastSlash+1]
			}
		}
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

// WatchedIndex answers Index's questions for a fixed set of directories from
// rows added one at a time. It keeps a fixed amount of state per watched
// directory rather than every row, so a caller streaming a large system pays
// for the directories it asks about, not for the system. Its answers for a
// directory that was not watched are always empty.
type WatchedIndex struct {
	dirs    map[string]*watchedDir
	lastDir string
}

type watchedDir struct {
	sel    LaunchSelector
	nested bool
}

// NewWatchedIndex returns an index that will answer for dirs.
func NewWatchedIndex(dirs []string) *WatchedIndex {
	// Most watched directories never see a row, so their state is only
	// allocated when one does.
	w := &WatchedIndex{dirs: make(map[string]*watchedDir, len(dirs))}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		w.dirs[watchKey(dir)] = nil
	}
	return w
}

// watchKey identifies a directory as normalizeDir does, but without the
// trailing slash, so keying by it costs no allocation for the many watched
// paths that arrive without one.
func watchKey(dir string) string {
	dir = filepath.ToSlash(dir)
	if len(dir) > 1 && strings.HasSuffix(dir, "/") && !strings.HasSuffix(dir, "://") {
		return dir[:len(dir)-1]
	}
	return dir
}

// state returns dir's state, allocating it, or nil when dir is not watched.
func (w *WatchedIndex) state(dir string) *watchedDir {
	wd, ok := w.dirs[dir]
	if !ok {
		return nil
	}
	if wd == nil {
		wd = &watchedDir{}
		w.dirs[dir] = wd
	}
	return wd
}

// Add records one media row under the same rules NewIndex applies.
func (w *WatchedIndex) Add(row *database.Media) {
	if row.IsMissing || len(w.dirs) == 0 {
		return
	}
	dir := row.ParentDir
	if dir == "" {
		dir = ParentDir(row.Path)
	}
	if dir == "" {
		return
	}
	key := watchKey(dir)
	if wd := w.state(key); wd != nil {
		wd.sel.Add(row)
	}
	// Marking ancestors is idempotent, and consecutive rows in path order
	// usually share a directory, so the walk is skipped for a repeat.
	if key == w.lastDir {
		return
	}
	w.lastDir = key
	if strings.Contains(key, "://") {
		return
	}
	rest := strings.TrimSuffix(key, "/")
	for {
		idx := strings.LastIndex(rest, "/")
		if idx < 0 {
			return
		}
		ancestor := rest[:idx]
		if idx == 0 {
			ancestor = "/"
		}
		if wd := w.state(ancestor); wd != nil {
			wd.nested = true
		}
		rest = rest[:idx]
		if rest == "" {
			return
		}
	}
}

// Resolve answers as Index.Resolve would over every added row.
func (w *WatchedIndex) Resolve(dirPath string) *database.Media {
	if w == nil || dirPath == "" {
		return nil
	}
	wd := w.dirs[watchKey(dirPath)]
	if wd == nil || wd.nested {
		return nil
	}
	return wd.sel.Result()
}

// HasMedia answers as Index.HasMedia would over every added row.
func (w *WatchedIndex) HasMedia(dirPath string) bool {
	if w == nil || dirPath == "" {
		return false
	}
	wd := w.dirs[watchKey(dirPath)]
	return wd != nil && (wd.nested || wd.sel.Count() > 0)
}

// LaunchTargets resolves every directory of a system from rows added one at a
// time, keeping a fixed amount of state per directory rather than every row.
// Once all rows are added, Map answers Resolve as an Index over them would.
type LaunchTargets struct {
	selectors map[string]*LaunchSelector
	nested    map[string]struct{}
	lastDir   string
}

// NewLaunchTargets returns an empty LaunchTargets.
func NewLaunchTargets() *LaunchTargets {
	return &LaunchTargets{
		selectors: make(map[string]*LaunchSelector),
		nested:    make(map[string]struct{}),
	}
}

// Add records one media row under the same rules NewIndex applies.
func (t *LaunchTargets) Add(row *database.Media) {
	if row.IsMissing {
		return
	}
	dir := row.ParentDir
	if dir == "" {
		dir = ParentDir(row.Path)
	}
	if dir == "" {
		return
	}
	dir = normalizeDir(dir)
	sel, ok := t.selectors[dir]
	if !ok {
		sel = &LaunchSelector{}
		t.selectors[dir] = sel
	}
	sel.Add(row)
	if dir == t.lastDir {
		return
	}
	t.lastDir = dir
	for _, ancestor := range ancestorDirs(dir) {
		t.nested[ancestor] = struct{}{}
	}
}

// Map returns the launch target of every directory that has one. The
// per-directory state is released; t must not be used afterwards.
func (t *LaunchTargets) Map() LaunchTargetMap {
	targets := make(LaunchTargetMap)
	for dir, sel := range t.selectors {
		if _, nested := t.nested[dir]; nested {
			continue
		}
		if target := sel.Result(); target != nil {
			targets[dir] = *target
		}
	}
	t.selectors, t.nested = nil, nil
	return targets
}

// LaunchTargetMap holds the directories that collapse to a single launch
// target, keyed by normalized directory.
type LaunchTargetMap map[string]database.Media

// Resolve answers as Index.Resolve would.
func (m LaunchTargetMap) Resolve(dirPath string) *database.Media {
	if dirPath == "" {
		return nil
	}
	target, ok := m[normalizeDir(dirPath)]
	if !ok {
		return nil
	}
	return &target
}
