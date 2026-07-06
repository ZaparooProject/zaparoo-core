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
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/perfmetrics"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/charlievieth/fastwalk"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// Batch configuration for transaction optimization
const (
	maxFilesPerTransaction = 5000
	// throttledMaxFilesPerTransaction is used instead of maxFilesPerTransaction
	// while background indexing is throttled or paused, so each commit's fsync
	// burst stays short and a throttle wait quickly follows it, rather than one
	// large uninterrupted 5000-file batch.
	throttledMaxFilesPerTransaction = 500
	mediaDatabaseCorruptMessage     = "media database is corrupt; manual repair or rebuild required; " +
		"original database left untouched"
	// walkEntryWaitInterval is how often (in scanned filesystem entries) the
	// parallel directory walk checks the pauser. Short enough that a throttled
	// walk still yields promptly even on a directory with few matched files.
	walkEntryWaitInterval = 200
)

// batchCommitLimit returns the file-count threshold for committing an
// indexing batch. While throttled or paused, commits are kept small so each
// fsync burst is short and a throttle wait quickly follows it, instead of one
// large uninterrupted batch competing with foreground storage access.
func batchCommitLimit(pauser *syncutil.Pauser) int {
	if pauser.IsThrottled() || pauser.IsPaused() {
		return throttledMaxFilesPerTransaction
	}
	return maxFilesPerTransaction
}

// maxReconcileRowsPerTransaction is kept for tests that exercise historical
// reconcile-volume commits. Production now commits at every system boundary;
// only the file-limit path can still commit mid-system.
var maxReconcileRowsPerTransaction int64 = 50000

func detectBrowsePrefixPolicy(files []platforms.ScanResult, threshold float64, minFiles int) browseprefix.Policy {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return browseprefix.DetectPolicyForPaths(paths, threshold, minFiles)
}

// detectNumberingPattern returns true when a directory looks like a ranked list.
// Kept for focused scanner tests; production code uses detectBrowsePrefixPolicy.
func detectNumberingPattern(files []platforms.ScanResult, threshold float64, minFiles int) bool {
	return detectBrowsePrefixPolicy(files, threshold, minFiles).Kind == browseprefix.KindRank
}

func isSQLiteDatabaseCorrupt(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrNotADB
	}

	msg := err.Error()
	return strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "file is not a database")
}

// noteIndexingCorruption flags the media database corrupt during indexing so the
// recovery flow rebuilds it: it logs an integrity fingerprint, writes the durable
// corrupt marker, persists the corrupt status, and clears the last-indexed-system
// pointer. Callers return the corruption error after invoking it.
func noteIndexingCorruption(db database.MediaDBI, reason string) {
	log.Error().Strs("integrity", db.IntegrityReport()).
		Msg("media database integrity check after corruption detected during indexing")
	db.MarkCorrupt(reason)
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCorrupt); setErr != nil {
		log.Error().Err(setErr).Msg("failed to mark media database as corrupt")
	}
	if setErr := db.SetLastIndexedSystem(""); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear last indexed system after corrupt database detection")
	}
}

// logMaintenanceError logs an indexing maintenance failure (status writes, cache
// population). When the failure is just the service context being cancelled
// mid-index — an expected shutdown condition — it logs at Debug; any other
// failure logs at Error so genuine problems stay visible in Sentry.
func logMaintenanceError(err error, msg string) {
	if errors.Is(err, context.Canceled) {
		log.Debug().Err(err).Msg(msg)
		return
	}
	log.Error().Err(err).Msg(msg)
}

type PathResult struct {
	Path   string
	System systemdefs.System
}

type slugSearchCacheDropper interface {
	DropSlugSearchCacheForSystems(systemIDs []string)
}

type indexingPlanStore interface {
	SetIndexingPlanSystems(systemIDs []string) error
	GetIndexingPlanSystems() ([]string, error)
}

func systemIDsFromDefs(systems []systemdefs.System) []string {
	systemIDs := make([]string, 0, len(systems))
	for _, system := range systems {
		systemIDs = append(systemIDs, system.ID)
	}
	return systemIDs
}

func systemDefsFromIDs(systemIDs []string) (systems []systemdefs.System, missing []string) {
	systems = make([]systemdefs.System, 0, len(systemIDs))
	missing = make([]string, 0)
	for _, systemID := range systemIDs {
		system, err := systemdefs.GetSystem(systemID)
		if err != nil || system == nil {
			missing = append(missing, systemID)
			continue
		}
		systems = append(systems, *system)
	}
	return systems, missing
}

func incompleteIndexedSystems(systems []systemdefs.System, completedSystems map[string]bool) []string {
	missing := make([]string, 0)
	for _, system := range systems {
		if !completedSystems[system.ID] {
			missing = append(missing, system.ID)
		}
	}
	return missing
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
	cfg *config.Instance,
	platform platforms.Platform,
	rootFolders []string,
	systems []systemdefs.System,
) []PathResult {
	launcherCache := &helpers.LauncherCache{}
	launcherCache.InitializeFromSlice(platform.Launchers(cfg))

	return getSystemPathsForLauncherCache(ctx, rootFolders, systems, launcherCache)
}

