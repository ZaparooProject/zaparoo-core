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

package localmedia

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esmedia"
)

// sourceDirListings caches ReadSourceDir results for one scrape run, the same
// way mediascanner/source_roots.go's sourceListings does for indexing: an
// artwork lookup probes the same handful of media/<subdir> directories for
// every row in a system, and a host round-trip per probe would make scraping
// an Android library cost as many SAF calls as a full reindex.
type sourceDirListings struct {
	reader platforms.SourceRootReader
	dirs   map[string][]platforms.SourceEntry
}

func newSourceDirListings(reader platforms.SourceRootReader) *sourceDirListings {
	return &sourceDirListings{reader: reader, dirs: make(map[string][]platforms.SourceEntry)}
}

func (l *sourceDirListings) list(ctx context.Context, dirPath string) []platforms.SourceEntry {
	if entries, ok := l.dirs[dirPath]; ok {
		return entries
	}
	entries, err := l.reader.ReadSourceDir(ctx, dirPath)
	if err != nil {
		entries = nil
	}
	l.dirs[dirPath] = entries
	return entries
}

func (l *sourceDirListings) hasFile(ctx context.Context, dirPath, name string) bool {
	for _, entry := range l.list(ctx, dirPath) {
		if !entry.Dir && entry.Name == name {
			return true
		}
	}
	return false
}

// sourceSegmentsPath builds the canonical source path for id and its decoded
// segments, the same construction mediascanner/source_roots.go's unexported
// sourcePath does (segments nil or empty means the bare root).
func sourceSegmentsPath(id string, segments []string) (string, error) {
	if len(segments) == 0 {
		return platforms.SourceScheme + "://" + id, nil
	}
	path, err := virtualpath.CreateVirtualPathSegments(platforms.SourceScheme, id, segments)
	if err != nil {
		return "", fmt.Errorf("build source path: %w", err)
	}
	return path, nil
}

// sourceRelativeSegments returns path's decoded segments below
// systemRootPath, both source paths under the same root id, or nil if path
// does not resolve under that root (a different root, or not a source path
// at all) - the source-root equivalent of esmedia.ResolvePath's
// root-membership check, working on decoded segments instead of real
// filesystem paths.
func sourceRelativeSegments(path, systemRootPath string) []string {
	rootID, rootSegments, rootErr := platforms.SourceLocation(systemRootPath)
	if rootErr != nil {
		return nil
	}
	pathID, pathSegments, pathErr := platforms.SourceLocation(path)
	if pathErr != nil || pathID != rootID || len(pathSegments) <= len(rootSegments) {
		return nil
	}
	for i, segment := range rootSegments {
		if pathSegments[i] != segment {
			return nil
		}
	}
	return pathSegments[len(rootSegments):]
}

// sourceArtworkFallbackNames is esmedia.ArtworkFallbackNames's source-root
// equivalent: gamePath's candidate artwork names, nested-subfolder name
// first then the flat name, built from decoded path segments instead of
// filepath.Rel (which mangles "://").
func sourceArtworkFallbackNames(gamePath, systemRootPath string) []string {
	relative := sourceRelativeSegments(gamePath, systemRootPath)
	if len(relative) == 0 {
		return nil
	}
	leaf := relative[len(relative)-1]
	stem := strings.TrimSuffix(leaf, filepath.Ext(leaf))
	if stem == "" {
		return nil
	}

	flatNames := esmedia.FallbackArtworkNames(stem)
	dirSegments := relative[:len(relative)-1]
	if len(dirSegments) == 0 {
		return flatNames
	}

	dirPrefix := strings.Join(dirSegments, "/")
	fallbackNames := make([]string, 0, len(flatNames)*2)
	for _, flat := range flatNames {
		fallbackNames = append(fallbackNames, dirPrefix+"/"+flat)
	}
	fallbackNames = append(fallbackNames, flatNames...)
	return fallbackNames
}

// sourceContainerArtworkFallbackNames is esmedia.ContainerArtworkFallbackNames's
// source-root equivalent: candidate artwork names for the directory holding
// gamePath, not gamePath itself - how a folder shown as one game (its single
// launch target) gets the folder's own artwork.
func sourceContainerArtworkFallbackNames(gamePath, systemRootPath string) []string {
	relative := sourceRelativeSegments(gamePath, systemRootPath)
	if len(relative) < 2 {
		return nil
	}
	rootID, rootSegments, err := platforms.SourceLocation(systemRootPath)
	if err != nil {
		return nil
	}
	parentSegments := append(append([]string{}, rootSegments...), relative[:len(relative)-1]...)
	parentPath, err := sourceSegmentsPath(rootID, parentSegments)
	if err != nil {
		return nil
	}
	return sourceDirectoryArtworkFallbackNames(parentPath, systemRootPath)
}

