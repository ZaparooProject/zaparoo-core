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

package mediascanner

// Source roots are media folders a host application granted to Core, which
// the operating system cannot open. They are indexed like RootDirs: the same
// launcher folders are looked for in them, and the same walk rules apply to
// the system folders found. Only reading goes through the platform.
//
// One rule differs, because a host can fail where a local disk would not: a
// failure to read a source root fails the run or marks the system incomplete,
// so media is never marked missing because the host did not answer. A root
// the host no longer grants is simply not listed, and its media goes missing
// as it would from an unmounted card.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// maxSourceWalkDepth bounds how far below a system folder a source walk goes,
// matching the limit on multi-segment virtual paths.
const maxSourceWalkDepth = 64

// sourcePath builds the canonical path of segments below the source root id.
func sourcePath(id string, segments []string) (string, error) {
	if len(segments) == 0 {
		return platforms.SourceScheme + "://" + id, nil
	}
	path, err := virtualpath.CreateVirtualPathSegments(platforms.SourceScheme, id, segments)
	if err != nil {
		return "", fmt.Errorf("build source path: %w", err)
	}
	return path, nil
}

// sourceListings memoizes directory listings for one discovery pass, as
// pathResolver does for RootDirs.
type sourceListings struct {
	reader platforms.SourceRootReader
	dirs   map[string][]platforms.SourceEntry
}

func (l *sourceListings) read(ctx context.Context, path string) ([]platforms.SourceEntry, error) {
	if entries, ok := l.dirs[path]; ok {
		return entries, nil
	}
	entries, err := l.reader.ReadSourceDir(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read source directory %s: %w", path, err)
	}
	l.dirs[path] = entries
	return entries, nil
}

// findSourceFolder resolves a launcher folder below a source root the way
// FindPath resolves one below a root directory: component by component, an
// exact name first, then a case-insensitive one. found is false when the
// folder is not there.
func findSourceFolder(
	ctx context.Context,
	listings *sourceListings,
	id, folder string,
) (path string, found bool, err error) {
	var segments []string
	for _, part := range strings.Split(filepath.ToSlash(folder), "/") {
		if part == "" || part == "." {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", false, fmt.Errorf("resolve source folder: %w", ctxErr)
		}
		dir, dirErr := sourcePath(id, segments)
		if dirErr != nil {
			return "", false, dirErr
		}
		entries, readErr := listings.read(ctx, dir)
		if readErr != nil {
			return "", false, readErr
		}
		match := ""
		for i := range entries {
			if entries[i].Dir && entries[i].Name == part {
				match = entries[i].Name
				break
			}
		}
		if match == "" {
			for i := range entries {
				if entries[i].Dir && strings.EqualFold(entries[i].Name, part) {
					match = entries[i].Name
					break
				}
			}
		}
		if match == "" {
			return "", false, nil
		}
		segments = append(segments, match)
	}
	if len(segments) == 0 {
		return "", false, nil
	}
	path, err = sourcePath(id, segments)
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}

// relativeScanFolders is every distinct non-absolute, non-SkipFilesystemScan
// launcher folder for one system. Absolute launcher folders are filesystem
// paths and never apply to a source root.
func relativeScanFolders(systemID string, launcherCache *helpers.LauncherCache) []string {
	launchers := launcherCache.GetLaunchersBySystem(systemID)
	var folders []string
	for i := range launchers {
		if launchers[i].SkipFilesystemScan {
			continue
		}
		for _, folder := range launchers[i].Folders {
			if !filepath.IsAbs(folder) && !helpers.Contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}
	return folders
}

// sourceRootSystems returns the systems a source root could hold media for:
// every system with at least one relative, scannable launcher folder. It is
// used to decide which systems a failed root's discovery might have affected,
// since a root that could not be read says nothing about what it would have
// contained.
func sourceRootSystems(systems []systemdefs.System, launcherCache *helpers.LauncherCache) map[string]bool {
	affected := make(map[string]bool, len(systems))
	for _, system := range systems {
		if len(relativeScanFolders(system.ID, launcherCache)) > 0 {
			affected[system.ID] = true
		}
	}
	return affected
}

// getSourceSystemPaths finds each system's launcher folders in the source
// roots, as getSystemPathsForLauncherCache does in RootDirs.
//
// A root that cannot be read (a flaky provider, a transient host error) does
// not abort discovery for every other root and system: it is logged, recorded
// in failedRoots, and skipped, the same way one bad RootDirs path already
// does not fail an entire scan. A root the host no longer lists at all is
// simply absent from roots and never reaches this function, per this file's
// own two-outcome design: "fails the run or marks the system incomplete".
// Only ctx.Err() still fails the whole call, since nothing further can be
// discovered once the caller has stopped waiting.
func getSourceSystemPaths(
	ctx context.Context,
	reader platforms.SourceRootReader,
	roots []string,
	systems []systemdefs.System,
	launcherCache *helpers.LauncherCache,
) (matches []PathResult, failedRoots []string, err error) {
	listings := &sourceListings{reader: reader, dirs: make(map[string][]platforms.SourceEntry)}
	seen := make(map[string]bool)
	for _, root := range roots {
		id, segments, locErr := platforms.SourceLocation(root)
		if locErr != nil || len(segments) > 0 {
			log.Warn().Str("root", root).Msg("skipping malformed source root")
			continue
		}
		// Collected separately from matches: a root that fails partway through
		// has its partial results discarded rather than committed, since a
		// listing failure partway through a root says nothing reliable about
		// the rest of it either.
		var rootMatches []PathResult
		rootFailed := false
		for _, system := range systems {
			folders := relativeScanFolders(system.ID, launcherCache)
			for _, folder := range folders {
				path, found, findErr := findSourceFolder(ctx, listings, id, folder)
				if findErr != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return nil, nil, ctxErr
					}
					log.Warn().Err(findErr).Str("root", id).
						Msg("skipping source root: discovery failed reading it")
					rootFailed = true
					break
				}
				if !found {
					continue
				}
				key := system.ID + ":" + path
				if seen[key] {
					continue
				}
				rootMatches = append(rootMatches, PathResult{System: system, Path: path})
			}
			if rootFailed {
				break
			}
		}
		if rootFailed {
			failedRoots = append(failedRoots, root)
			continue
		}
		for _, match := range rootMatches {
			seen[match.System.ID+":"+match.Path] = true
		}
		matches = append(matches, rootMatches...)
	}
	log.Info().Int("roots", len(roots)).Int("failedRoots", len(failedRoots)).
		Int("matches", len(matches)).Msg("source root discovery complete")
	return matches, failedRoots, nil
}

