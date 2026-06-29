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

package methods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// Browse pagination phases. A cursor in the dirs phase pages through
// directories (keyed by Name); the files phase pages through files. Directories
// always fully precede files, so a cursor only ever resumes one stream.
const (
	browsePhaseDirs  = "dirs"
	browsePhaseFiles = "files"
)

// browseCursorData is the JSON-serializable keyset cursor for browse pagination.
// Phase selects the stream the cursor resumes ("dirs" or "files"; absent means a
// legacy file-only cursor). DirName is the dirs-phase keyset; SortValue/SortMode/
// LastID are the files-phase keyset. TotalFiles/TotalDirs carry the first-page
// counts forward so cursor pages do not rerun the count queries.
type browseCursorData struct {
	SortValue  string `json:"sortValue"`
	SortMode   string `json:"sortMode,omitempty"`
	Phase      string `json:"phase,omitempty"`
	DirName    string `json:"dirName,omitempty"`
	LastID     int64  `json:"lastId"`
	TotalFiles int    `json:"totalFiles,omitempty"`
	TotalDirs  int    `json:"totalDirs,omitempty"`
}

func encodeCursorData(data *browseCursorData) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal browse cursor: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func encodeBrowseCursor(lastID int64, sortValue string, totalFiles ...int) (string, error) {
	data := browseCursorData{LastID: lastID, SortValue: sortValue}
	if len(totalFiles) > 0 && totalFiles[0] > 0 {
		data.TotalFiles = totalFiles[0]
	}
	return encodeCursorData(&data)
}

func encodeBrowseCursorWithMode(lastID int64, sortValue, sortMode string, totalFiles int) (string, error) {
	data := browseCursorData{LastID: lastID, SortValue: sortValue, SortMode: sortMode}
	if totalFiles > 0 {
		data.TotalFiles = totalFiles
	}
	return encodeCursorData(&data)
}

// encodeDirCursor builds a dirs-phase cursor positioned after dirName.
func encodeDirCursor(dirName string, totalFiles, totalDirs int) (string, error) {
	return encodeCursorData(&browseCursorData{
		Phase:      browsePhaseDirs,
		DirName:    dirName,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	})
}

// encodeFileCursor builds a files-phase cursor from the last file's keyset.
func encodeFileCursor(lastID int64, sortValue, sortMode string, totalFiles, totalDirs int) (string, error) {
	return encodeCursorData(&browseCursorData{
		Phase:      browsePhaseFiles,
		SortValue:  sortValue,
		SortMode:   sortMode,
		LastID:     lastID,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	})
}

// encodeFilesStartCursor builds a files-phase cursor with no keyset (LastID 0),
// marking the transition from the dirs phase so the next page starts files from
// the beginning.
func encodeFilesStartCursor(totalFiles, totalDirs int) (string, error) {
	return encodeCursorData(&browseCursorData{
		Phase:      browsePhaseFiles,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	})
}

func decodeBrowseCursor(cursor string) (*database.BrowseCursor, error) {
	if cursor == "" {
		return nil, nil //nolint:nilnil // empty cursor is valid
	}

	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, models.ClientErrf("invalid cursor format: %w", err)
	}

	var data browseCursorData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, models.ClientErrf("invalid cursor data: %w", err)
	}

	return &database.BrowseCursor{
		LastID:     data.LastID,
		SortValue:  data.SortValue,
		SortMode:   data.SortMode,
		Phase:      data.Phase,
		DirName:    data.DirName,
		TotalFiles: data.TotalFiles,
		TotalDirs:  data.TotalDirs,
	}, nil
}

// browseSem limits concurrent media.browse requests to avoid saturating SQLite.
var browseSem = make(chan struct{}, 3)

func logBrowseTiming(operation, path string, started time.Time, rows int) {
	log.Debug().
		Str("operation", operation).
		Str("path", path).
		Int("rows", rows).
		Dur("duration", time.Since(started)).
		Msg("media browse query completed")
}

