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

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/charlievieth/fastwalk"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// Batch configuration for transaction optimization
const (
	maxFilesPerTransaction   = 10000
	maxSystemsPerTransaction = 10
)

// listNumberingRegex matches file list numbering patterns like "1. ", "01 - ", "42. "
var listNumberingRegex = regexp.MustCompile(`^\d{1,3}[.\s\-]+`)

// detectNumberingPattern returns true if a significant portion of files match list numbering pattern.
// This heuristic detects directory-wide list numbering (e.g., "1. Game.zip", "2. Game.zip")
// to distinguish from legitimate title numbers (e.g., "1942.zip", "007.zip").
//
// Parameters:
//   - files: slice of scan results to analyze
//   - threshold: minimum ratio of matching files (0.0-1.0) to trigger stripping
//   - minFiles: minimum number of files required to apply heuristic
//
// Returns true if >threshold% of files match AND file count >= minFiles.
func detectNumberingPattern(files []platforms.ScanResult, threshold float64, minFiles int) bool {
	if len(files) < minFiles {
		return false // Don't apply heuristic to small file sets
	}

	matchCount := 0
	for _, file := range files {
		filename := filepath.Base(file.Path)
		if listNumberingRegex.MatchString(filename) {
			matchCount++
		}
	}

	return float64(matchCount)/float64(len(files)) > threshold
}

type PathResult struct {
	Path   string
	System systemdefs.System
}

// FindPath case-insensitively finds a file/folder at a path and returns the actual filesystem case.
// On case-insensitive filesystems (Windows, macOS), this ensures we get the real case from the filesystem
// rather than preserving the input case, which prevents case-mismatch issues during string comparisons.
//
// This function recursively normalizes the entire path from root to leaf to ensure all components
// match the actual filesystem case.
//
// Special handling:
//   - Linux: Prefers exact match before case-insensitive match (handles File.txt vs file.txt)
//   - Windows: Handles 8.3 short names (PROGRA~1) via fallback to os.Stat
//   - All platforms: Works with symlinks, UNC paths, network drives
func FindPath(ctx context.Context, path string) (string, error) {
	// Check if path exists first
	if _, err := statWithContext(ctx, path); err != nil {
		return "", fmt.Errorf("path does not exist: %s", path)
	}

	// Get absolute path to ensure we have a complete path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Extract volume (Windows: "C:", UNC: "\\server\share", Unix: "")
	volume := filepath.VolumeName(absPath)

	// Start building result from volume/root
	currentPath := volume
	if currentPath == "" {
		// Unix absolute path - start from root
		if filepath.IsAbs(absPath) {
			currentPath = string(filepath.Separator)
		}
	} else {
		// Windows: ensure volume ends with separator (C: -> C:\)
		// filepath.Join("C:", "Users") -> "C:Users" (relative, wrong!)
		// filepath.Join("C:\", "Users") -> "C:\Users" (absolute, correct!)
		if !strings.HasSuffix(currentPath, string(filepath.Separator)) {
			currentPath += string(filepath.Separator)
		}
	}

	// Get relative path (everything after volume)
	relPath := absPath[len(volume):]

	// Clean leading separators to ensure clean split
	// Handles both "\" from "C:\Users" and "/" from mixed-slash paths
	relPath = strings.TrimLeft(relPath, "/\\")

	// Split into components and normalize each
	parts := strings.Split(relPath, string(filepath.Separator))

	for _, part := range parts {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if part == "" || part == "." {
			continue
		}

		entries, err := readDirWithContext(ctx, currentPath)
		if err != nil {
			return "", fmt.Errorf("failed to read directory %s: %w", currentPath, err)
		}

		found := false

		// On case-sensitive filesystems (Linux), prefer exact match first
		// This prevents File.txt and file.txt ambiguity
		if runtime.GOOS == "linux" {
			for _, entry := range entries {
				if entry.Name() == part {
					currentPath = filepath.Join(currentPath, entry.Name())
					found = true
					break
				}
			}
		}

		// Fallback to case-insensitive match (or first attempt on Windows/macOS)
		if !found {
			for _, entry := range entries {
				if strings.EqualFold(entry.Name(), part) {
					currentPath = filepath.Join(currentPath, entry.Name())
					found = true
					break
				}
			}
		}

		// Handle 8.3 short names on Windows (PROGRA~1)
		// If component not found via ReadDir but path exists via Stat,
		// it might be a short name or special filesystem entry
		if !found {
			targetCheck := filepath.Join(currentPath, part)
			if _, err := statWithContext(ctx, targetCheck); err == nil {
				// Path exists but wasn't found in directory listing
				// Accept the component as-is (likely 8.3 short name)
				currentPath = targetCheck
				found = true
			}
		}

		if !found {
			return "", fmt.Errorf("component %s not found in %s", part, currentPath)
		}
	}

	return currentPath, nil
}

