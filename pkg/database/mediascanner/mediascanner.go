// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

type PathResult struct {
	Path   string
	System systemdefs.System
}

// FindPath case-insensitively finds a file/folder at a path.
func FindPath(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)

	files, err := os.ReadDir(parent)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %s: %w", parent, err)
	}

	for _, file := range files {
		target := file.Name()

		if len(target) != len(name) {
			continue
		} else if strings.EqualFold(target, name) {
			return filepath.Join(parent, target), nil
		}
	}

	return "", fmt.Errorf("file match not found: %s", path)
}

func GetSystemPaths(
	_ *config.Instance,
	_ platforms.Platform,
	rootFolders []string,
	systems []systemdefs.System,
) []PathResult {
	var matches []PathResult

	for _, system := range systems {
		launchers := helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)

		var folders []string
		for i := range launchers {
			for _, folder := range launchers[i].Folders {
				if !helpers.Contains(folders, folder) {
					folders = append(folders, folder)
				}
			}
		}

		// check for <root>/<folder>
		for _, folder := range rootFolders {
			gf, err := FindPath(folder)
			if err != nil {
				continue
			}

			for _, folder := range folders {
				systemFolder := filepath.Join(gf, folder)
				path, err := FindPath(systemFolder)
				if err != nil {
					continue
				}

				matches = append(matches, PathResult{
					System: system,
					Path:   path,
				})
			}
		}

		// check for absolute paths
		for _, folder := range folders {
			if filepath.IsAbs(folder) {
				systemFolder := folder
				path, err := FindPath(systemFolder)
				if err != nil {
					continue
				}
				matches = append(matches, PathResult{
					System: system,
					Path:   path,
				})
			}
		}
	}

	return matches
}

type resultsStack struct {
	results [][]string
}

func newResultsStack() *resultsStack {
	return &resultsStack{
		results: make([][]string, 0),
	}
}

func (r *resultsStack) push() {
	r.results = append(r.results, make([]string, 0))
}

func (r *resultsStack) pop() {
	if len(r.results) == 0 {
		return
	}
	r.results = r.results[:len(r.results)-1]
}

func (r *resultsStack) get() (*[]string, error) {
	if len(r.results) == 0 {
		return nil, errors.New("nothing on stack")
	}
	return &r.results[len(r.results)-1], nil
}

// GetFiles searches for all valid games in a given path and returns a list of
// files. This function deep searches .zip files and handles symlinks at all
// levels.
func GetFiles(
	cfg *config.Instance,
	platform platforms.Platform,
	systemID string,
	path string,
) ([]string, error) {
	var allResults []string
	stack := newResultsStack()
	visited := make(map[string]struct{})

	system, err := systemdefs.GetSystem(systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get system %s: %w", systemID, err)
	}

	var scanner func(path string, file fs.DirEntry, err error) error
	scanner = func(path string, file fs.DirEntry, _ error) error {
		// avoid recursive symlinks
		if file.IsDir() {
			key := path
			if file.Type()&os.ModeSymlink != 0 {
				realPath, symlinkErr := filepath.EvalSymlinks(path)
				if symlinkErr != nil {
					return fmt.Errorf("failed to evaluate symlink %s: %w", path, symlinkErr)
				}
				key = realPath
			}
			if _, seen := visited[key]; seen {
				return filepath.SkipDir
			}
			visited[key] = struct{}{}
		}

		// handle symlinked directories
		if file.Type()&os.ModeSymlink != 0 {
			absSym, absErr := filepath.Abs(path)
			if absErr != nil {
				return fmt.Errorf("failed to get absolute path for %s: %w", path, absErr)
			}

			realPath, realPathErr := filepath.EvalSymlinks(absSym)
			if realPathErr != nil {
				return fmt.Errorf("failed to evaluate symlink %s: %w", absSym, realPathErr)
			}

			file, statErr := os.Stat(realPath)
			if statErr != nil {
				return fmt.Errorf("failed to stat symlink target %s: %w", realPath, statErr)
			}

			if file.IsDir() {
				stack.push()
				defer stack.pop()

				walkErr := filepath.WalkDir(realPath, scanner)
				if walkErr != nil {
					return fmt.Errorf("failed to walk directory %s: %w", realPath, walkErr)
				}

				results, stackErr := stack.get()
				if stackErr != nil {
					return stackErr
				}

				for i := range *results {
					allResults = append(allResults, strings.Replace((*results)[i], realPath, path, 1))
				}

				return nil
			}
		}

		results, stackErr := stack.get()
		if stackErr != nil {
			return stackErr
		}

		if helpers.IsZip(path) && platform.Settings().ZipsAsDirs {
			// zip files
			zipFiles, zipErr := helpers.ListZip(path)
			if zipErr != nil {
				// skip invalid zip files
				log.Warn().Err(zipErr).Msgf("error listing zip: %s", path)
				return nil
			}

			for i := range zipFiles {
				abs := filepath.Join(path, zipFiles[i])
				if helpers.MatchSystemFile(cfg, platform, system.ID, abs) {
					*results = append(*results, abs)
				}
			}
		} else if helpers.MatchSystemFile(cfg, platform, system.ID, path) {
			// regular files
			*results = append(*results, path)
		}

		return nil
	}

	stack.push()
	defer stack.pop()

	root, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", path, err)
	}

	// handle symlinks on the root game folder because WalkDir fails silently on them
	var realPath string
	if root.Mode()&os.ModeSymlink == 0 {
		realPath = path
	} else {
		realPath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate symlink %s: %w", path, err)
		}
	}

	realRoot, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat real path %s: %w", realPath, err)
	}

	if !realRoot.IsDir() {
		return nil, errors.New("root is not a directory")
	}

	err = filepath.WalkDir(realPath, scanner)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %s: %w", realPath, err)
	}

	results, err := stack.get()
	if err != nil {
		return nil, err
	}

	allResults = append(allResults, *results...)

	// change root back to symlink
	if realPath != path {
		for i := range allResults {
			allResults[i] = strings.Replace(allResults[i], realPath, path, 1)
		}
	}

	return allResults, nil
}