// HandleMediaBrowse handles the media.browse API method for directory-style
// navigation of indexed media content.
func HandleMediaBrowse(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Debug().Msg("received media browse request")

	result, err := browseMedia(env)
	if err != nil && errors.Is(err, context.Canceled) {
		// The client navigated away or cancelled the request mid-browse. This is
		// expected and high-volume, so log at Debug to keep it out of Sentry.
		// context.DeadlineExceeded is intentionally NOT downgraded here — a browse
		// timeout may signal a real performance regression worth seeing.
		return nil, fmt.Errorf("%w", models.QuietClientErr(err))
	}
	return result, err
}

func browseMedia(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	select {
	case browseSem <- struct{}{}:
		defer func() { <-browseSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}

	var params models.BrowseParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid browse params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	maxResults := defaultMaxResults
	if params.MaxResults != nil && *params.MaxResults > 0 {
		maxResults = *params.MaxResults
	}

	var cursorStr string
	if params.Cursor != nil {
		cursorStr = *params.Cursor
	}
	cursor, err := decodeBrowseCursor(cursorStr)
	if err != nil {
		return nil, models.ClientErrf("invalid cursor: %w", err)
	}

	var sort string
	if params.Sort != nil {
		sort = *params.Sort
	}

	var systems []systemdefs.System
	if params.Systems != nil && len(*params.Systems) > 0 {
		fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
		var resolveErr error
		systems, resolveErr = resolveSystems(*params.Systems, fuzzy)
		if resolveErr != nil {
			return nil, resolveErr
		}
	}

	// No path → return root entries
	if params.Path == nil || *params.Path == "" {
		if len(systems) > 0 {
			return browseSystemRoots(&env, systems)
		}
		return browseRoots(&env)
	}

	path := *params.Path

	// Virtual path (contains ://)
	if strings.Contains(path, "://") {
		return browseVirtual(&env, path, cursor, maxResults, params.Letter, sort, systems)
	}

	// Filesystem path
	return browseFilesystem(&env, path, cursor, maxResults, params.Letter, sort, systems)
}

// browseRoots returns the top-level root entries: filesystem roots with indexed
// content and virtual scheme roots.
func browseRoots(env *requests.RequestEnv) (any, error) {
	ctx := env.Context

	var rootDirs []string
	if env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}

	// Get filesystem root counts
	rootCounts, err := env.Database.MediaDB.BrowseRootCounts(ctx, rootDirs)
	if err != nil {
		return nil, fmt.Errorf("error getting root counts: %w", err)
	}

	// Get virtual scheme roots
	virtualSchemes, err := env.Database.MediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting virtual schemes: %w", err)
	}

	entries := make([]models.BrowseEntry, 0, len(rootCounts)+len(virtualSchemes))

	// Add filesystem roots. Skip roots with a known count of 0 (no content).
	// Roots with nil count (cache not populated yet) are included without a count.
	for _, root := range rootDirs {
		count := rootCounts[root]
		if count != nil && *count == 0 {
			continue
		}
		entries = append(entries, models.BrowseEntry{
			Name:      filepath.Base(root),
			Path:      root,
			Type:      "root",
			FileCount: count,
		})
	}

	// Build scheme→group mapping from launcher cache
	schemeGroups := buildSchemeGroupMap(env)

	// Add virtual scheme roots
	for _, vs := range virtualSchemes {
		entry := models.BrowseEntry{
			Name:      schemeDisplayName(vs.Scheme),
			Path:      vs.Scheme,
			Type:      "root",
			FileCount: &vs.FileCount,
		}
		if group, ok := schemeGroups[vs.Scheme]; ok {
			entry.Group = &group
		}
		entries = append(entries, entry)
	}

	return models.BrowseResults{
		Entries: entries,
	}, nil
}