func getSystemPathsForLauncherCache(
	ctx context.Context,
	rootFolders []string,
	systems []systemdefs.System,
	launcherCache *helpers.LauncherCache,
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

		launchers := launcherCache.GetLaunchersBySystem(system.ID)

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

func filterRunnableSystems(
	systems []systemdefs.System,
	systemPaths map[string][]string,
	systemsWithScanners map[string]bool,
	existingSystemIDs map[string]bool,
	hasAnyScanner bool,
) []systemdefs.System {
	filtered := make([]systemdefs.System, 0, len(systems))
	for _, system := range systems {
		if len(systemPaths[system.ID]) > 0 {
			filtered = append(filtered, system)
			continue
		}

		if systemsWithScanners[system.ID] {
			filtered = append(filtered, system)
			continue
		}

		if existingSystemIDs[system.ID] {
			filtered = append(filtered, system)
			continue
		}

		if hasAnyScanner {
			filtered = append(filtered, system)
		}
	}

	return filtered
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
	pauser *syncutil.Pauser,
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
	if pauser.IsThrottled() || pauser.IsPaused() {
		// A parallel walk otherwise escapes the throttle: its worker
		// goroutines keep hammering storage regardless of the duty cycle
		// checked below. Single-threaded walking keeps concurrent reads
		// bounded while throttled or paused.
		conf.NumWorkers = 1
	}

	matcher := helpers.NewLauncherMatcher(cfg, platform)

	log.Debug().Str("system", systemID).Str("path", path).Msg("starting directory walk")
	err = fastwalk.Walk(conf, path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// fastwalk reports a directory read failure by re-invoking this
			// callback with the error an entry callback returned (see
			// walker.walk in fastwalk.go), which is also how our own
			// pauser.Wait/ctx cancellation below reaches here. A real
			// filesystem error is logged and skipped so the walk continues;
			// our own cancellation must propagate, not be swallowed as if
			// it were a bad directory.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
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

		if n%walkEntryWaitInterval == 0 {
			if waitErr := pauser.Wait(ctx); waitErr != nil {
				return fmt.Errorf("directory walk cancelled while throttled: %w", waitErr)
			}
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
				if matcher.MatchSystemFileForScan(system.ID, abs) {
					results = append(results, abs)
				}
			}
			mu.Unlock()
		} else if matcher.MatchSystemFileForScan(system.ID, p) {
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
		logMaintenanceError(setErr, "failed to set indexing status to cancelled")
	}
	return 0, ctx.Err()
}

// refreshMidScanCaches makes just-committed systems fully usable while the
// rest of the scan continues: the slug search cache serves fast search, the
// system tags cache serves tag filters, and the browse cache serves directory
// listings. Best-effort — a failure only means those systems stay on the SQL
// fallback paths until the end-of-run rebuild.
func refreshMidScanCaches(ctx context.Context, db database.MediaDBI, systemIDs []string) {
	started := time.Now()
	if err := db.RefreshSlugSearchCacheForSystems(ctx, systemIDs); err != nil {
		log.Warn().Err(err).Strs("systems", systemIDs).Msg("mid-scan slug search cache refresh failed")
	}
	sysDefs := make([]systemdefs.System, 0, len(systemIDs))
	for _, id := range systemIDs {
		if sys, err := systemdefs.GetSystem(id); err == nil && sys != nil {
			sysDefs = append(sysDefs, *sys)
		}
	}
	if len(sysDefs) > 0 {
		if err := db.PopulateSystemTagsCacheForSystems(ctx, sysDefs); err != nil {
			log.Warn().Err(err).Strs("systems", systemIDs).Msg("mid-scan system tags cache refresh failed")
		}
	}
	if err := db.PopulateBrowseCacheForSystems(ctx, systemIDs); err != nil {
		log.Warn().Err(err).Strs("systems", systemIDs).Msg("mid-scan browse cache refresh failed")
	}
	log.Debug().
		Strs("systems", systemIDs).
		Dur("elapsed", time.Since(started)).
		Msg("mid-scan cache refresh complete")
}

// handleCancellationWithRollback performs cleanup when media indexing is cancelled after transaction begins
func handleCancellationWithRollback(ctx context.Context, db database.MediaDBI, message string) (int, error) {
	log.Info().Msg(message)
	if rbErr := db.RollbackTransaction(); rbErr != nil {
		log.Error().Err(rbErr).Msg("failed to rollback transaction after cancellation")
	}
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCancelled); setErr != nil {
		logMaintenanceError(setErr, "failed to set indexing status to cancelled")
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
func newIndexLauncherCache(
	cfg *config.Instance,
	platform platforms.Platform,
) (*helpers.LauncherCache, []platforms.Launcher) {
	allLaunchers := platform.Launchers(cfg)
	allLaunchers = append(allLaunchers, launchables.Launchers(cfg, platform)...)
	launcherCache := &helpers.LauncherCache{}
	launcherCache.InitializeFromSlice(allLaunchers)
	return launcherCache, allLaunchers
}

func NewNamesIndex(
	ctx context.Context,
	platform platforms.Platform,
	cfg *config.Instance,
	systems []systemdefs.System,
	fdb *database.Database,
	update func(IndexStatus),
	pauser *syncutil.Pauser,
) (indexedFiles int, err error) {
	db := fdb.MediaDB
	indexStartTime := time.Now()
	metrics := perfmetrics.NewRecorderForDB(db)
	runMetricsStart := metrics.Capture(ctx, true)
	phaseMetricsStart := runMetricsStart
	logPhaseMetrics := func(phase string) {
		phaseMetricsEnd := metrics.Capture(ctx, true)
		perfmetrics.AddDelta(log.Info().Str("phase", phase), &phaseMetricsStart, &phaseMetricsEnd).
			Msg("media indexing phase metrics")
		phaseMetricsStart = phaseMetricsEnd
	}

	// Activate the NormalizeTag cache for the duration of this indexing run.
	// The bracket vocabulary is small (~200–400 unique strings), so the cache
	// stabilises early and collapses the repeated regex cost across 100k+ files.
	tags.SetNormalizeTagCache(make(map[string]string))
	defer tags.SetNormalizeTagCache(nil)

	// Same lifecycle for company-name normalization, which runs the slug word
	// pipeline per developer/publisher paren tag and repeats heavily.
	tags.SetCompanyNameCache(make(map[string]tags.TagValue))
	defer tags.SetCompanyNameCache(nil)

	// Temporarily increase SQLite cache to 32MB for bulk indexing
	db.SetIndexingCacheSize(true)
	defer db.SetIndexingCacheSize(false)

	log.Info().
		Int("systemCount", len(systems)).
		Msg("starting media indexing")

	// Track requested systems for resume validation before platform/path filtering.
	requestedSystemIDs := systemIDsFromDefs(systems)
	allSystemIDs := systemIDsFromDefs(systemdefs.AllSystems())
	fullRun := helpers.EqualStringSlices(requestedSystemIDs, allSystemIDs)

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
	var storedPlanSystemIDs []string

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
			case !helpers.EqualStringSlices(storedSystems, requestedSystemIDs):
				log.Warn().Msg("system list changed from previous indexing, reverting to fresh index")
			default:
				log.Info().Msgf("previous indexing interrupted. attempting to resume from system: %s",
					lastIndexedSystemID)
				shouldResume = true
				if planStore, ok := db.(indexingPlanStore); ok {
					var getPlanErr error
					storedPlanSystemIDs, getPlanErr = planStore.GetIndexingPlanSystems()
					if getPlanErr != nil {
						log.Warn().Err(getPlanErr).
							Msg("failed to get stored indexing plan; recomputing runnable systems")
						storedPlanSystemIDs = nil
					} else if len(storedPlanSystemIDs) == 0 {
						log.Warn().Msg("stored indexing plan missing; recomputing runnable systems")
					}
				}
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
	case mediadb.IndexingStatusCorrupt:
		return 0, errors.New(mediaDatabaseCorruptMessage)
	}

	// Build launcher metadata once so runnable-system filtering and scanner
	// execution both use the same launcher set even when the global cache is stale.
	launcherCache, allLaunchers := newIndexLauncherCache(cfg, platform)

	// Get the ordered list of systems for this run (deterministic by ID)
	update(IndexStatus{Phase: PhaseDiscovering})
	systemPaths := make(map[string][]string)
	for _, v := range getSystemPathsForLauncherCache(ctx, platform.RootDirs(cfg), systems, launcherCache) {
		systemPaths[v.System.ID] = append(systemPaths[v.System.ID], v.Path)
	}
	logPhaseMetrics("path_discovery")
	update(IndexStatus{Phase: PhaseInitializing})

	systemsWithScanners := make(map[string]bool, len(allLaunchers))
	// Build any-scanner list once so runnable-system filtering can preserve
	// platforms that intentionally discover media via scanner-only sources.
	var anyScanners []*platforms.Launcher
	for i := range allLaunchers {
		if allLaunchers[i].SystemID != "" && allLaunchers[i].Scanner != nil {
			systemsWithScanners[allLaunchers[i].SystemID] = true
		}
		if allLaunchers[i].SystemID == "" && allLaunchers[i].Scanner != nil {
			anyScanners = append(anyScanners, &allLaunchers[i])
		}
	}
	for _, item := range launchables.Media(cfg, platform) {
		systemsWithScanners[item.SystemID] = true
	}

	existingSystems, getExistingSystemsErr := db.GetAllSystems()
	if getExistingSystemsErr != nil {
		return 0, fmt.Errorf("failed to load existing systems for runnable-system filtering: %w", getExistingSystemsErr)
	}
	existingSystemIDs := make(map[string]bool, len(existingSystems))
	for _, system := range existingSystems {
		existingSystemIDs[system.SystemID] = true
	}

	systems = filterRunnableSystems(systems, systemPaths, systemsWithScanners, existingSystemIDs, len(anyScanners) > 0)
	if shouldResume && len(storedPlanSystemIDs) > 0 {
		planSystems, missingPlanSystems := systemDefsFromIDs(storedPlanSystemIDs)
		if len(missingPlanSystems) > 0 {
			log.Warn().Strs("systems", missingPlanSystems).
				Msg("stored indexing plan references unknown systems; reverting to fresh index")
			shouldResume = false
		} else {
			// Resume the exact runnable plan that was persisted when indexing started.
			// Recomputing it after a reboot can silently shrink the plan if media paths
			// or cached system rows are temporarily unavailable, which could otherwise
			// let a partial index reach the completion block and clear resume metadata.
			systems = planSystems
		}
	}
	currentSystemIDs := systemIDsFromDefs(systems)

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

	// Ensure transaction cleanup and status update on completion or error.
	// Register before persisting run metadata and seeding tags so early
	// initialization failures still leave a terminal failed status.
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

		// Mark indexing as failed on a genuine error. Cancellation (handleCancellation sets
		// Cancelled and returns ctx.Err()) and corruption (noteIndexingCorruption sets Corrupt)
		// already persist their own terminal status, so they must not be overwritten here.
		if err != nil && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) && !isSQLiteDatabaseCorrupt(err) {
			if setErr := db.SetIndexingStatus(mediadb.IndexingStatusFailed); setErr != nil {
				logMaintenanceError(setErr, "failed to set indexing status to failed after error")
			}
		}
	}()

	// 3. Record the requested system set and exact runnable plan for resume validation.
	if setErr := db.SetIndexingSystems(requestedSystemIDs); setErr != nil {
		return 0, fmt.Errorf("failed to set indexing systems: %w", setErr)
	}
	if planStore, ok := db.(indexingPlanStore); ok {
		if setErr := planStore.SetIndexingPlanSystems(currentSystemIDs); setErr != nil {
			return 0, fmt.Errorf("failed to set indexing plan systems: %w", setErr)
		}
	}
	log.Info().Msgf("starting indexing for requested systems: %v (runnable: %v)", requestedSystemIDs, currentSystemIDs)

	// Ensure the canonical tag vocabulary exists before any system reconciles
	// against it. Set-based: no existing rows are read into memory.
	if err = SeedCanonicalTags(ctx, db); err != nil {
		if errors.Is(err, context.Canceled) {
			return handleCancellation(ctx, db, "Media indexing cancelled during canonical tag seeding")
		}
		if isSQLiteDatabaseCorrupt(err) {
			noteIndexingCorruption(db, fmt.Sprintf("canonical tag seeding: %v", err))
			return 0, fmt.Errorf("%s: %w", mediaDatabaseCorruptMessage, err)
		}
		return 0, fmt.Errorf("failed to seed canonical tags: %w", err)
	}

	logPhaseMetrics("seed_canonical_tags")

	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusRunning); setErr != nil {
		logMaintenanceError(setErr, "failed to set indexing status to running")
	}
	if !shouldResume {
		if setErr := db.SetLastIndexedSystem(""); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear last indexed system")
		}
	}

	// Build sorted system list as the single loop driver. This covers all three
	// previous sources: sysPathIDs (systems with paths), launcher-specific
	// systems that may have no paths (was loop 2), and all systems for
	// any-scanners (was loop 3).
	sortedSystems := make([]systemdefs.System, len(systems))
	copy(sortedSystems, systems)
	sort.Slice(sortedSystems, func(i, j int) bool {
		return sortedSystems[i].ID < sortedSystems[j].ID
	})

	status := IndexStatus{
		Total: len(sortedSystems) + 1, // +1 for final "Writing database" step
		Step:  0,
	}

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

	// Batch tracking variables for adaptive transaction management. One
	// A transaction is committed at each system boundary. Earlier builds batched
	// multiple small systems into one transaction, but crash logs repeatedly ended
	// at the next system's status update while that cross-system transaction was
	// still open. Keep the mid-system file-limit commit for memory safety, then
	// finalize each system before moving on.
	filesInBatch := 0
	rowsInBatch := int64(0)
	batchStarted := false
	pendingSystems := make([]string, 0)
	// Set after the first system-boundary commit runs an approximate ANALYZE,
	// so a fresh database gets planner statistics minutes into the scan
	// instead of at the end.
	earlyAnalyzeDone := false

	// Sub-phase wall-time accumulators across the systems loop, logged once after
	// it so the monolithic "systems" phase can be attributed to its parts:
	// file collection (filesystem scan + scanners), staging row inserts,
	// per-system reconcile (set-based merge + missing flags + disambiguation),
	// and transaction commits (the fsync + checkpoint cost).
	var (
		collectDur   time.Duration
		insertDur    time.Duration
		reconcileDur time.Duration
		commitDur    time.Duration
	)

	// TODO: skip unchanged systems via a per-system fingerprint — store
	// hash(sorted walked paths) + parser version + media row count after each
	// system completes; on match at the next run, skip the state load, parse,
	// and reconcile phases entirely. Needs its own design pass for
	// invalidation rules (parser/config changes) and a forced-reindex path.
	//
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

		systemMetricsStart := metrics.Capture(ctx, false)
		systemID := sys.ID
		status.SystemID = systemID
		status.Step++
		update(status)

		// Resolve media type once per system to avoid repeated map lookups
		mediaType := slugs.MediaTypeGame
		if system, sysErr := systemdefs.GetSystem(systemID); sysErr == nil && system != nil {
			mediaType = system.GetMediaType()
		}

		if completedSystems[systemID] {
			log.Debug().Msgf("skipping already indexed system: %s", systemID)
			continue
		}

		if dropper, ok := db.(slugSearchCacheDropper); ok {
			dropper.DropSlugSearchCacheForSystems([]string{systemID})
		}

		// Drop any staged rows a crashed run left behind (a mid-system commit
		// makes staged rows durable); this system re-stages from scratch.
		if clearErr := db.ClearScanStage(); clearErr != nil {
			if isSQLiteDatabaseCorrupt(clearErr) {
				noteIndexingCorruption(db, fmt.Sprintf("scan stage clear for %s: %v", systemID, clearErr))
				return 0, fmt.Errorf("%s: %w", mediaDatabaseCorruptMessage, clearErr)
			}
			return 0, fmt.Errorf("failed to clear scan staging tables for %s: %w", systemID, clearErr)
		}

		files := make([]platforms.ScanResult, 0)
		systemStartTime := time.Now()
		collectStart := time.Now()
		// Set when any file source for this system errors. The staged set is
		// then a subset of the library, so the reconcile must not treat
		// absence from it as evidence media is missing.
		scanIncomplete := false

		log.Info().
			Str("system", systemID).
			Int("step", status.Step).
			Int("total", status.Total).
			Int("paths", len(systemPaths[systemID])).
			Msg("indexing system")

		// 1. Filesystem scan (no-op if this system has no configured paths)
		for _, systemPath := range systemPaths[systemID] {
			pathFiles, pathErr := GetFiles(ctx, cfg, platform, systemID, systemPath, pauser)
			if pathErr != nil {
				if errors.Is(pathErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during file scanning")
				}
				log.Error().Err(pathErr).Msgf("error getting files for system: %s", systemID)
				scanIncomplete = true
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
		sysLaunchers := launcherCache.GetLaunchersBySystem(systemID)
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
				// Pipeline: scanner filters/enriches existing files. Replace the
				// collected list only on success — a failing scanner must not
				// clobber it, or every file already collected would go missing.
				var piped []platforms.ScanResult
				piped, scanErr = l.Scanner(ctx, cfg, systemID, files)
				if scanErr == nil {
					files = piped
				}
			}
			if scanErr != nil {
				scanIncomplete = true
				if errors.Is(scanErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during custom scanner")
				}
				if errors.Is(scanErr, syscall.ECONNREFUSED) {
					// The scanner's backing service (e.g. Kodi) isn't running.
					// Expected on devices without it; keep out of Sentry.
					log.Warn().Err(scanErr).Msgf("skipping %s scanner: service unavailable", l.ID)
					continue
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
				scanIncomplete = true
				if errors.Is(scanErr, context.Canceled) {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during 'any' scanner")
				}
				log.Error().Err(scanErr).Msgf("error running %s 'any' scanner for system: %s",
					anyScanners[i].ID, systemID)
				continue
			}
			files = append(files, results...)
		}

		// 4. Platform-defined virtual media. These are indexed as normal MediaDB
		// rows with zaparoo:// paths, so search, browse, paging, and missing-state
		// handling stay in one place.
		for _, item := range launchables.MediaForSystem(cfg, platform, systemID) {
			files = append(files, platforms.ScanResult{
				Path:  item.ZapScript(),
				Name:  item.Name,
				NoExt: true,
			})
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
		if len(files) >= 1000 {
			log.Debug().
				Str("system", systemID).
				Int("files", len(files)).
				Int("directories", len(filesByDir)).
				Msg("grouped scanned files by directory")
		}

		prefixPolicyByDir := make(map[string]browseprefix.Policy)
		for dir, dirFiles := range filesByDir {
			// Use 50% threshold and require at least 5 files to apply heuristic
			prefixPolicyByDir[dir] = detectBrowsePrefixPolicy(
				dirFiles, browseprefix.DefaultThreshold, browseprefix.DefaultMinFiles,
			)
		}
		if len(files) >= 1000 {
			log.Debug().
				Str("system", systemID).
				Int("directories", len(prefixPolicyByDir)).
				Msg("calculated directory prefix policies")
		}

		collectDur += time.Since(collectStart)

		for fileIdx, file := range files {
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
				if len(files) >= 1000 {
					log.Debug().
						Str("system", systemID).
						Int("processed", fileIdx).
						Int("remaining", len(files)-fileIdx).
						Msg("beginning media indexing transaction")
				}
				if beginErr := db.BeginTransaction(true); beginErr != nil {
					return 0, fmt.Errorf("failed to begin new transaction: %w", beginErr)
				}
				batchStarted = true
			}

			// Look up stripping policy for this file's directory
			dir := filepath.Dir(file.Path)
			prefixPolicy := prefixPolicyByDir[dir]

			insertStart := time.Now()
			addErr := StageMediaPath(&StageMediaPathParams{
				Config:       cfg,
				DB:           db,
				Path:         file.Path,
				SystemID:     systemID,
				MediaType:    mediaType,
				ProvidedName: file.Name,
				PrefixPolicy: prefixPolicy,
				NoExt:        file.NoExt,
			})
			insertDur += time.Since(insertStart)
			if addErr != nil {
				if isSQLiteDatabaseCorrupt(addErr) {
					noteIndexingCorruption(db, fmt.Sprintf("media staging for %s: %v", systemID, addErr))
					return 0, fmt.Errorf("%s: %w", mediaDatabaseCorruptMessage, addErr)
				}
				return 0, fmt.Errorf("unrecoverable error staging media path %q: %w", file.Path, addErr)
			}
			filesInBatch++
			if len(files) >= 1000 && (fileIdx+1)%1000 == 0 {
				log.Debug().
					Str("system", systemID).
					Int("processed", fileIdx+1).
					Int("total", len(files)).
					Msg("processed media paths")
			}

			// Commit if we hit file limit (memory safety - even mid-system). The
			// current system is only partially processed here, so it is NOT marked
			// complete; on resume the cursor points at it and it is re-indexed from
			// scratch (idempotent). Systems fully processed earlier in this batch are
			// now durable and marked complete.
			if filesInBatch >= batchCommitLimit(pauser) {
				log.Debug().
					Str("system", systemID).
					Int("files", filesInBatch).
					Int("batchedSystems", len(pendingSystems)).
					Msg("committing media indexing batch (file limit)")
				commitStart := time.Now()
				if commitErr := db.CommitTransaction(); commitErr != nil {
					return 0, fmt.Errorf("failed to commit batch transaction (file limit): %w", commitErr)
				}
				commitElapsed := time.Since(commitStart)
				commitDur += commitElapsed
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
				// Resume cursor points at the in-progress system so it is redone;
				// systems sorted before it (including the batched, fully-processed
				// ones) are treated as complete on resume.
				if setErr := db.SetLastIndexedSystem(systemID); setErr != nil {
					log.Error().Err(setErr).Msgf(
						"failed to set last indexed system to %s after file limit commit", systemID)
				}
				for _, s := range pendingSystems {
					completedSystems[s] = true
				}
				pendingSystems = pendingSystems[:0]
				// Staged rows for this system are now durable; the reconcile at
				// system end still sees them (staging is a real table, not
				// transaction-local state), so a mid-system commit is safe.
				filesInBatch = 0
				rowsInBatch = 0
				batchStarted = false

				// Give a throttled/paused foreground consumer a window right
				// after the commit's fsync burst, before starting the next batch.
				if waitErr := pauser.Wait(ctx); waitErr != nil {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled after file-limit commit")
				}
			}
		}

		// Reconcile this system's staged rows into the media tables inside the
		// currently open transaction, which is shared across batched systems.
		// The set-based merge computes new/changed/missing rows, tag links, and
		// the touched-title disambiguation recompute entirely in SQL; the commit
		// itself is deferred to a batch boundary below so the fsync + checkpoint
		// cost is amortised.
		reconcileStart := time.Now()
		// A mid-system file-limit commit may have closed the transaction on the
		// final file; reopen one so the reconcile writes have a home.
		if !batchStarted {
			if beginErr := db.BeginTransaction(true); beginErr != nil {
				return 0, fmt.Errorf("failed to begin transaction to reconcile system %s: %w", systemID, beginErr)
			}
			batchStarted = true
		}
		if scanIncomplete {
			log.Warn().
				Str("system", systemID).
				Msg("file collection hit errors; keeping existing missing-media state for this system")
		}
		reconcileStats, reconcileErr := db.ReconcileStagedSystem(
			ctx, systemID, database.ScanReconcileOpts{IncompleteScan: scanIncomplete},
		)
		if reconcileErr != nil {
			if errors.Is(reconcileErr, context.Canceled) {
				return handleCancellationWithRollback(ctx, db, "Media indexing cancelled during system reconcile")
			}
			if isSQLiteDatabaseCorrupt(reconcileErr) {
				noteIndexingCorruption(db, fmt.Sprintf("staged reconcile for %s: %v", systemID, reconcileErr))
				return 0, fmt.Errorf("%s: %w", mediaDatabaseCorruptMessage, reconcileErr)
			}
			return 0, fmt.Errorf("failed to reconcile staged system %s: %w", systemID, reconcileErr)
		}
		if reconcileStats.SystemKnown {
			log.Debug().
				Str("system", systemID).
				Int64("titlesInserted", reconcileStats.TitlesInserted).
				Int64("titlesRenamed", reconcileStats.TitlesRenamed).
				Int64("mediaUpserted", reconcileStats.MediaUpserted).
				Int64("mediaMissing", reconcileStats.MediaMissing).
				Int64("tagsInserted", reconcileStats.TagsInserted).
				Int64("tagLinksAdded", reconcileStats.TagLinksAdded).
				Int64("tagLinksDeleted", reconcileStats.TagLinksDeleted).
				Int64("touchedTitles", reconcileStats.TouchedTitles).
				Msg("reconciled staged system")
			pendingSystems = append(pendingSystems, systemID)
			// Track reconcile write volume so a run of low-file/high-reconcile
			// systems still commits regularly, bounding WAL growth.
			rowsInBatch += reconcileStats.MediaMissing + reconcileStats.MediaUpserted +
				reconcileStats.TitlesInserted + reconcileStats.TouchedTitles
		} else {
			// System has no DB row and produced no files — nothing to commit.
			completedSystems[systemID] = true
		}
		reconcileDur += time.Since(reconcileStart)

		// Give a throttled/paused foreground consumer a window after
		// reconcile's set-based SQL merge, before the batch commit below.
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return handleCancellationWithRollback(ctx, db, "Media indexing cancelled after system reconcile")
		}

		// Commit at every system boundary so no transaction spans the next
		// system's staging clear or filesystem scan. This keeps the resume cursor
		// current and avoids carrying uncommitted WAL across many small systems.
		if batchStarted {
			log.Debug().
				Str("system", systemID).
				Int("files", filesInBatch).
				Int64("reconcileRows", rowsInBatch).
				Int("batchedSystems", len(pendingSystems)).
				Msg("committing media indexing batch")
			commitStart := time.Now()
			if commitErr := db.CommitTransaction(); commitErr != nil {
				return 0, fmt.Errorf("failed to commit batch transaction: %w", commitErr)
			}
			commitElapsed := time.Since(commitStart)
			commitDur += commitElapsed
			log.Debug().
				Str("system", systemID).
				Int("files", filesInBatch).
				Int("batchedSystems", len(pendingSystems)).
				Dur("commitTime", commitElapsed).
				Msg("committed batch")
			if commitElapsed > 5*time.Second {
				log.Warn().
					Int("files", filesInBatch).
					Dur("commitTime", commitElapsed).
					Msg("database commit took longer than expected")
			}
			// The cursor points at the last fully-finalized system; systems before
			// it are complete and it is redone on resume (idempotent).
			var justCommitted []string
			if len(pendingSystems) > 0 {
				lastDone := pendingSystems[len(pendingSystems)-1]
				if setErr := db.SetLastIndexedSystem(lastDone); setErr != nil {
					log.Error().Err(setErr).Msgf("failed to set last indexed system to %s after batch commit", lastDone)
				}
				for _, s := range pendingSystems {
					completedSystems[s] = true
				}
				justCommitted = append(justCommitted, pendingSystems...)
				pendingSystems = pendingSystems[:0]
			}
			filesInBatch = 0
			rowsInBatch = 0
			batchStarted = false

			// Give a throttled/paused foreground consumer a window right
			// after the commit's fsync burst, before analyze/cache refresh.
			if waitErr := pauser.Wait(ctx); waitErr != nil {
				return handleCancellationWithRollback(ctx, db, "Media indexing cancelled after system boundary commit")
			}

			if len(justCommitted) > 0 {
				// Give the query planner statistics as soon as the first system
				// lands: a fresh database has an empty sqlite_stat1 until the
				// end-of-run ANALYZE, and mid-scan fallback queries can pick
				// catastrophic plans without it.
				if !earlyAnalyzeDone {
					if analyzeErr := db.AnalyzeApproximate(); analyzeErr != nil {
						log.Warn().Err(analyzeErr).Msg("early approximate ANALYZE failed")
					}
					earlyAnalyzeDone = true
				}
				refreshMidScanCaches(ctx, db, justCommitted)

				// Cache refresh (slug search, tags, browse) is read-heavy SQL
				// work; give the foreground another window before the next
				// system starts.
				if waitErr := pauser.Wait(ctx); waitErr != nil {
					return handleCancellationWithRollback(ctx, db, "Media indexing cancelled after cache refresh")
				}
			}
		}

		systemElapsed := time.Since(systemStartTime)
		systemMetricsEnd := metrics.Capture(ctx, false)
		perfmetrics.AddDelta(
			log.Info().
				Str("system", systemID).
				Int("files", len(files)).
				Dur("elapsed", systemElapsed),
			&systemMetricsStart,
			&systemMetricsEnd,
		).Msg("completed system indexing")

		if systemElapsed > 30*time.Second {
			log.Warn().
				Str("system", systemID).
				Int("files", len(files)).
				Dur("elapsed", systemElapsed).
				Msg("system indexing took longer than expected - check for slow storage or large directories")
		}
	}

	log.Info().
		Dur("collect", collectDur).
		Dur("insert", insertDur).
		Dur("reconcile", reconcileDur).
		Dur("commit", commitDur).
		Msg("media indexing systems sub-phase breakdown")
	logPhaseMetrics("systems")

	if missingSystems := incompleteIndexedSystems(sortedSystems, completedSystems); len(missingSystems) > 0 {
		return 0, fmt.Errorf("media indexing stopped before completing all planned systems: %v", missingSystems)
	}

	status.Step++
	status.SystemID = ""
	update(status)

	status.Phase = PhaseCreatingIndexes
	update(status)

	// Rebuild all secondary indexes before marking indexing as complete.
	// This ensures the database is fully searchable when indexing finishes.
	t0 := time.Now()
	if idxErr := db.CreateSecondaryIndexes(); idxErr != nil {
		log.Error().Err(idxErr).Msg("failed to create secondary indexes")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("CreateSecondaryIndexes complete")
	logPhaseMetrics("create_secondary_indexes")

	// Mark database as complete and ready for use. UpdateLastGenerated clears
	// SystemTagsCache and SlugResolutionCache via invalidateCaches, so cache
	// population must happen after this call.
	err = db.UpdateLastGenerated()
	if err != nil {
		return 0, fmt.Errorf("failed to update last generated timestamp: %w", err)
	}
	logPhaseMetrics("update_last_generated")

	// Re-materialize user-owned data (favourites, launcher overrides) from UserDB
	// onto the freshly built media.db, before the caches below are populated so
	// they reflect it. Best-effort: a failure here only means the projection is
	// stale until the next reindex; the truth in UserDB is unaffected.
	t0 = time.Now()
	if applied, reapplyErr := reapplyMediaUserData(ctx, db, fdb.UserDB); reapplyErr != nil {
		log.Error().Err(reapplyErr).Msg("failed to re-apply media user data")
	} else {
		log.Info().Dur("elapsed", time.Since(t0)).Int("applied", applied).Msg("re-apply media user data complete")
	}
	logPhaseMetrics("reapply_media_user_data")

	status.Phase = PhaseBuildingCaches
	update(status)

	indexedSystems := make([]string, 0)
	log.Debug().Msgf("processed systems: %v", completedSystems)
	for k, v := range completedSystems {
		if v {
			indexedSystems = append(indexedSystems, k)
		}
	}
	sort.Strings(indexedSystems)
	log.Debug().Msgf("indexed systems: %v", indexedSystems)

	// UpdateLastGenerated (above) invalidated the SystemTagsCache rows for every
	// indexed system, so all of them must be repopulated here. Restricting this to
	// only the systems that changed would leave the preserved systems with no cache
	// rows, and GetSystemTagsCached — which checks the in-memory cache first and
	// never self-heals on a hit — would then return empty tags for them.
	indexedSystemDefs := make([]systemdefs.System, 0, len(indexedSystems))
	for _, systemID := range indexedSystems {
		system, getSystemErr := systemdefs.GetSystem(systemID)
		if getSystemErr != nil {
			log.Warn().Err(getSystemErr).Str("system", systemID).Msg("skipping scoped cache rebuild for unknown system")
			continue
		}
		indexedSystemDefs = append(indexedSystemDefs, *system)
	}

	selectiveRun := len(requestedSystemIDs) > 0 && !fullRun

	// Refresh planner statistics before the synchronous cache builds: their
	// aggregate queries otherwise plan against stats from before the bulk
	// (re)index, since the full ANALYZE only runs later in background
	// optimization.
	t0 = time.Now()
	if analyzeErr := db.AnalyzeApproximate(); analyzeErr != nil {
		log.Warn().Err(analyzeErr).Msg("failed to refresh planner statistics before cache builds")
	}
	log.Info().Dur("elapsed", time.Since(t0)).Msg("PragmaOptimize complete")
	logPhaseMetrics("pragma_optimize")

	// Populate caches after UpdateLastGenerated. For selective scans we rebuild the
	// persisted per-system SQL cache for the indexed systems, refresh in-memory slug
	// coverage for them, and rebuild the in-memory tag cache from the SystemTagsCache
	// table so first-entry requests for those systems stay warm too.
	t0 = time.Now()
	if selectiveRun {
		if cacheErr := db.PopulateSystemTagsCacheForSystems(ctx, indexedSystemDefs); cacheErr != nil {
			logMaintenanceError(cacheErr, "failed to populate system tags cache for indexed systems")
		}
		log.Info().
			Dur("elapsed", time.Since(t0)).
			Int("systems", len(indexedSystemDefs)).
			Msg("PopulateSystemTagsCacheForSystems complete")
		logPhaseMetrics("populate_system_tags_cache")

		t0 = time.Now()
		if cacheErr := db.RebuildTagCache(); cacheErr != nil {
			log.Error().Err(cacheErr).Msg("failed to rebuild tag cache after selective indexing")
		}
		log.Info().Dur("elapsed", time.Since(t0)).Msg("RebuildTagCache complete")
		logPhaseMetrics("rebuild_tag_cache")

		t0 = time.Now()
		if cacheErr := db.RefreshSlugSearchCacheForSystems(ctx, indexedSystems); cacheErr != nil {
			logMaintenanceError(cacheErr, "failed to refresh slug search cache for indexed systems")
		}
		log.Info().
			Dur("elapsed", time.Since(t0)).
			Int("systems", len(indexedSystems)).
			Msg("RefreshSlugSearchCacheForSystems complete")
		logPhaseMetrics("refresh_slug_search_cache")
	} else {
		if cacheErr := db.PopulateSystemTagsCache(ctx); cacheErr != nil {
			logMaintenanceError(cacheErr, "failed to populate system tags cache")
		}
		log.Info().Dur("elapsed", time.Since(t0)).Msg("PopulateSystemTagsCache complete")
		logPhaseMetrics("populate_system_tags_cache")

		t0 = time.Now()
		if cacheErr := db.RebuildSlugSearchCache(); cacheErr != nil {
			log.Error().Err(cacheErr).Msg("failed to rebuild slug search cache")
		}
		log.Info().Dur("elapsed", time.Since(t0)).Msg("RebuildSlugSearchCache complete")
		logPhaseMetrics("rebuild_slug_search_cache")

		t0 = time.Now()
		if cacheErr := db.RebuildTagCache(); cacheErr != nil {
			log.Error().Err(cacheErr).Msg("failed to rebuild tag cache")
		}
		log.Info().Dur("elapsed", time.Since(t0)).Msg("RebuildTagCache complete")
		logPhaseMetrics("rebuild_tag_cache")
	}

	// Bump the index generation counter so persisted cache files written
	// below carry the new value. Boot-time loads compare this against the
	// DB to detect stale cache files from a previous run.
	_, bumpErr := db.BumpIndexGeneration()
	if bumpErr != nil {
		log.Error().Err(bumpErr).Msg("failed to bump index generation")
	}

	// Persist the rebuilt in-memory caches to disk so a subsequent cold
	// boot can skip the SQL rebuild path. Best-effort: a write failure
	// just means next boot pays the rebuild cost, no correctness impact.
	// Skip if the bump failed: the persisted file would embed a generation
	// the DB doesn't agree with, and the next boot would load it as fresh.
	if bumpErr != nil {
		log.Warn().Msg("skipping cache persist because index generation bump failed")
	} else {
		if persistErr := db.PersistTagCache(); persistErr != nil {
			log.Error().Err(persistErr).Msg("failed to persist tag cache to disk")
		}
		if persistErr := db.PersistSlugSearchCache(); persistErr != nil {
			log.Error().Err(persistErr).Msg("failed to persist slug search cache to disk")
		}
	}
	logPhaseMetrics("persist_caches")

	// Mark indexing as completed and clear indexing metadata
	if setErr := db.SetIndexingStatus(mediadb.IndexingStatusCompleted); setErr != nil {
		logMaintenanceError(setErr, "failed to set indexing status to completed")
	}
	if setErr := db.SetLastIndexedSystem(""); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear last indexed system on completion")
	}
	if setErr := db.SetIndexingSystems(nil); setErr != nil {
		log.Error().Err(setErr).Msg("failed to clear indexing systems on completion")
	}
	if planStore, ok := db.(indexingPlanStore); ok {
		if setErr := planStore.SetIndexingPlanSystems(nil); setErr != nil {
			log.Error().Err(setErr).Msg("failed to clear indexing plan systems on completion")
		}
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

	indexedFiles = status.Files
	indexElapsed := time.Since(indexStartTime)
	indexMetricsEnd := metrics.Capture(ctx, true)
	perfmetrics.AddDelta(
		log.Info().
			Int("files", indexedFiles).
			Int("systemsCompleted", len(indexedSystems)).
			Dur("elapsed", indexElapsed),
		&runMetricsStart,
		&indexMetricsEnd,
	).Msg("media indexing resource summary")

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