func GetSystemPaths(
	ctx context.Context,
	_ *config.Instance,
	_ platforms.Platform,
	rootFolders []string,
	systems []systemdefs.System,
) []PathResult {
	var matches []PathResult

	log.Info().
		Int("rootFolders", len(rootFolders)).
		Int("systems", len(systems)).
		Msg("starting path discovery")

	// Validate root folders ONCE before iterating systems
	// This prevents logging the same error 200+ times (once per system)
	validRootFolders := make([]string, 0, len(rootFolders))
	for _, folder := range rootFolders {
		gf, err := FindPath(ctx, folder)
		if err != nil {
			switch {
			case errors.Is(err, ErrFsTimeout):
				log.Warn().Str("path", folder).Dur("timeout", defaultFsTimeout).
					Msg("root folder timed out (possible stale mount)")
			case ctx.Err() != nil:
				log.Info().Msg("path discovery cancelled")
				return matches
			default:
				log.Debug().Err(err).Str("path", folder).Msg("skipping root folder - not found or inaccessible")
			}
			continue
		}
		validRootFolders = append(validRootFolders, gf)
	}

	log.Info().
		Int("validRoots", len(validRootFolders)).
		Int("totalRoots", len(rootFolders)).
		Msg("root folder validation complete")

	for _, system := range systems {
		select {
		case <-ctx.Done():
			log.Info().Msg("path discovery cancelled")
			return matches
		default:
		}

		// GlobalLauncherCache is assumed to be read-only after initialization
		launchers := helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)

		var folders []string
		for j := range launchers {
			// Skip filesystem scanning for launchers that don't need it
			if launchers[j].SkipFilesystemScan {
				log.Trace().
					Str("launcher", launchers[j].ID).
					Str("system", system.ID).
					Msg("skipping filesystem scan for launcher")
				continue
			}
			for _, folder := range launchers[j].Folders {
				if !helpers.Contains(folders, folder) {
					folders = append(folders, folder)
				}
			}
		}

		log.Trace().
			Str("system", system.ID).
			Int("launchers", len(launchers)).
			Strs("folders", folders).
			Strs("rootFolders", validRootFolders).
			Msg("resolving system paths")

		for _, gf := range validRootFolders {
			for _, folder := range folders {
				if filepath.IsAbs(folder) {
					continue // handled separately below
				}

				systemFolder := filepath.Join(gf, folder)
				path, err := FindPath(ctx, systemFolder)
				if err != nil {
					if ctx.Err() != nil {
						return matches
					}
					continue
				}

				matches = append(matches, PathResult{
					System: system,
					Path:   path,
				})
			}
		}

		for _, folder := range folders {
			if !filepath.IsAbs(folder) {
				continue
			}

			path, err := FindPath(ctx, folder)
			if err != nil {
				if ctx.Err() != nil {
					return matches
				}
				continue
			}
			matches = append(matches, PathResult{
				System: system,
				Path:   path,
			})
		}
	}

	// Deduplicate by (SystemID, resolved Path) to prevent UNIQUE constraint
	// failures when multiple root folders resolve to the same directory
	seen := make(map[string]bool)
	deduplicated := make([]PathResult, 0, len(matches))
	for _, m := range matches {
		key := m.System.ID + ":" + m.Path
		if !seen[key] {
			seen[key] = true
			deduplicated = append(deduplicated, m)
		}
	}

	log.Info().
		Int("matches", len(deduplicated)).
		Int("duplicatesRemoved", len(matches)-len(deduplicated)).
		Msg("path discovery complete")

	return deduplicated
}