func browseSystemRoots(env *requests.RequestEnv, systems []systemdefs.System) (any, error) {
	started := time.Now()
	routes, err := buildSystemBrowseRouteCandidates(env, systems)
	if err != nil {
		return nil, err
	}
	systemIDs := make([]string, 0, len(systems))
	for _, system := range systems {
		systemIDs = append(systemIDs, system.ID)
	}
	log.Debug().
		Strs("systems", systemIDs).
		Int("routes", len(routes)).
		Dur("elapsed", time.Since(started)).
		Msg("media browse system root candidates built")

	started = time.Now()
	counts, err := env.Database.MediaDB.BrowseRouteCounts(env.Context, database.BrowseRouteCountsOptions{
		Routes:  routes,
		Systems: systems,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting system route counts: %w", err)
	}
	log.Debug().
		Strs("systems", systemIDs).
		Int("routes", len(routes)).
		Int("counts", len(counts)).
		Dur("elapsed", time.Since(started)).
		Msg("media browse system root counts loaded")

	entries := make([]models.BrowseEntry, 0, len(routes))
	schemeGroups := buildSchemeGroupMap(env)
	for _, route := range routes {
		count, ok := counts[route]
		if !ok || count.FileCount == 0 {
			continue
		}

		fileCount := count.FileCount
		entry := models.BrowseEntry{
			Name:      browseRouteDisplayName(route),
			Path:      route,
			Type:      "root",
			FileCount: &fileCount,
			SystemIDs: count.SystemIDs,
		}
		if len(count.SystemIDs) == 1 {
			entry.SystemID = &count.SystemIDs[0]
		}
		if group, ok := schemeGroups[route]; ok {
			entry.Group = &group
		}
		entries = append(entries, entry)
	}

	entries = dedupeSystemRootEntries(entries)

	return models.BrowseResults{Entries: entries}, nil
}

func dedupeSystemRootEntries(entries []models.BrowseEntry) []models.BrowseEntry {
	if len(entries) < 2 {
		return entries
	}

	filtered := make([]models.BrowseEntry, 0, len(entries))
	for i := range entries {
		if systemRootEntryCoveredByDescendant(entries, i) {
			continue
		}
		filtered = append(filtered, entries[i])
	}

	return filtered
}

func systemRootEntryCoveredByDescendant(entries []models.BrowseEntry, parentIdx int) bool {
	parent := entries[parentIdx]
	if parent.FileCount == nil {
		return false
	}

	descendantCount := 0
	foundDescendant := false
	for childIdx := range entries {
		if childIdx == parentIdx {
			continue
		}

		child := entries[childIdx]
		if !isStrictFilesystemDescendant(child.Path, parent.Path) {
			continue
		}
		if child.FileCount == nil {
			return false
		}

		foundDescendant = true
		descendantCount += *child.FileCount
	}

	return foundDescendant && descendantCount == *parent.FileCount
}

func isStrictFilesystemDescendant(childPath, parentPath string) bool {
	if strings.Contains(childPath, "://") || strings.Contains(parentPath, "://") {
		return false
	}

	child := filepath.Clean(childPath)
	parent := filepath.Clean(parentPath)
	if child == parent {
		return false
	}

	parentWithSeparator := parent
	if !strings.HasSuffix(parentWithSeparator, string(filepath.Separator)) {
		parentWithSeparator += string(filepath.Separator)
	}

	return strings.HasPrefix(child, parentWithSeparator)
}

func buildSystemBrowseRouteCandidates(env *requests.RequestEnv, systems []systemdefs.System) ([]string, error) {
	var rootDirs []string
	if env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}

	routes := make([]string, 0)
	seen := make(map[string]bool)
	addRoute := func(route string) {
		if route == "" || seen[route] {
			return
		}
		seen[route] = true
		routes = append(routes, route)
	}
	addFilesystemRoute := func(route string) {
		cleaned := filepath.Clean(route)
		if !isPathUnderRootDirs(cleaned, rootDirs) {
			return
		}
		addRoute(filepath.ToSlash(cleaned))
	}

	if env.LauncherCache != nil {
		for i := range systems {
			launchers := env.LauncherCache.GetLaunchersBySystem(systems[i].ID)
			for j := range launchers {
				launcher := &launchers[j]
				for _, scheme := range launcher.Schemes {
					addRoute(scheme + "://")
				}

				if launcher.SkipFilesystemScan {
					continue
				}
				for _, folder := range launcher.Folders {
					if filepath.IsAbs(folder) {
						addFilesystemRoute(folder)
						continue
					}
					for _, root := range rootDirs {
						addFilesystemRoute(filepath.Join(root, folder))
					}
				}
			}
		}
	}

	if env.Database.MediaDB != nil {
		if err := addBrowseDBSystemRoots(env, systems, rootDirs, addRoute, addFilesystemRoute); err != nil {
			return nil, err
		}
	}

	return routes, nil
}

