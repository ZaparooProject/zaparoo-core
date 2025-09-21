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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
			// Skip filesystem scanning for launchers that don't need it
			if launchers[i].SkipFilesystemScan {
				continue
			}
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

	// 1. Determine resume state
	indexingStatus, getStatusErr := db.GetIndexingStatus()
	if getStatusErr != nil {
		log.Warn().Err(getStatusErr).Msg("failed to get indexing status, assuming fresh start")
		indexingStatus = "" // Treat as fresh start if status cannot be retrieved
	}

	lastIndexedSystemID := ""
	shouldResume := false

	switch indexingStatus {
	case mediadb.IndexingStatusRunning, mediadb.IndexingStatusPending:
		var getSystemErr error
		lastIndexedSystemID, getSystemErr = db.GetLastIndexedSystem()
		if getSystemErr != nil {
			log.Warn().Err(getSystemErr).Msg("failed to get last indexed system, assuming fresh start")
			// Fall through to fresh index
		} else if lastIndexedSystemID != "" {
			log.Info().Msgf("Previous indexing interrupted. Attempting to resume from system: %s", lastIndexedSystemID)
			shouldResume = true
		}
	case mediadb.IndexingStatusFailed:
		log.Info().Msg("Previous indexing run failed, starting fresh index.")
		// Explicitly clear status for a fresh start after a failure
		if setErr := db.SetLastIndexedSystem(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear last indexed system after failed run")
		}
		if setErr := db.SetIndexingStatus(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear indexing status after failed run")
		}
	}

	// Get the ordered list of systems for this run (deterministic by ID)
	systemPaths := make(map[string][]string)
	for _, v := range GetSystemPaths(cfg, platform, platform.RootDirs(cfg), systems) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}
	sysPathIDs := helpers.AlphaMapKeys(systemPaths)

	// Validate resume point against current system list
	if shouldResume && lastIndexedSystemID != "" {
		foundLastIndexed := false
		// Check against the provided systems list, not just sysPathIDs (which depends on paths)
		for _, system := range systems {
			if system.ID == lastIndexedSystemID {
				foundLastIndexed = true
				break
			}
		}
		if !foundLastIndexed {
			log.Warn().Msgf("Last indexed system '%s' not found in current system list. Reverting to full re-index.",
				lastIndexedSystemID)
			shouldResume = false // Cannot resume reliably, force full re-index
			// Clear state for a fresh start
			if setErr := db.SetLastIndexedSystem(""); setErr != nil {
				log.Error().Err(setErr).Msg("failed to clear last indexed system after unresumable state")
			}
			if setErr := db.SetIndexingStatus(""); setErr != nil {
				log.Error().Err(setErr).Msg("failed to clear indexing status after unresumable state")
			}
		}
	}

	// Initialize scan state
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

	// 2. Conditional Truncate and Initial Status Set
	if !shouldResume {
		err = db.Truncate()
		if err != nil {
			return 0, fmt.Errorf("failed to truncate database: %w", err)
		}
		if setErr := db.SetIndexingStatus(mediadb.IndexingStatusRunning); setErr != nil {
			log.Error().Err(setErr).Msg("failed to set indexing status to running on fresh start")
		}
		if setErr := db.SetLastIndexedSystem(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear last indexed system on fresh start")
		}

		// Seed known tags only on fresh start (after truncate)
		err = SeedKnownTags(db, &scanState)
		if err != nil {
			return 0, fmt.Errorf("failed to seed known tags: %w", err)
		}
	} else {
		// If resuming, ensure status is "running" and populate existing scan state indexes
		if setErr := db.SetIndexingStatus(mediadb.IndexingStatusRunning); setErr != nil {
			log.Error().Err(setErr).Msg("failed to set indexing status to running during resume")
		}

		// When resuming, we need to populate the scan state with existing data
		// to avoid ID conflicts and continue from where we left off
		err = PopulateScanStateFromDB(db, &scanState)
		if err != nil {
			log.Error().Err(err).Msg("failed to populate scan state from database, continuing with fresh state")
		}
	}

	err = db.BeginTransaction()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure transaction rollback and status update on any error
	defer func() {
		if err != nil {
			if rbErr := db.RollbackTransaction(); rbErr != nil {
				log.Error().Err(rbErr).Msg("failed to rollback transaction after error")
			}
			// Mark indexing as failed on error
			if setErr := db.SetIndexingStatus(mediadb.IndexingStatusFailed); setErr != nil {
				log.Error().Err(setErr).Msg("failed to set indexing status to failed after error")
			}
		}
	}()

	status := IndexStatus{
		Total: len(sysPathIDs) + 2, // Adjusted total steps for the current list
		Step:  1,
	}

	// Track which launchers have already been scanned to prevent double-execution
	scannedLaunchers := make(map[string]bool)
	// This map tracks systems that have been fully processed and committed
	completedSystems := make(map[string]bool)

	// Populate completedSystems if resuming
	if shouldResume {
		for _, k := range sysPathIDs {
			if k == lastIndexedSystemID {
				// DO NOT mark the last indexed system as completed - we need to resume from it
				// Only mark systems BEFORE the last indexed system as completed
				break
			}
			completedSystems[k] = true // Mark all systems before the resume point as completed
		}
	}

	update(status)

	scannedSystems := make(map[string]bool)
	for _, s := range systemdefs.AllSystems() {
		scannedSystems[s.ID] = false
	}

	// Main loop for systems
	for _, k := range sysPathIDs {
		systemID := k

		if completedSystems[systemID] {
			log.Debug().Msgf("Skipping already indexed system: %s", systemID)
			status.Step++
			update(status)
			continue // Skip this system if it was already completed in a previous run
		}

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
				// Mark launcher as scanned to prevent double-execution
				scannedLaunchers[l.ID] = true
			}
		}

		if len(files) == 0 {
			log.Debug().Msgf("no files found for system: %s", systemID)
		} else {
			status.Files += len(files)
			log.Debug().Msgf("scanned %d files for system: %s", len(files), systemID)
		}

		scannedSystems[systemID] = true

		for _, p := range files {
			AddMediaPath(db, &scanState, systemID, p.Path)
		}

		// Commit in batches to reduce lock time and allow API operations
		if commitErr := db.CommitTransaction(); commitErr != nil {
			// If commit fails, the defer will handle rollback and status=failed
			err = fmt.Errorf("failed to commit batch transaction for system %s: %w", systemID, commitErr)
			return // Exit early
		}

		// Mark system as completed and update last indexed system after successful commit
		completedSystems[systemID] = true
		if setErr := db.SetLastIndexedSystem(systemID); setErr != nil {
			log.Error().Err(setErr).Msgf("failed to set last indexed system to %s", systemID)
			// This is a non-fatal error for indexing, but important to log.
		}

		FlushScanStateMaps(&scanState)
		if beginErr := db.BeginTransaction(); beginErr != nil {
			err = fmt.Errorf("failed to begin new transaction after system %s commit: %w", systemID, beginErr)
			return // Exit early
		}
	}

	// run each custom scanner at least once, even if there are no paths
	// defined or results from a regular index
	launchers := helpers.GlobalLauncherCache.GetAllLaunchers()
	log.Debug().Msgf("checking %d launchers for custom scanners", len(launchers))
	for i := range launchers {
		l := &launchers[i]
		systemID := l.SystemID
		log.Debug().Msgf("launcher %s for system %s: scanner=%v scanned=%v",
			l.ID, systemID, l.Scanner != nil, scannedLaunchers[l.ID])
		if !scannedLaunchers[l.ID] && l.Scanner != nil && systemID != "" {
			log.Debug().Msgf("running %s scanner for system: %s", l.ID, systemID)
			results, scanErr := l.Scanner(cfg, systemID, []platforms.ScanResult{})
			if scanErr != nil {
				log.Error().Err(scanErr).Msgf("error running %s scanner for system: %s", l.ID, systemID)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), systemID)

			if len(results) > 0 {
				status.Files += len(results)
				for _, p := range results {
					AddMediaPath(db, &scanState, systemID, p.Path)
				}
				// Commit in batches to reduce lock time and allow API operations
				if commitErr := db.CommitTransaction(); commitErr != nil {
					err = fmt.Errorf("failed to commit batch transaction for custom scanner for system %s: %w",
						systemID, commitErr)
					return
				}
				FlushScanStateMaps(&scanState)
				if beginErr := db.BeginTransaction(); beginErr != nil {
					err = fmt.Errorf("failed to begin new transaction after custom scanner commit for system %s: %w",
						systemID, beginErr)
					return
				}
			}
			scannedSystems[systemID] = true
			scannedLaunchers[l.ID] = true // Mark this specific launcher as run
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
			systemID := s.ID
			if completedSystems[systemID] {
				log.Debug().Msgf("Skipping 'any' scanner for already completed system: %s", systemID)
				continue // Skip if system already fully processed
			}

			log.Debug().Msgf("running %s 'any' scanner for system: %s", l.ID, s.ID)
			results, scanErr := l.Scanner(cfg, s.ID, []platforms.ScanResult{})
			if scanErr != nil {
				log.Error().Err(scanErr).Msgf("error running %s 'any' scanner for system: %s", l.ID, s.ID)
				continue
			}

			log.Debug().Msgf("scanned %d files for system: %s", len(results), s.ID)

			if len(results) > 0 {
				status.Files += len(results)
				for _, p := range results {
					AddMediaPath(db, &scanState, systemID, p.Path)
				}

				// Commit in batches to reduce lock time and allow API operations
				if commitErr := db.CommitTransaction(); commitErr != nil {
					err = fmt.Errorf("failed to commit batch transaction for 'any' scanner for system %s: %w",
						systemID, commitErr)
					return
				}
				FlushScanStateMaps(&scanState)
				if beginErr := db.BeginTransaction(); beginErr != nil {
					err = fmt.Errorf("failed to begin new transaction after 'any' scanner commit for system %s: %w",
						systemID, beginErr)
					return
				}
			}
			scannedSystems[systemID] = true
		}
	}

	status.Step++
	status.SystemID = ""
	update(status)

	scanState.TagIDs = make(map[string]int)

	// Phase 1: Complete data operations (foreground) - commit all data
	err = db.CommitTransaction()
	if err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Mark database as complete and ready for use
	err = db.UpdateLastGenerated()
	if err != nil {
		return 0, fmt.Errorf("failed to update last generated timestamp: %w", err)
	}

	// Mark indexing as completed and clear last indexed system
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCompleted); setErr != nil {
		log.Error().Err(setErr).Msg("failed to set indexing status to completed")
	}
	if setErr := db.SetLastIndexedSystem(""); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear last indexed system on completion")
	}

	// Mark optimization as pending
	err = db.SetOptimizationStatus("pending")
	if err != nil {
		log.Error().Err(err).Msg("failed to set optimization status to pending")
	}

	// Phase 2: Start background optimization (indexes, analyze, vacuum)
	go db.RunBackgroundOptimization()

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("processed systems: %v", completedSystems)
	for k, v := range completedSystems {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	indexedFiles = status.Files
	return
}
