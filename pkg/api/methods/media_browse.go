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
	"encoding/base64"
	"encoding/json"
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

// browseCursorData is the JSON-serializable keyset cursor for browse pagination.
type browseCursorData struct {
	SortValue string `json:"sortValue"`
	LastID    int64  `json:"lastId"`
}

func encodeBrowseCursor(lastID int64, sortValue string) (string, error) {
	data := browseCursorData{LastID: lastID, SortValue: sortValue}
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal browse cursor: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
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
		LastID:    data.LastID,
		SortValue: data.SortValue,
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

	for childIdx := range entries {
		if childIdx == parentIdx {
			continue
		}

		child := entries[childIdx]
		if child.FileCount == nil || *child.FileCount != *parent.FileCount {
			continue
		}
		if isStrictFilesystemDescendant(child.Path, parent.Path) {
			return true
		}
	}

	return false
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
		for _, root := range rootDirs {
			prefix := filepath.ToSlash(filepath.Clean(root))
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			started := time.Now()
			fileCount, err := env.Database.MediaDB.BrowseFileCount(env.Context, database.BrowseFileCountOptions{
				PathPrefix: prefix,
				Systems:    systems,
			})
			logBrowseTiming("system_root_file_count", prefix, started, fileCount)
			if err != nil {
				return nil, fmt.Errorf("error getting system root file count: %w", err)
			}
			if fileCount > 0 {
				addFilesystemRoute(root)
			}

			started = time.Now()
			dirs, err := env.Database.MediaDB.BrowseDirectories(env.Context, database.BrowseDirectoriesOptions{
				PathPrefix: prefix,
				Systems:    systems,
			})
			logBrowseTiming("system_root_directories", prefix, started, len(dirs))
			if err != nil {
				return nil, fmt.Errorf("error getting system route directories: %w", err)
			}
			for _, dir := range dirs {
				addFilesystemRoute(filepath.Join(root, dir.Name))
			}
		}

		started := time.Now()
		virtualSchemes, err := env.Database.MediaDB.BrowseVirtualSchemes(
			env.Context,
			database.BrowseVirtualSchemesOptions{Systems: systems},
		)
		logBrowseTiming("system_virtual_schemes", "", started, len(virtualSchemes))
		if err != nil {
			return nil, fmt.Errorf("error getting system virtual routes: %w", err)
		}
		for _, scheme := range virtualSchemes {
			addRoute(scheme.Scheme)
		}
	}

	return routes, nil
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

	// Get subdirectories (only on first page)
	var dirs []database.BrowseDirectoryResult
	if cursor == nil {
		var err error
		started := time.Now()
		dirs, err = env.Database.MediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
			PathPrefix: prefix,
			Systems:    systems,
		})
		logBrowseTiming("directories", prefix, started, len(dirs))
		if err != nil {
			return nil, fmt.Errorf("error browsing directories: %w", err)
		}
	}

	// Get files
	opts := &database.BrowseFilesOptions{
		PathPrefix: prefix,
		Cursor:     cursor,
		Limit:      maxResults + 1,
		Letter:     letter,
		Sort:       sort,
		Systems:    systems,
	}
	started := time.Now()
	files, err := env.Database.MediaDB.BrowseFiles(ctx, opts)
	logBrowseTiming("files", prefix, started, len(files))
	if err != nil {
		return nil, fmt.Errorf("error browsing files: %w", err)
	}

	// Get total file count (skip when no files and no cursor — count is obviously 0)
	var totalFiles int
	if len(files) > 0 || cursor != nil {
		started = time.Now()
		totalFiles, err = env.Database.MediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
			PathPrefix: prefix,
			Letter:     letter,
			Systems:    systems,
		})
		logBrowseTiming("file_count", prefix, started, totalFiles)
		if err != nil {
			return nil, fmt.Errorf("error getting file count: %w", err)
		}
	}

	return buildBrowseResponse(env, cleaned, dirs, files, maxResults, totalFiles, sort)
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

	started = time.Now()
	totalFiles, err := env.Database.MediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		PathPrefix: schemePath,
		Letter:     letter,
		Systems:    systems,
	})
	logBrowseTiming("virtual_file_count", schemePath, started, totalFiles)
	if err != nil {
		return nil, fmt.Errorf("error getting virtual file count: %w", err)
	}

	return buildBrowseResponse(env, schemePath, nil, files, maxResults, totalFiles, sort)
}

// buildBrowseResponse assembles the BrowseResults from directories, files, and pagination info.
func buildBrowseResponse(
	env *requests.RequestEnv,
	path string,
	dirs []database.BrowseDirectoryResult,
	files []database.SearchResultWithCursor,
	maxResults int,
	totalFiles int,
	sort string,
) (any, error) {
	hasNextPage := len(files) > maxResults
	if hasNextPage {
		files = files[:maxResults]
	}

	entries := make([]models.BrowseEntry, 0, len(dirs)+len(files))

	// Add directory entries
	for _, dir := range dirs {
		entries = append(entries, models.BrowseEntry{
			Name:      dir.Name,
			Path:      path + "/" + dir.Name,
			Type:      "directory",
			FileCount: &dir.FileCount,
			SystemIDs: dir.SystemIDs,
		})
	}

	// Build file entries
	var rootDirs []string
	if env.LauncherCache != nil && env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}
	for i := range files {
		entry := buildMediaEntry(&files[i], env, rootDirs)
		entries = append(entries, entry)
	}

	// Build pagination
	var pagination *models.PaginationInfo
	if len(files) > 0 {
		var nextCursor *string
		if hasNextPage {
			lastResult := files[len(files)-1]
			var sortValue string
			switch sort {
			case "filename-asc", "filename-desc":
				sortValue = lastResult.Path
			default:
				sortValue = lastResult.Name
			}
			encoded, encErr := encodeBrowseCursor(lastResult.MediaID, sortValue)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encode cursor: %w", encErr)
			}
			nextCursor = &encoded
		}
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
	}, nil
}

// buildMediaEntry converts a SearchResultWithCursor into a BrowseEntry of type "media".
func buildMediaEntry(
	result *database.SearchResultWithCursor,
	env *requests.RequestEnv,
	rootDirs []string,
) models.BrowseEntry {
	entry := models.BrowseEntry{
		Name:     result.Name,
		Path:     result.Path,
		Type:     "media",
		SystemID: &result.SystemID,
		Tags:     result.Tags,
	}

	zapScript := result.ZapScript()
	entry.ZapScript = &zapScript

	if env.LauncherCache != nil {
		relPath := env.LauncherCache.ToRelativePath(rootDirs, result.SystemID, result.Path)
		entry.RelPath = &relPath
	}

	return entry
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