func addBrowseDBSystemRoots(
	env *requests.RequestEnv,
	systems []systemdefs.System,
	rootDirs []string,
	addRoute func(string),
	addFilesystemRoute func(string),
) error {
	started := time.Now()
	candidates, cacheReady, err := env.Database.MediaDB.BrowseSystemRootCandidates(
		env.Context,
		database.BrowseSystemRootCandidatesOptions{Roots: rootDirs, Systems: systems},
	)
	if err != nil {
		return fmt.Errorf("error getting system root candidates: %w", err)
	}
	if cacheReady {
		logBrowseTiming("system_root_candidates", "", started, len(candidates.HasMedia))
		for _, root := range rootDirs {
			if !candidates.HasMedia[root] {
				continue
			}
			addFilesystemRoute(root)
			for _, name := range candidates.Children[root] {
				addFilesystemRoute(filepath.Join(root, name))
			}
		}
	} else {
		// Cache not ready yet (first boot, mid-rebuild). Fall back to the
		// per-root fan-out so the response is still complete.
		for _, root := range rootDirs {
			prefix := filepath.ToSlash(filepath.Clean(root))
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			fileCountStarted := time.Now()
			fileCount, fcErr := env.Database.MediaDB.BrowseFileCount(env.Context, database.BrowseFileCountOptions{
				PathPrefix: prefix,
				Systems:    systems,
			})
			logBrowseTiming("system_root_file_count", prefix, fileCountStarted, fileCount)
			if fcErr != nil {
				return fmt.Errorf("error getting system root file count: %w", fcErr)
			}
			if fileCount > 0 {
				addFilesystemRoute(root)
			}

			dirsStarted := time.Now()
			dirs, dirsErr := env.Database.MediaDB.BrowseDirectories(env.Context, database.BrowseDirectoriesOptions{
				PathPrefix: prefix,
				Systems:    systems,
			})
			logBrowseTiming("system_root_directories", prefix, dirsStarted, len(dirs))
			if dirsErr != nil {
				return fmt.Errorf("error getting system route directories: %w", dirsErr)
			}
			for _, dir := range dirs {
				addFilesystemRoute(filepath.Join(root, dir.Name))
			}
		}
	}

	virtualStarted := time.Now()
	virtualSchemes, err := env.Database.MediaDB.BrowseVirtualSchemes(
		env.Context,
		database.BrowseVirtualSchemesOptions{Systems: systems},
	)
	logBrowseTiming("system_virtual_schemes", "", virtualStarted, len(virtualSchemes))
	if err != nil {
		return fmt.Errorf("error getting system virtual routes: %w", err)
	}
	for _, scheme := range virtualSchemes {
		addRoute(scheme.Scheme)
	}
	return nil
}