// GetFiles searches for all valid games in a given path and returns a list of
// files. Uses fastwalk for parallel directory traversal with built-in symlink
// cycle detection. Deep searches .zip files when ZipsAsDirs is enabled.
func GetFiles(
	ctx context.Context,
	cfg *config.Instance,
	platform platforms.Platform,
	systemID string,
	path string,
) ([]string, error) {
	system, err := systemdefs.GetSystem(systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get system %s: %w", systemID, err)
	}

	var entriesScanned atomic.Int64
	walkStartTime := time.Now()

	var mu syncutil.Mutex
	var results []string

	conf := &fastwalk.Config{
		Follow: true,
	}

	matcher := helpers.NewLauncherMatcher(cfg, platform)

	log.Debug().Str("system", systemID).Str("path", path).Msg("starting directory walk")
	err = fastwalk.Walk(conf, path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", p).Msg("walk error")
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n := entriesScanned.Add(1)
		if n%5000 == 0 {
			log.Debug().
				Str("system", systemID).
				Str("path", p).
				Int64("entriesScanned", n).
				Dur("elapsed", time.Since(walkStartTime)).
				Msg("directory walk progress")
		}

		if d.IsDir() {
			dirName := filepath.Base(p)
			// Skip macOS metadata directories and hidden directories.
			// __MACOSX contains Apple resource fork files that are not
			// real archives. Dot-prefixed directories are OS metadata
			// that never contain game ROMs. The walk root is exempt so
			// that a user-configured dot-prefixed folder still works.
			if p != path && (dirName == "__MACOSX" || dirName[0] == '.') {
				return filepath.SkipDir
			}

			markerPath := filepath.Join(p, ".zaparooignore")
			if _, statErr := statWithContext(ctx, markerPath); statErr == nil {
				log.Info().Str("path", p).Msg("skipping directory with .zaparooignore marker")
				return filepath.SkipDir
			}
			return nil
		}

		// Skip macOS AppleDouble resource fork files before the zip
		// check — they carry valid-looking extensions (.zip etc.) but
		// are not real archives.
		baseName := filepath.Base(p)
		if len(baseName) >= 2 && baseName[0] == '.' && baseName[1] == '_' {
			return nil
		}

		if helpers.IsZip(p) && platform.Settings().ZipsAsDirs {
			log.Trace().Str("path", p).Msg("opening zip file for indexing")
			zipFiles, zipErr := helpers.ListZip(p)
			if zipErr != nil {
				log.Warn().Err(zipErr).Msgf("error listing zip: %s", p)
				return nil
			}

			mu.Lock()
			for i := range zipFiles {
				abs := filepath.Join(p, zipFiles[i])
				if matcher.MatchSystemFile(system.ID, abs) {
					results = append(results, abs)
				}
			}
			mu.Unlock()
		} else if matcher.MatchSystemFile(system.ID, p) {
			mu.Lock()
			results = append(results, p)
			mu.Unlock()
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %s: %w", path, err)
	}

	scanned := entriesScanned.Load()
	walkElapsed := time.Since(walkStartTime)

	log.Debug().
		Str("system", systemID).
		Str("path", path).
		Int64("entriesScanned", scanned).
		Int("filesFound", len(results)).
		Dur("elapsed", walkElapsed).
		Msg("completed directory walk")

	if scanned > 0 && len(results) == 0 {
		log.Info().
			Str("system", systemID).
			Str("path", path).
			Int64("entriesScanned", scanned).
			Msg("directory walk found entries but no files matched any launcher")
	}

	// Warn when the walk rate is slow rather than when absolute elapsed
	// time is high. A 33K-entry directory legitimately takes ~19s on
	// MiSTer ARM + USB 2.0; only warn when the filesystem is genuinely
	// sluggish (< 500 entries/sec sustained over at least 5 seconds).
	const (
		minSlowWalkElapsed = 5 * time.Second
		minEntriesPerSec   = 500.0
	)
	if walkElapsed > minSlowWalkElapsed && scanned > 0 {
		rate := float64(scanned) / walkElapsed.Seconds()
		if rate < minEntriesPerSec {
			log.Warn().
				Str("system", systemID).
				Str("path", path).
				Int64("entriesScanned", scanned).
				Dur("elapsed", walkElapsed).
				Float64("entriesPerSec", rate).
				Msg("directory walk is slow - possible stale mount or degraded storage")
		}
	}

	return results, nil
}

// handleCancellation performs cleanup when media indexing is cancelled
func handleCancellation(ctx context.Context, db database.MediaDBI, message string) (int, error) {
	log.Info().Msg(message)
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCancelled); setErr != nil {
		log.Error().Err(setErr).Msg("failed to set indexing status to cancelled")
	}
	return 0, ctx.Err()
}

// handleCancellationWithRollback performs cleanup when media indexing is cancelled after transaction begins
func handleCancellationWithRollback(ctx context.Context, db database.MediaDBI, message string) (int, error) {
	log.Info().Msg(message)
	if rbErr := db.RollbackTransaction(); rbErr != nil {
		log.Error().Err(rbErr).Msg("failed to rollback transaction after cancellation")
	}
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCancelled); setErr != nil {
		log.Error().Err(setErr).Msg("failed to set indexing status to cancelled")
	}
	return 0, ctx.Err()
}

const (
	PhaseDiscovering     = "discovering"
	PhaseInitializing    = "initializing"
	PhaseCreatingIndexes = "creating_indexes"
	PhaseBuildingCaches  = "building_caches"
)