// sourceDirectoryArtworkFallbackNames is
// esmedia.DirectoryArtworkFallbackNames's source-root equivalent: candidate
// artwork names for directoryPath's own name (a folder cover), nested before
// flat, including a disc-folder-extension-stripped stem the same way the
// filesystem version does.
func sourceDirectoryArtworkFallbackNames(directoryPath, systemRootPath string) []string {
	relative := sourceRelativeSegments(directoryPath, systemRootPath)
	if len(relative) == 0 {
		return nil
	}
	base := relative[len(relative)-1]
	stems := []string{base}
	if ext := filepath.Ext(base); esmedia.IsDiscFolderExt(ext) {
		if trimmed := strings.TrimSuffix(base, ext); trimmed != "" {
			stems = append(stems, trimmed)
		}
	}

	parentSegments := relative[:len(relative)-1]
	names := make([]string, 0, len(stems)*len(esmedia.ArtworkExtensions)*2)
	for _, stem := range stems {
		flatNames := esmedia.FallbackArtworkNames(stem)
		if len(parentSegments) == 0 {
			names = append(names, flatNames...)
			continue
		}
		parentPrefix := strings.Join(parentSegments, "/")
		for _, flat := range flatNames {
			names = append(names, parentPrefix+"/"+flat)
		}
		names = append(names, flatNames...)
	}
	return names
}

// sourceAncestorDirectories returns the source paths of every directory
// between gamePath and systemRootPath (exclusive of the root itself), the
// source-root equivalent of directoryCollector's filesystem walk: the
// directories folder artwork can describe.
func sourceAncestorDirectories(gamePath, systemRootPath string) []string {
	rootID, rootSegments, rootErr := platforms.SourceLocation(systemRootPath)
	if rootErr != nil {
		return nil
	}
	relative := sourceRelativeSegments(gamePath, systemRootPath)
	if len(relative) < 2 {
		// Zero segments below the root is not a file at all; one segment is
		// a file directly in the root, with no directory between it and the
		// root to collect.
		return nil
	}
	dirs := make([]string, 0, len(relative)-1)
	for depth := 1; depth < len(relative); depth++ {
		segments := append(append([]string{}, rootSegments...), relative[:depth]...)
		path, err := sourceSegmentsPath(rootID, segments)
		if err != nil {
			continue
		}
		dirs = append(dirs, path)
	}
	return dirs
}

// statSourceMediaDirs is esmedia.StatMediaDirsFS's source-root equivalent:
// lists rootPath's media/ subdirectories through the host instead of a real
// filesystem, returning the same name-to-path shape.
func statSourceMediaDirs(ctx context.Context, listings *sourceDirListings, rootPath string) map[string]string {
	id, rootSegments, err := platforms.SourceLocation(rootPath)
	if err != nil {
		return nil
	}
	mediaSegments := append(append([]string{}, rootSegments...), "media")
	mediaPath, err := sourceSegmentsPath(id, mediaSegments)
	if err != nil {
		return nil
	}

	dirs := make(map[string]string)
	for _, entry := range listings.list(ctx, mediaPath) {
		if !entry.Dir {
			continue
		}
		childPath, childErr := sourceSegmentsPath(id, append(append([]string{}, mediaSegments...), entry.Name))
		if childErr != nil {
			continue
		}
		dirs[entry.Name] = childPath
	}
	return dirs
}

// findSourceFile is esmedia.FindFileFS's source-root equivalent: dirPath
// values in availableDirs are canonical source paths built by
// statSourceMediaDirs, and name may itself contain "/" for a game in a ROM
// subfolder (the artwork mirrors that subfolder structure) - existence is
// checked against the cached listing, never a real filesystem touch.
func findSourceFile(
	ctx context.Context,
	listings *sourceDirListings,
	fallbackNames, candidates []string,
	availableDirs map[string]string,
) *esmedia.File {
	if len(fallbackNames) == 0 {
		return nil
	}
	for _, dir := range candidates {
		dirPath, ok := availableDirs[dir]
		if !ok {
			continue
		}
		id, dirSegments, err := platforms.SourceLocation(dirPath)
		if err != nil {
			continue
		}
		for _, name := range fallbackNames {
			nameSegments := strings.Split(name, "/")
			if !validNameSegments(nameSegments) {
				continue
			}
			parentSegments := append(append([]string{}, dirSegments...), nameSegments[:len(nameSegments)-1]...)
			leaf := nameSegments[len(nameSegments)-1]
			parentPath, parentErr := sourceSegmentsPath(id, parentSegments)
			if parentErr != nil {
				continue
			}
			if !listings.hasFile(ctx, parentPath, leaf) {
				continue
			}
			filePath, fileErr := sourceSegmentsPath(id, append(parentSegments, leaf))
			if fileErr != nil {
				continue
			}
			return &esmedia.File{Path: filePath, ContentType: esmedia.MimeFromExt(filePath)}
		}
	}
	return nil
}

func validNameSegments(segments []string) bool {
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// isSourceSegmentsPropForNames mirrors isLocalMediaPropForNames's path
// reconstruction for a source:// property: the candidate path is built with
// sourceSegmentsPath, never filepath.Join, which mangles "://".
func isSourceSegmentsPropForNames(propPath, root, dir, name string) bool {
	id, rootSegments, err := platforms.SourceLocation(root)
	if err != nil {
		return false
	}
	nameSegments := strings.Split(name, "/")
	if !validNameSegments(nameSegments) {
		return false
	}
	segments := append(append(append([]string{}, rootSegments...), "media", dir), nameSegments...)
	candidate, err := sourceSegmentsPath(id, segments)
	if err != nil {
		return false
	}
	return propPath == candidate
}