func isPathUnderRootDirs(path string, rootDirs []string) bool {
	for _, root := range rootDirs {
		cleanedRoot := filepath.Clean(root)
		rel, err := filepath.Rel(cleanedRoot, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func browseRouteDisplayName(route string) string {
	if strings.Contains(route, "://") {
		return schemeDisplayName(route)
	}
	trimmed := strings.TrimSuffix(route, "/")
	if trimmed == "" {
		return route
	}
	return filepath.Base(trimmed)
}

// browseFilesystem lists the immediate children of a filesystem directory path
// by querying the indexed media database.
func browseFilesystem(
	env *requests.RequestEnv,
	path string,
	cursor *database.BrowseCursor,
	maxResults int,
	letter *string,
	sort string,
	systems []systemdefs.System,
) (any, error) {
	// Normalize the path
	cleaned := filepath.ToSlash(filepath.Clean(path))

	// Security: reject path traversal attempts
	if cleaned != filepath.ToSlash(path) && cleaned+"/" != filepath.ToSlash(path) {
		return nil, models.ClientErrf("invalid path: contains disallowed components")
	}

	// Security: verify path is within an allowed root
	var rootDirs []string
	if env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}
	if !isPathUnderRoots(cleaned, rootDirs) {
		return nil, models.ClientErrf("path is not within an allowed root directory")
	}

	// Ensure trailing slash for prefix matching
	prefix := cleaned
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	ctx := env.Context

	// Counts are computed once on the first page and carried forward in the
	// cursor so paging through a large directory does not rerun them.
	var totalDirs, totalFiles int
	if cursor != nil {
		totalDirs = cursor.TotalDirs
		totalFiles = cursor.TotalFiles
	}

	// Directory phase: directories are paginated by name and always precede
	// files, so a cursor only ever resumes one stream. The letter filter targets
	// files, so a letter query skips directories entirely.
	inDirsPhase := letter == nil && (cursor == nil || cursor.Phase == browsePhaseDirs)
	if inDirsPhase {
		afterName := ""
		if cursor != nil {
			afterName = cursor.DirName
		}
		started := time.Now()
		dirs, err := env.Database.MediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
			PathPrefix: prefix,
			AfterName:  afterName,
			Systems:    systems,
			Limit:      maxResults + 1,
		})
		logBrowseTiming("directories", prefix, started, len(dirs))
		if err != nil {
			return nil, fmt.Errorf("error browsing directories: %w", err)
		}

		if cursor == nil {
			started = time.Now()
			totalDirs, err = env.Database.MediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
				PathPrefix: prefix,
				Systems:    systems,
			})
			logBrowseTiming("dir_count", prefix, started, totalDirs)
			if err != nil {
				return nil, fmt.Errorf("error getting directory count: %w", err)
			}
		}

		hasMoreDirs := len(dirs) > maxResults
		if hasMoreDirs {
			dirs = dirs[:maxResults]
		}

		// More directories remain: emit a directory-only page keyed by name.
		if hasMoreDirs {
			if cursor == nil {
				totalFiles, err = browseTotalFileCount(ctx, env, prefix, nil, systems)
				if err != nil {
					return nil, err
				}
			}
			next, encErr := encodeDirCursor(dirs[len(dirs)-1].Name, totalFiles, totalDirs)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encode cursor: %w", encErr)
			}
			return buildBrowseResponse(env, cleaned, dirs, nil, maxResults, totalFiles, totalDirs, &next, true, systems)
		}

		// Directories are exhausted. Fill the rest of the page with the first
		// files so small folders return in a single round-trip (mixed boundary
		// page), then continue paging files from there.
		if cursor == nil {
			totalFiles, err = browseTotalFileCount(ctx, env, prefix, nil, systems)
			if err != nil {
				return nil, err
			}
		}
		remaining := maxResults - len(dirs)
		if remaining <= 0 {
			// The page is already full of directories; transition to the files
			// phase on the next page if any files exist.
			var next *string
			hasNext := totalFiles > 0
			if hasNext {
				encoded, encErr := encodeFilesStartCursor(totalFiles, totalDirs)
				if encErr != nil {
					return nil, fmt.Errorf("failed to encode cursor: %w", encErr)
				}
				next = &encoded
			}
			return buildBrowseResponse(
				env, cleaned, dirs, nil, maxResults, totalFiles, totalDirs, next, hasNext, systems)
		}

		started = time.Now()
		files, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: prefix,
			Limit:      remaining + 1,
			Sort:       sort,
			Systems:    systems,
		})
		logBrowseTiming("files", prefix, started, len(files))
		if err != nil {
			return nil, fmt.Errorf("error browsing files: %w", err)
		}
		files, next, encErr := paginateFiles(files, remaining, totalFiles, totalDirs, sort)
		if encErr != nil {
			return nil, encErr
		}
		return buildBrowseResponse(
			env, cleaned, dirs, files, maxResults, totalFiles, totalDirs, next, next != nil, systems)
	}

	// Files phase. A files-phase cursor with no keyset (LastID == 0) marks the
	// transition out of the dirs phase, so files start from the beginning.
	fileCursor := cursor
	if cursor != nil && cursor.Phase == browsePhaseFiles && cursor.LastID == 0 {
		fileCursor = nil
	}

	started := time.Now()
	files, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: prefix,
		Cursor:     fileCursor,
		Limit:      maxResults + 1,
		Letter:     letter,
		Sort:       sort,
		Systems:    systems,
	})
	logBrowseTiming("files", prefix, started, len(files))
	if err != nil {
		return nil, fmt.Errorf("error browsing files: %w", err)
	}

	// Get total file count. First-page cursors carry this forward so loading
	// additional pages in large directories does not repeat the same count query.
	if totalFiles == 0 && (len(files) > 0 || cursor != nil) {
		totalFiles, err = browseTotalFileCount(ctx, env, prefix, letter, systems)
		if err != nil {
			return nil, err
		}
	}

	files, next, encErr := paginateFiles(files, maxResults, totalFiles, totalDirs, sort)
	if encErr != nil {
		return nil, encErr
	}
	return buildBrowseResponse(env, cleaned, nil, files, maxResults, totalFiles, totalDirs, next, next != nil, systems)
}