type IndexStatus struct {
	SystemID string
	Phase    string // PhaseDiscovering, PhaseInitializing, or empty during indexing
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
	ctx context.Context,
	platform platforms.Platform,
	cfg *config.Instance,
	systems []systemdefs.System,
	fdb *database.Database,
	update func(IndexStatus),
	pauser *syncutil.Pauser,
) (int, error) {
	db := fdb.MediaDB
	indexStartTime := time.Now()

	// Activate the NormalizeTag cache for the duration of this indexing run.
	// The bracket vocabulary is small (~200–400 unique strings), so the cache
	// stabilises early and collapses the repeated regex cost across 100k+ files.
	tags.SetNormalizeTagCache(make(map[string]string))
	defer tags.SetNormalizeTagCache(nil)

	// Temporarily increase SQLite cache to 32MB for bulk indexing
	db.SetIndexingCacheSize(true)
	defer db.SetIndexingCacheSize(false)

	log.Info().
		Int("systemCount", len(systems)).
		Msg("starting media indexing")

	var indexedFiles int
	var err error

	// Create list of system IDs for storage
	currentSystemIDs := make([]string, 0, len(systems))
	systemIDMap := make(map[string]bool, len(systems))
	for _, sys := range systems {
		currentSystemIDs = append(currentSystemIDs, sys.ID)
		systemIDMap[sys.ID] = true
	}

	// 1. Check for database locks or issues before starting
	log.Info().Msg("checking database readiness for indexing")
	// Quick database health check - try to read a simple value
	_, checkErr := db.GetIndexingStatus()
	if checkErr != nil {
		return 0, fmt.Errorf("database not ready for indexing (possible lock or corruption): %w", checkErr)
	}

	// 2. Determine resume state
	log.Info().Msg("determining indexing resume state")
	indexingStatus, getStatusErr := db.GetIndexingStatus()
	if getStatusErr != nil {
		return 0, fmt.Errorf("failed to get indexing status: %w", getStatusErr)
	}

	lastIndexedSystemID := ""
	shouldResume := false

	switch indexingStatus {
	case "":
		log.Info().Msg("starting fresh indexing (no previous indexing status)")
	case mediadb.IndexingStatusRunning, mediadb.IndexingStatusPending:
		log.Warn().Str("status", indexingStatus).
			Msg("previous indexing was interrupted, attempting to resume")
		var getSystemErr error
		lastIndexedSystemID, getSystemErr = db.GetLastIndexedSystem()
		if getSystemErr != nil {
			return 0, fmt.Errorf("failed to get last indexed system during resume: %w", getSystemErr)
		} else if lastIndexedSystemID != "" {
			// Validate that we can resume with the current configuration
			// Always check if we're indexing the same systems
			storedSystems, getStoredErr := db.GetIndexingSystems()

			switch {
			case getStoredErr != nil:
				log.Warn().Err(getStoredErr).Msg("failed to get stored indexing configuration, assuming fresh start")
			case !helpers.EqualStringSlices(storedSystems, currentSystemIDs):
				log.Warn().Msg("system list changed from previous indexing, reverting to fresh index")
			default:
				log.Info().Msgf("previous indexing interrupted. attempting to resume from system: %s",
					lastIndexedSystemID)
				shouldResume = true
			}
		}
	case mediadb.IndexingStatusFailed:
		log.Info().Msg("previous indexing run failed, starting fresh index")
		// Explicitly clear status for a fresh start after a failure
		if setErr := db.SetLastIndexedSystem(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear last indexed system after failed run")
		}
		if setErr := db.SetIndexingStatus(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear indexing status after failed run")
		}
	}

	// Get the ordered list of systems for this run (deterministic by ID)
	update(IndexStatus{Phase: PhaseDiscovering})
	systemPaths := make(map[string][]string)
	for _, v := range GetSystemPaths(ctx, cfg, platform, platform.RootDirs(cfg), systems) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}
	update(IndexStatus{Phase: PhaseInitializing})

	// Check for cancellation or pause
	select {
	case <-ctx.Done():
		return handleCancellation(ctx, db, "Media indexing cancelled during initialization")
	default:
	}
	if waitErr := pauser.Wait(ctx); waitErr != nil {
		return handleCancellation(ctx, db, "Media indexing cancelled while paused during initialization")
	}

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
			log.Warn().Msgf("last indexed system '%s' not found in current system list, reverting to full re-index",
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
		SystemsIndex:  0,
		SystemIDs:     make(map[string]int),
		TitlesIndex:   0,
		TitleIDs:      make(map[string]int),
		MediaIndex:    0,
		MediaIDs:      make(map[string]int),
		TagTypesIndex: 0,
		TagTypeIDs:    make(map[string]int),
		TagsIndex:     0,
		TagIDs:        make(map[string]int),
		MissingMedia:  make(map[int]struct{}),
	}

	// 3. Set up scan state — persistent mode is always active
	if setErr := db.SetIndexingSystems(currentSystemIDs); setErr != nil {
		return 0, fmt.Errorf("failed to set indexing systems: %w", setErr)
	}
	log.Info().Msgf("starting indexing for systems: %v", currentSystemIDs)

	// Populate scan state from existing DB (max IDs, system map, tag maps)
	if err = PopulateScanStateForSelectiveIndexing(ctx, db, &scanState, []string{}); err != nil {
		if errors.Is(err, context.Canceled) {
			return handleCancellation(ctx, db, "Media indexing cancelled during scan state population")
		}
		return 0, fmt.Errorf("failed to populate scan state: %w", err)
	}

	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusRunning); setErr != nil {
		log.Error().Err(setErr).Msg("failed to set indexing status to running")
	}
	if !shouldResume {
		if setErr := db.SetLastIndexedSystem(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear last indexed system")
		}
	}

	// Seed canonical tags based on tag existence, independent of resume state
	if scanState.TagTypesIndex == 0 {
		log.Info().Msg("seeding known tags")
		// SeedCanonicalTags runs in its own non-batch transaction for safety.
		err = SeedCanonicalTags(db, &scanState)
		if err != nil {
			return 0, fmt.Errorf("failed to seed known tags: %w", err)
		}
		log.Info().Msg("successfully seeded known tags")
	}

	// Ensure transaction cleanup and status update on completion or error
	defer func() {
		// Always attempt to rollback any dangling transaction, whether success or failure
		// On success, this should be a no-op (tx == nil), but ensures cleanup if
		// the last transaction was never committed due to batchStarted being false
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			if err != nil {
				log.Error().Err(rbErr).Msg("failed to rollback transaction after error")
			} else {
				log.Debug().Err(rbErr).Msg("no transaction to rollback (expected)")
			}
		}

		if err != nil {
			// Mark indexing as failed on error
			if setErr := db.SetIndexingStatus(mediadb.IndexingStatusFailed); setErr != nil {
				log.Error().Err(setErr).Msg("failed to set indexing status to failed after error")
			}
		}
	}()

	// Build sorted system list as the single loop driver. This covers all three
	// previous sources: sysPathIDs (systems with paths), launcher-specific
	// systems that may have no paths (was loop 2), and all systems for
	// any-scanners (was loop 3).
	sortedSystems := make([]systemdefs.System, len(systems))
	copy(sortedSystems, systems)
	sort.Slice(sortedSystems, func(i, j int) bool {
		return sortedSystems[i].ID < sortedSystems[j].ID
	})

	// Build any-scanner list once — launchers with no SystemID run for every system.
	var anyScanners []*platforms.Launcher
	allLaunchers := helpers.GlobalLauncherCache.GetAllLaunchers()
	for i := range allLaunchers {
		if allLaunchers[i].SystemID == "" && allLaunchers[i].Scanner != nil {
			anyScanners = append(anyScanners, &allLaunchers[i])
		}
	}

	status := IndexStatus{
		Total: len(sortedSystems) + 1, // +1 for final "Writing database" step
		Step:  0,
	}

	// Track UNIQUE constraint failures across all systems
	var uniqueConstraintFailures int

	// Track which launchers have already been scanned to prevent double-execution
	scannedLaunchers := make(map[string]bool)
	// This map tracks systems that have been fully processed and committed
	completedSystems := make(map[string]bool)

	// Populate completedSystems if resuming — use sortedSystems for consistent ordering
	if shouldResume {
		for _, sys := range sortedSystems {
			if sys.ID == lastIndexedSystemID {
				// DO NOT mark the last indexed system as completed - we need to resume from it
				// Only mark systems BEFORE the last indexed system as completed
				break
			}
			completedSystems[sys.ID] = true
		}
	}

	// Reset IsMissing flags only for systems that will actually be processed in
	// this run. On resume, skipped systems must keep their persisted markers.
	processableSystemDBIDs := make([]int, 0, len(sortedSystems))
	for _, sys := range sortedSystems {
		if completedSystems[sys.ID] {
			continue
		}
		if dbid, ok := scanState.SystemIDs[sys.ID]; ok {
			processableSystemDBIDs = append(processableSystemDBIDs, dbid)
		}
	}
	if len(processableSystemDBIDs) > 0 {
		if resetErr := db.ResetMissingFlags(processableSystemDBIDs); resetErr != nil {
			return 0, fmt.Errorf("failed to reset missing flags: %w", resetErr)
		}
		log.Info().Int("systems", len(processableSystemDBIDs)).Msg("reset IsMissing flags for indexed systems")
	}

	// Batch tracking variables for adaptive transaction management
	filesInBatch := 0
	systemsInBatch := 0
	batchStarted := false

	// Unified loop: each system is processed exactly once regardless of source.
	// Filesystem scan, per-system launcher scanners, and any-scanners are all
	// collected before the AddMediaPath phase. Populate* and FlushScanStateMaps
	// are called for every system, fixing the stale-state gaps that existed in
	// the previous loop 2 and loop 3.
	for _, sys := range sortedSystems {
		// Check for cancellation or pause
		select {
		case <-ctx.Done():
			return handleCancellationWithRollback(ctx, db, "Media indexing cancelled")
		default:
		}
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return handleCancellationWithRollback(ctx, db, "Media indexing cancelled while paused")
		}

		systemID := sys.ID

		// Resolve media type once per system to avoid repeated map lookups
		mediaType := slugs.MediaTypeGame
		if system, sysErr := systemdefs.GetSystem(systemID); sysErr == nil && system != nil {
			mediaType = system.GetMediaType()
		}

		if completedSystems[systemID] {
			log.Debug().Msgf("skipping already indexed system: %s", systemID)
			status.Step++
			update(status)
			continue
		}

		// Load existing data for this system — always persistent.
		if loadErr := PopulatePersistentScanStateForSystem(ctx, db, &scanState, systemID); loadErr != nil {
			if errors.Is(loadErr, context.Canceled) {
				return handleCancellation(ctx, db, "Media indexing cancelled during system data loading")
			}
			return 0, fmt.Errorf("failed to load system data for persistent indexing: %w", loadErr)
		}

		files := make([]platforms.ScanResult, 0)
		systemStartTime := time.Now()

		status.SystemID = systemID
		status.Step++
		update(status)

		log.Info().
			Str("system", systemID).
			Int("step", status.Step).
			Int("total", status.Total).
			Int("paths", len(systemPaths[systemID])).
			Msg("indexing system")

		// 1. Filesystem scan (no-op if this system has no configured paths)
		for _, systemPath := range systemPaths[systemID] {
			pathFiles, pathErr := GetFiles(ctx, cfg, platform, systemID, systemPath)
			if pathErr != nil {
				if errors.Is(pathErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during file scanning")
				}
				log.Error().Err(pathErr).Msgf("error getting files for system: %s", systemID)
				continue
			}
			for _, f := range pathFiles {
				files = append(files, platforms.ScanResult{Path: f})
			}
		}

		// 2. Per-system launcher scanners.
		//
		// SkipFilesystemScan launchers (e.g. RetroBat, Kodi) generate results
		// independently — they don't filter/enrich the shared file list, so they
		// receive empty input and their results are *appended* to files.
		//
		// Non-skip launchers (e.g. Batocera gamelist.xml enrichment) act as a
		// pipeline: they receive the current files and their output *replaces*
		// files, allowing them to filter, reorder, or add metadata.
		//
		// GetLaunchersBySystem returns nil for systems with no registered
		// launchers, making this loop a no-op for those systems. This also
		// replaces the previous loop 2 (launchers with a specific SystemID but
		// no filesystem paths) since all systems are now visited.
		sysLaunchers := helpers.GlobalLauncherCache.GetLaunchersBySystem(systemID)
		for i := range sysLaunchers {
			l := &sysLaunchers[i]
			if l.Scanner == nil {
				continue
			}
			log.Debug().Msgf("running %s scanner for system: %s", l.ID, systemID)
			var scanErr error
			if l.SkipFilesystemScan {
				// Isolated: scanner gets empty input, results accumulated
				var independent []platforms.ScanResult
				independent, scanErr = l.Scanner(ctx, cfg, systemID, nil)
				if scanErr == nil {
					files = append(files, independent...)
				}
			} else {
				// Pipeline: scanner filters/enriches existing files
				files, scanErr = l.Scanner(ctx, cfg, systemID, files)
			}
			if scanErr != nil {
				if errors.Is(scanErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during custom scanner")
				}
				log.Error().Err(scanErr).Msgf("error running %s scanner for system: %s", l.ID, systemID)
				continue
			}
			scannedLaunchers[l.ID] = true
		}

		// 3. Any-scanners — no SystemID, run for every system.
		//    Replaces loop 3 (previously a separate outer loop over anyScanners).
		for i := range anyScanners {
			log.Debug().Msgf("running %s 'any' scanner for system: %s", anyScanners[i].ID, systemID)
			results, scanErr := anyScanners[i].Scanner(ctx, cfg, systemID, []platforms.ScanResult{})
			if scanErr != nil {
				if errors.Is(scanErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during 'any' scanner")
				}
				log.Error().Err(scanErr).Msgf("error running %s 'any' scanner for system: %s",
					anyScanners[i].ID, systemID)
				continue
			}
			files = append(files, results...)
		}

		if len(files) == 0 {
			log.Debug().Msgf("no files found for system: %s", systemID)
		} else {
			status.Files += len(files)
			log.Debug().Msgf("scanned %d files for system: %s", len(files), systemID)
		}

		// Group files by directory and determine stripping policy per directory
		filesByDir := make(map[string][]platforms.ScanResult)
		for _, file := range files {
			dir := filepath.Dir(file.Path)
			filesByDir[dir] = append(filesByDir[dir], file)
		}

		stripPolicyByDir := make(map[string]bool)
		for dir, dirFiles := range filesByDir {
			// Use 50% threshold and require at least 5 files to apply heuristic
			stripPolicyByDir[dir] = detectNumberingPattern(dirFiles, 0.5, 5)
		}

		for _, file := range files {
			// Check for cancellation or pause between file processing
			select {
			case <-ctx.Done():
				return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during file processing")
			default:
			}
			if waitErr := pauser.Wait(ctx); waitErr != nil {
				return handleCancellationWithRollback(ctx, db,
					"Media indexing cancelled while paused during file processing")
			}

			// Start transaction if needed (at start of system OR after mid-system commit)
			if !batchStarted {
				if beginErr := db.BeginTransaction(true); beginErr != nil {
					return 0, fmt.Errorf("failed to begin new transaction: %w", beginErr)
				}
				batchStarted = true
			}

			// Look up stripping policy for this file's directory
			dir := filepath.Dir(file.Path)
			shouldStrip := stripPolicyByDir[dir]

			_, _, addErr := AddMediaPath(db, &scanState, systemID, file.Path, file.NoExt, shouldStrip, cfg, mediaType)
			if addErr != nil {
				var sqliteErr sqlite3.Error
				if errors.As(addErr, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
					uniqueConstraintFailures++
					log.Debug().Err(addErr).Str("path", file.Path).Msg("skipping duplicate media entry")
					continue
				}
				return 0, fmt.Errorf("unrecoverable error adding media path %q: %w", file.Path, addErr)
			}
			filesInBatch++

			// Commit if we hit file limit (memory safety - even mid-system)
			if filesInBatch >= maxFilesPerTransaction {
				commitStart := time.Now()
				if commitErr := db.CommitTransaction(); commitErr != nil {
					return 0, fmt.Errorf("failed to commit batch transaction (file limit): %w", commitErr)
				}
				commitElapsed := time.Since(commitStart)
				log.Debug().
					Str("system", systemID).
					Int("files", filesInBatch).
					Dur("commitTime", commitElapsed).
					Msg("committed batch (file limit)")
				if commitElapsed > 5*time.Second {
					log.Warn().
						Int("files", filesInBatch).
						Dur("commitTime", commitElapsed).
						Msg("database commit took longer than expected")
				}
				// Update progress after successful commit
				if setErr := db.SetLastIndexedSystem(systemID); setErr != nil {
					log.Error().Err(setErr).Msgf(
						"failed to set last indexed system to %s after file limit commit", systemID)
				}
				// NOTE: Do not flush TitleIDs/MediaIDs here — we are still
				// mid-system. Clearing them would break dedup for remaining
				// files in this system (multi-disc titles, persistent-mode
				// existing-media tracking). Flush only happens between systems.
				filesInBatch = 0
				systemsInBatch = 0
				batchStarted = false
			}
		}

		// Only count system in batch if we have pending uncommitted work
		if batchStarted {
			systemsInBatch++
		}

		// Mark system as processed (even if split across multiple commits)
		completedSystems[systemID] = true

		// In persistent mode, MissingMedia accumulates across all systems and is
		// flushed once at the end of indexing via a single BulkSetMediaMissing
		// call (after the final commit, since that writes through db.sql and
		// would otherwise contend with the open batch transaction).

		systemElapsed := time.Since(systemStartTime)
		log.Info().
			Str("system", systemID).
			Int("files", len(files)).
			Dur("elapsed", systemElapsed).
			Msg("completed system indexing")

		if systemElapsed > 30*time.Second {
			log.Warn().
				Str("system", systemID).
				Int("files", len(files)).
				Dur("elapsed", systemElapsed).
				Msg("system indexing took longer than expected - check for slow storage or large directories")
		}

		// Commit after N small systems OR when file limit forces it
		if batchStarted && systemsInBatch >= maxSystemsPerTransaction {
			commitStart := time.Now()
			if commitErr := db.CommitTransaction(); commitErr != nil {
				return 0, fmt.Errorf("failed to commit batch transaction (system limit): %w", commitErr)
			}
			commitElapsed := time.Since(commitStart)
			log.Debug().
				Int("systems", systemsInBatch).
				Int("files", filesInBatch).
				Dur("commitTime", commitElapsed).
				Msg("committed batch (system limit)")
			if commitElapsed > 5*time.Second {
				log.Warn().
					Int("files", filesInBatch).
					Dur("commitTime", commitElapsed).
					Msg("database commit took longer than expected")
			}
			// Update progress after successful commit
			if setErr := db.SetLastIndexedSystem(systemID); setErr != nil {
				log.Error().Err(setErr).Msgf(
					"failed to set last indexed system to %s after system limit commit", systemID)
			}
			FlushScanStateMaps(&scanState)
			filesInBatch = 0
			systemsInBatch = 0
			batchStarted = false
		} else {
			// Always flush between systems even without a commit — TitleIDs/MediaIDs
			// are system-scoped and Populate* re-loads them for the next system.
			FlushScanStateMaps(&scanState)
		}
	}

	// Commit any remaining uncommitted data from the unified loop
	if batchStarted && filesInBatch > 0 {
		if commitErr := db.CommitTransaction(); commitErr != nil {
			return 0, fmt.Errorf("failed to commit final batch transaction: %w", commitErr)
		}
		lastSystem := ""
		if len(sortedSystems) > 0 {
			lastSystem = sortedSystems[len(sortedSystems)-1].ID
		}
		if setErr := db.SetLastIndexedSystem(lastSystem); setErr != nil {
			log.Error().Err(setErr).Msg("failed to set last indexed system after final commit")
		}
		batchStarted = false
	}

	status.Step++
	status.SystemID = ""
	update(status)

	// Nil out all ScanState maps to release backing memory. Go maps retain
	// their allocated bucket array even after all keys are deleted, so the
	// only way to reclaim that memory is to drop all references and let GC
	// collect the backing arrays. With 250k titles this can be 20-40MB.
	scanState.SystemIDs = nil
	scanState.TitleIDs = nil
	scanState.MediaIDs = nil
	scanState.TagTypeIDs = nil
	scanState.TagIDs = nil

	// Phase 1: Complete data operations (foreground) - commit all data
	// Note: We may not have an active transaction here due to batching
	if batchStarted {
		err = db.CommitTransaction()
		if err != nil {
			return 0, fmt.Errorf("failed to commit final transaction: %w", err)
		}
	}

	// Flush accumulated missing-media markers in a single bulk update.
	// MissingMedia accumulates across all systems during persistent indexing
	// (FlushScanStateMaps preserves it). BulkSetMediaMissing writes through
	// db.sql, so it must run after every batch transaction has been committed
	// to avoid contending with the SQLite write lock.
	if len(scanState.MissingMedia) > 0 {
		log.Info().
			Int("missingCount", len(scanState.MissingMedia)).
			Msg("marking missing media")
		if missErr := db.BulkSetMediaMissing(scanState.MissingMedia); missErr != nil {
			return 0, fmt.Errorf("failed to mark missing media: %w", missErr)
		}
	}
	scanState.MissingMedia = nil
	status.Phase = PhaseCreatingIndexes
	update(status)

	// Rebuild all secondary indexes before marking indexing as complete.
	// This ensures the database is fully searchable when indexing finishes.
	t0 := time.Now()
	if idxErr := db.CreateSecondaryIndexes(); idxErr != nil {
		log.Error().Err(idxErr).Msg("failed to create secondary indexes")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("CreateSecondaryIndexes complete")

	// Mark database as complete and ready for use. UpdateLastGenerated clears
	// SystemTagsCache and SlugResolutionCache via invalidateCaches, so cache
	// population must happen after this call.
	err = db.UpdateLastGenerated()
	if err != nil {
		return 0, fmt.Errorf("failed to update last generated timestamp: %w", err)
	}

	status.Phase = PhaseBuildingCaches
	update(status)

	// Populate caches after UpdateLastGenerated. These run synchronously so
	// searches return correct results the moment indexing finishes, and the
	// populated SystemTagsCache persists across service restarts.
	t0 = time.Now()
	if cacheErr := db.PopulateSystemTagsCache(ctx); cacheErr != nil {
		log.Error().Err(cacheErr).Msg("failed to populate system tags cache")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("PopulateSystemTagsCache complete")

	t0 = time.Now()
	if cacheErr := db.RebuildSlugSearchCache(); cacheErr != nil {
		log.Error().Err(cacheErr).Msg("failed to rebuild slug search cache")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("RebuildSlugSearchCache complete")

	t0 = time.Now()
	if cacheErr := db.RebuildTagCache(); cacheErr != nil {
		log.Error().Err(cacheErr).Msg("failed to rebuild tag cache")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("RebuildTagCache complete")

	// Mark indexing as completed and clear indexing metadata
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCompleted); setErr != nil {
		log.Error().Err(setErr).Msg("failed to set indexing status to completed")
	}
	if setErr := db.SetLastIndexedSystem(""); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear last indexed system on completion")
	}
	if setErr := db.SetIndexingSystems(nil); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear indexing systems on completion")
	}

	// Invalidate media count cache after successful indexing
	if cacheErr := db.InvalidateCountCache(); cacheErr != nil {
		log.Error().Err(cacheErr).Msg("failed to invalidate media count cache after indexing")
	}

	// Mark optimization as pending — background optimization will handle
	// index rebuilds, cache population, and WAL checkpoint without blocking
	// game launches or search queries.
	err = db.SetOptimizationStatus("pending")
	if err != nil {
		err = fmt.Errorf("failed to set optimization status to pending: %w", err)
		log.Error().Err(err).Msg("failed to set optimization status to pending")
	}

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("processed systems: %v", completedSystems)
	for k, v := range completedSystems {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	if uniqueConstraintFailures > 0 {
		log.Warn().Int("count", uniqueConstraintFailures).
			Msg("UNIQUE constraint failures during indexing (possible duplicate paths)")
	}

	indexedFiles = status.Files
	indexElapsed := time.Since(indexStartTime)

	if err != nil {
		log.Error().
			Err(err).
			Int("files", indexedFiles).
			Int("systemsCompleted", len(indexedSystems)).
			Dur("elapsed", indexElapsed).
			Msg("media indexing completed with error")
	} else {
		log.Info().
			Int("files", indexedFiles).
			Int("systemsCompleted", len(indexedSystems)).
			Dur("elapsed", indexElapsed).
			Msg("media indexing completed successfully")
	}

	return indexedFiles, err
}