// getSourceFiles walks one system folder in a source root and returns the
// media paths in it, with the rules GetFiles applies to a root directory:
// hidden and __MACOSX directories below the folder are skipped, a
// .zaparooignore marker skips its directory, AppleDouble files are ignored,
// and launcher scan excludes and file matching decide the rest. ZIP files are
// indexed as files; their contents are not listed, since Core cannot open a
// file in a source root.
func getSourceFiles(
	ctx context.Context,
	cfg *config.Instance,
	platform platforms.Platform,
	reader platforms.SourceRootReader,
	systemID string,
	path string,
	pauser *syncutil.Pauser,
) ([]string, error) {
	id, base, err := platforms.SourceLocation(path)
	if err != nil {
		return nil, fmt.Errorf("walk source folder: %w", err)
	}
	matcher := helpers.NewLauncherMatcher(cfg, platform)

	type dir struct {
		segments []string
	}
	pending := []dir{{segments: base}}
	var results []string
	entries := 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("source walk cancelled: %w", err)
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		dirPath, err := sourcePath(id, current.segments)
		if err != nil {
			return nil, err
		}
		listing, err := reader.ReadSourceDir(ctx, dirPath)
		if err != nil {
			return nil, fmt.Errorf("read source directory %s: %w", dirPath, err)
		}
		ignored := false
		for i := range listing {
			if !listing[i].Dir && listing[i].Name == ".zaparooignore" {
				ignored = true
				break
			}
		}
		if ignored {
			log.Info().Str("path", dirPath).Msg("skipping directory with .zaparooignore marker")
			continue
		}

		for i := range listing {
			entry := &listing[i]
			entries++
			if entries%walkEntryWaitInterval == 0 {
				if waitErr := pauser.Wait(ctx); waitErr != nil {
					return nil, fmt.Errorf("source walk cancelled while throttled: %w", waitErr)
				}
			}
			if !virtualpath.ValidSegment(entry.Name) {
				log.Debug().Str("dir", dirPath).Msg("skipping source entry with an unusable name")
				continue
			}
			segments := make([]string, len(current.segments), len(current.segments)+1)
			copy(segments, current.segments)
			segments = append(segments, entry.Name)

			if entry.Dir {
				if entry.Name == "__MACOSX" || entry.Name[0] == '.' {
					continue
				}
				if len(segments)-len(base) >= maxSourceWalkDepth {
					log.Warn().Str("dir", dirPath).Msg("source directory too deep, not walking further")
					continue
				}
				childPath, err := sourcePath(id, segments)
				if err != nil {
					log.Warn().Err(err).Str("dir", dirPath).Msg("skipping source directory")
					continue
				}
				if matcher.ShouldSkipScanDirectory(systemID, childPath) {
					log.Info().Str("system", systemID).Str("path", childPath).
						Msg("skipping launcher-excluded scan directory")
					continue
				}
				pending = append(pending, dir{segments: segments})
				continue
			}

			if len(entry.Name) >= 2 && entry.Name[0] == '.' && entry.Name[1] == '_' {
				continue
			}
			filePath, err := sourcePath(id, segments)
			if err != nil {
				log.Warn().Err(err).Str("dir", dirPath).Msg("skipping source file")
				continue
			}
			if matcher.MatchSystemFileForScan(systemID, filePath) {
				results = append(results, filePath)
			}
		}
	}
	log.Debug().Str("system", systemID).Str("path", path).
		Int("entriesScanned", entries).Int("filesFound", len(results)).
		Msg("completed source walk")
	return results, nil
}