// browseTotalFileCount returns the direct-child file count for a path prefix,
// logging the query timing.
func browseTotalFileCount(
	ctx context.Context,
	env *requests.RequestEnv,
	prefix string,
	letter *string,
	systems []systemdefs.System,
) (int, error) {
	started := time.Now()
	count, err := env.Database.MediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		PathPrefix: prefix,
		Letter:     letter,
		Systems:    systems,
	})
	logBrowseTiming("file_count", prefix, started, count)
	if err != nil {
		return 0, fmt.Errorf("error getting file count: %w", err)
	}
	return count, nil
}

// paginateFiles trims an over-fetched file slice to the page limit and, when
// more rows remain, encodes the keyset cursor for the next page.
func paginateFiles(
	files []database.SearchResultWithCursor,
	limit int,
	totalFiles, totalDirs int,
	sort string,
) (page []database.SearchResultWithCursor, next *string, err error) {
	if len(files) <= limit {
		return files, nil, nil
	}
	files = files[:limit]
	last := files[len(files)-1]
	sortValue := last.SortValue
	if sortValue == "" {
		switch sort {
		case "filename-asc", "filename-desc":
			sortValue = last.Path
		default:
			sortValue = last.Name
		}
	}
	encoded, encErr := encodeFileCursor(last.MediaID, sortValue, last.SortMode, totalFiles, totalDirs)
	if encErr != nil {
		return nil, nil, fmt.Errorf("failed to encode cursor: %w", encErr)
	}
	return files, &encoded, nil
}

// browseVirtual lists all indexed media entries under a virtual URI scheme.
func browseVirtual(
	env *requests.RequestEnv,
	schemePath string,
	cursor *database.BrowseCursor,
	maxResults int,
	letter *string,
	sort string,
	systems []systemdefs.System,
) (any, error) {
	// Validate scheme is known
	if !isKnownVirtualScheme(env, schemePath) {
		return nil, models.ClientErrf("unknown virtual scheme: %s", schemePath)
	}

	ctx := env.Context

	opts := &database.BrowseFilesOptions{
		PathPrefix: schemePath,
		Cursor:     cursor,
		Limit:      maxResults + 1,
		Letter:     letter,
		Sort:       sort,
		Systems:    systems,
	}
	started := time.Now()
	files, err := env.Database.MediaDB.BrowseFiles(ctx, opts)
	logBrowseTiming("virtual_files", schemePath, started, len(files))
	if err != nil {
		return nil, fmt.Errorf("error browsing virtual media: %w", err)
	}

	var totalFiles int
	if cursor != nil && cursor.TotalFiles > 0 {
		totalFiles = cursor.TotalFiles
	} else {
		totalFiles, err = browseTotalFileCount(ctx, env, schemePath, letter, systems)
		if err != nil {
			return nil, err
		}
	}

	files, next, encErr := paginateFiles(files, maxResults, totalFiles, 0, sort)
	if encErr != nil {
		return nil, encErr
	}
	return buildBrowseResponse(env, schemePath, nil, files, maxResults, totalFiles, 0, next, next != nil, systems)
}