type IndexStatus struct {
	SystemID string
	Total    int
	Step     int
	Files    int
}

// NewNamesIndex takes a list of systems, indexes all valid game files on disk
// and writes a name index to the DB.
//
// Overwrites any existing names index but does not clean up old missing files.
//
// Takes a function which will be called with the current status of the index
// during key steps.
//
// Returns the total number of files indexed.
func NewNamesIndex(
	platform platforms.Platform,
	cfg *config.Instance,
	systems []systemdefs.System,
	fdb *database.Database,
	update func(IndexStatus),
) (indexedFiles int, err error) {
	db := fdb.MediaDB

	err = db.Truncate()
	if err != nil {
		return 0, fmt.Errorf("failed to truncate database: %w", err)
	}
	err = db.BeginTransaction()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure transaction rollback on any error
	defer func() {
		if err != nil {
			if rbErr := db.RollbackTransaction(); rbErr != nil {
				log.Error().Err(rbErr).Msg("failed to rollback transaction after error")
			}
		}
	}()

	status := IndexStatus{
		Total: len(systems) + 2, // estimate steps
		Step:  1,
	}

	scanState := database.ScanState{
		SystemsIndex:   0,
		SystemIDs:      make(map[string]int),
		TitlesIndex:    0,
		TitleIDs:       make(map[string]int),
		MediaIndex:     0,
		MediaIDs:       make(map[string]int),
		TagTypesIndex:  0,
		TagTypeIDs:     make(map[string]int),
		TagsIndex:      0,
		TagIDs:         make(map[string]int),
		MediaTagsIndex: 0,
	}
	SeedKnownTags(db, &scanState)

	update(status)
	systemPaths := make(map[string][]string)
	for _, v := range GetSystemPaths(cfg, platform, platform.RootDirs(cfg), systems) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}

	scanned := make(map[string]bool)
	for _, s := range systemdefs.AllSystems() {
		scanned[s.ID] = false
	}

	sysPathIDs := helpers.AlphaMapKeys(systemPaths)
	// update steps with true count
	status.Total = len(sysPathIDs) + 2

	for _, k := range sysPathIDs {
		systemID := k
		files := make([]platforms.ScanResult, 0)

		status.SystemID = systemID
		status.Step++
		update(status)

		// scan using standard folder and extensions
		for _, path := range systemPaths[k] {
			pathFiles, pathErr := GetFiles(cfg, platform, k, path)
			if pathErr != nil {
				log.Error().Err(pathErr).Msgf("error getting files for system: %s", systemID)
				continue
			}
			for _, f := range pathFiles {
				files = append(files, platforms.ScanResult{Path: f})
			}
		}

		// for each system launcher in a platform, run the results through its
		// custom scan function if one exists
		launchers := helpers.GlobalLauncherCache.GetLaunchersBySystem(k)
		for i := range launchers {
			l := &launchers[i]
			if l.Scanner != nil {
				log.Debug().Msgf("running %s scanner for system: %s", l.ID, systemID)
				var scanErr error
				files, scanErr = l.Scanner(cfg, systemID, files)
				if scanErr != nil {
					log.Error().Err(scanErr).Msgf("error running %s scanner for system: %s", l.ID, systemID)
					continue
				}
			}
		}

		if len(files) == 0 {
			log.Debug().Msgf("no files found for system: %s", systemID)
			continue
		}

		status.Files += len(files)
		log.Debug().Msgf("scanned %d files for system: %s", len(files), systemID)
		scanned[systemID] = true

		for _, p := range files {
			AddMediaPath(db, &scanState, systemID, p.Path)
		}

		// Commit in batches to reduce lock time and allow API operations
		if commitErr := db.CommitTransaction(); commitErr != nil {
			return 0, fmt.Errorf("failed to commit batch transaction: %w", commitErr)
		}
		FlushScanStateMaps(&scanState)
		if beginErr := db.BeginTransaction(); beginErr != nil {
			return 0, fmt.Errorf("failed to begin new transaction: %w", beginErr)
		}
	}

	// run each custom scanner at least once, even if there are no paths
	// defined or results from a regular index
	scannedLaunchers := make(map[string]bool)
	launchers := helpers.GlobalLauncherCache.GetAllLaunchers()
	log.Debug().Msgf("checking %d launchers for custom scanners", len(launchers))
	for i := range launchers {
		l := &launchers[i]
		systemID := l.SystemID
		log.Debug().Msgf("launcher %s for system %s: scanner=%v scanned=%v", l.ID, systemID, l.Scanner != nil, scannedLaunchers[l.ID])
		if !scannedLaunchers[l.ID] && l.Scanner != nil {
			log.Debug().Msgf("running %s scanner for system: %s", l.ID, systemID)
			results, scanErr := l.Scanner(cfg, systemID, []platforms.ScanResult{})
			if scanErr != nil {
				log.Error().Err(scanErr).Msgf("error running %s scanner for system: %s", l.ID, systemID)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), systemID)

			status.Files += len(results)
			scanned[systemID] = true
			scannedLaunchers[l.ID] = true

			if len(results) > 0 {
				for _, p := range results {
					AddMediaPath(db, &scanState, systemID, p.Path)
				}

				// Commit in batches to reduce lock time and allow API operations
				if commitErr := db.CommitTransaction(); commitErr != nil {
					return 0, fmt.Errorf("failed to commit batch transaction: %w", commitErr)
				}
				FlushScanStateMaps(&scanState)
				if beginErr := db.BeginTransaction(); beginErr != nil {
					return 0, fmt.Errorf("failed to begin new transaction: %w", beginErr)
				}
			}
		}
	}

	// launcher scanners with no system defined are run against every system
	var anyScanners []platforms.Launcher
	allLaunchers := helpers.GlobalLauncherCache.GetAllLaunchers()
	for i := range allLaunchers {
		if allLaunchers[i].SystemID == "" && allLaunchers[i].Scanner != nil {
			anyScanners = append(anyScanners, allLaunchers[i])
		}
	}

	for i := range anyScanners {
		l := &anyScanners[i]
		for _, s := range systems {
			log.Debug().Msgf("running %s scanner for system: %s", l.ID, s.ID)
			results, scanErr := l.Scanner(cfg, s.ID, []platforms.ScanResult{})
			if scanErr != nil {
				log.Error().Err(scanErr).Msgf("error running %s scanner for system: %s", l.ID, s.ID)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), s.ID)

			if len(results) > 0 {
				status.Files += len(results)
				scanned[s.ID] = true
				systemID := s.ID

				for _, p := range results {
					AddMediaPath(db, &scanState, systemID, p.Path)
				}

				// Commit in batches to reduce lock time and allow API operations
				if commitErr := db.CommitTransaction(); commitErr != nil {
					return 0, fmt.Errorf("failed to commit batch transaction: %w", commitErr)
				}
				FlushScanStateMaps(&scanState)
				if beginErr := db.BeginTransaction(); beginErr != nil {
					return 0, fmt.Errorf("failed to begin new transaction: %w", beginErr)
				}
			}
		}
	}

	status.Step++
	status.SystemID = ""
	update(status)

	scanState.TagIDs = make(map[string]int)
	err = db.ReindexTables()
	if err != nil {
		return 0, fmt.Errorf("failed to reindex tables: %w", err)
	}
	err = db.CommitTransaction()
	if err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	err = db.Vacuum()
	if err != nil {
		return 0, fmt.Errorf("failed to vacuum database: %w", err)
	}
	err = db.UpdateLastGenerated()
	if err != nil {
		return 0, fmt.Errorf("failed to update last generated timestamp: %w", err)
	}

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("scanned systems: %v", scanned)
	for k, v := range scanned {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	indexedFiles = status.Files
	return
}