// buildBrowseResponse assembles a BrowseResults page from directory and file
// entries plus precomputed pagination. Directory entries are singleton-alias
// enriched; pagination is attached whenever the page has entries so the caller's
// next cursor (directory keyset, files-phase transition, or file keyset) is
// surfaced.
func buildBrowseResponse(
	env *requests.RequestEnv,
	path string,
	dirs []database.BrowseDirectoryResult,
	files []database.SearchResultWithCursor,
	maxResults int,
	totalFiles int,
	totalDirs int,
	nextCursor *string,
	hasNextPage bool,
	systems []systemdefs.System,
) (any, error) {
	var rootDirs []string
	if env.LauncherCache != nil && env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}

	singletonAliases := resolveDirSingletonAliases(env, path, dirs, systems)

	entries := make([]models.BrowseEntry, 0, len(dirs)+len(files))
	for _, dir := range dirs {
		dirPath := filepath.ToSlash(filepath.Join(path, dir.Name))
		entry := models.BrowseEntry{
			Name:      dir.Name,
			Path:      dirPath,
			Type:      "directory",
			FileCount: &dir.FileCount,
			SystemIDs: dir.SystemIDs,
		}
		if alias, ok := singletonAliases[dirPath+"/"]; ok {
			result := database.SearchResultWithCursor{
				MediaID:       alias.Row.DBID,
				SystemID:      alias.Row.System.SystemID,
				Name:          browseMediaDisplayName(alias.Row.Path, alias.Row.SortName, alias.Row.Title.Name),
				Path:          alias.Row.Path,
				Tags:          alias.Tags,
				ZapScriptTags: alias.ZapScriptTags,
				HasCover:      alias.HasCover,
			}
			mediaEntry := buildMediaEntry(&result, env, rootDirs)
			entry.Name = mediaEntry.Name
			entry.MediaID = mediaEntry.MediaID
			entry.SystemID = mediaEntry.SystemID
			entry.RelPath = mediaEntry.RelPath
			entry.ZapScript = mediaEntry.ZapScript
			entry.Tags = mediaEntry.Tags
			entry.DisambiguatingTags = mediaEntry.DisambiguatingTags
			entry.HasCover = mediaEntry.HasCover
		}
		entries = append(entries, entry)
	}

	for i := range files {
		entry := buildMediaEntry(&files[i], env, rootDirs)
		entries = append(entries, entry)
	}

	var pagination *models.PaginationInfo
	if len(entries) > 0 {
		pagination = &models.PaginationInfo{
			NextCursor:  nextCursor,
			HasNextPage: hasNextPage,
			PageSize:    maxResults,
		}
	}

	return models.BrowseResults{
		Path:       path,
		Entries:    entries,
		Pagination: pagination,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}, nil
}

// resolveDirSingletonAliases batch-resolves singleton container aliases for the
// page's candidate directories when browsing a single system. Only small dirs
// (recursive FileCount <= maxSingletonAliasCandidateFiles) are considered, so
// large trees like MiSTer's _Arcade/_alternatives are never scanned.
func resolveDirSingletonAliases(
	env *requests.RequestEnv,
	path string,
	dirs []database.BrowseDirectoryResult,
	systems []systemdefs.System,
) map[string]database.SingletonContainerAlias {
	if len(dirs) == 0 || env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}

	// Determine which system to resolve against. Use the explicit filter if
	// exactly one system was requested; otherwise infer from the directory
	// entries (all dirs with media must belong to the same single system).
	var systemID string
	if len(systems) == 1 {
		systemID = systems[0].ID
	} else if len(systems) == 0 {
		for _, dir := range dirs {
			if len(dir.SystemIDs) == 0 {
				continue
			}
			if len(dir.SystemIDs) > 1 || (systemID != "" && systemID != dir.SystemIDs[0]) {
				systemID = ""
				break
			}
			systemID = dir.SystemIDs[0]
		}
	}
	if systemID == "" {
		return nil
	}

	var candidates []database.SingletonAliasCandidate
	prefix := path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for _, dir := range dirs {
		if !isSingletonDirectoryAliasCandidate(dir.FileCount) {
			continue
		}
		if len(dir.SystemIDs) > 0 && (len(dir.SystemIDs) != 1 || dir.SystemIDs[0] != systemID) {
			continue
		}
		candidates = append(candidates, database.SingletonAliasCandidate{
			ChildDir:  prefix + dir.Name + "/",
			FileCount: dir.FileCount,
		})
	}
	if len(candidates) == 0 || !singletonMediaAliasesEnabled(env) {
		return nil
	}

	started := time.Now()
	system, sysErr := env.Database.MediaDB.FindSystemBySystemID(systemID)
	if sysErr != nil {
		log.Debug().Err(sysErr).Str("system", systemID).Msg("browse singleton alias system lookup failed")
		return nil
	}
	aliases, aliasErr := env.Database.MediaDB.ResolveSingletonContainerAliases(
		env.Context, system.DBID, candidates,
	)
	if aliasErr != nil {
		log.Debug().Err(aliasErr).Str("path", path).Msg("browse singleton alias batch resolution failed")
		return nil
	}
	var singletonAliases map[string]database.SingletonContainerAlias
	if len(aliases) > 0 {
		singletonAliases = make(map[string]database.SingletonContainerAlias, len(aliases))
		for i := range aliases {
			singletonAliases[aliases[i].ChildDir] = aliases[i]
		}
	}
	log.Debug().
		Str("path", path).
		Int("candidates", len(candidates)).
		Int("aliases", len(singletonAliases)).
		Dur("duration", time.Since(started)).
		Msg("browse singleton alias resolution timing")
	return singletonAliases
}

func browseMediaDisplayName(path, sortName, titleName string) string {
	if sortName != "" {
		return sortName
	}

	base := filepath.Base(path)
	if base != "." && base != string(filepath.Separator) {
		if ext := filepath.Ext(base); ext != "" {
			base = base[:len(base)-len(ext)]
		}
		if base != "" {
			return base
		}
	}

	return titleName
}

// buildMediaEntry converts a SearchResultWithCursor into a BrowseEntry of type "media".
func buildMediaEntry(
	result *database.SearchResultWithCursor,
	env *requests.RequestEnv,
	rootDirs []string,
) models.BrowseEntry {
	entry := models.BrowseEntry{
		MediaID:            result.MediaID,
		Name:               result.Name,
		Path:               result.Path,
		Type:               "media",
		SystemID:           &result.SystemID,
		Tags:               result.Tags,
		DisambiguatingTags: result.ZapScriptTags,
		HasCover:           result.HasCover,
	}
	zapScript := result.ZapScript()
	entry.ZapScript = &zapScript

	if env.LauncherCache != nil {
		relPath := env.LauncherCache.ToRelativePath(rootDirs, result.SystemID, result.Path)
		entry.RelPath = &relPath
	}

	return entry
}

// isSingletonDirectoryAliasCandidate bounds the dirs considered for singleton
// alias resolution: real disc-folder containers hold a handful of files, and
// the cap keeps the batch query from fetching rows for large directory trees.
func isSingletonDirectoryAliasCandidate(fileCount int) bool {
	const maxSingletonAliasCandidateFiles = 64
	return fileCount > 0 && fileCount <= maxSingletonAliasCandidateFiles
}

// isPathUnderRoots checks if the given path is at or under one of the allowed root directories.
func isPathUnderRoots(path string, rootDirs []string) bool {
	for _, root := range rootDirs {
		if helpers.PathHasPrefix(path, root) {
			return true
		}
	}
	return false
}

// isKnownVirtualScheme checks if the given scheme path matches a launcher's scheme.
func isKnownVirtualScheme(env *requests.RequestEnv, schemePath string) bool {
	if env.LauncherCache == nil {
		return false
	}
	launchers := env.LauncherCache.GetAllLaunchers()
	for i := range launchers {
		for _, scheme := range launchers[i].Schemes {
			if schemePath == scheme+"://" {
				return true
			}
		}
	}
	return false
}

// buildSchemeGroupMap builds a mapping from virtual URI scheme prefix to the
// launcher group name. Uses the launcher's Groups[0] if available, otherwise
// falls back to the launcher ID.
func buildSchemeGroupMap(env *requests.RequestEnv) map[string]string {
	groups := make(map[string]string)
	if env.LauncherCache == nil {
		return groups
	}
	launchers := env.LauncherCache.GetAllLaunchers()
	for i := range launchers {
		var group string
		if len(launchers[i].Groups) > 0 {
			group = launchers[i].Groups[0]
		} else if launchers[i].ID != "" {
			group = launchers[i].ID
		}
		if group == "" {
			continue
		}
		for _, scheme := range launchers[i].Schemes {
			groups[scheme+"://"] = group
		}
	}
	return groups
}

// schemeDisplayName returns a human-readable name for a virtual URI scheme.
func schemeDisplayName(scheme string) string {
	name := strings.TrimSuffix(scheme, "://")
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
