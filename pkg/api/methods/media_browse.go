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
	"net/url"
	"path/filepath"
	"slices"
	sortpkg "sort"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// Browse pagination phases. A cursor in the dirs phase pages through
// directories (keyed by Name); the files phase pages through files. Directories
// always fully precede files, so a cursor only ever resumes one stream.
const (
	browsePhaseDirs  = "dirs"
	browsePhaseFiles = "files"

	browseRootViewRoutes   = "routes"
	browseRootViewContents = "contents"
)

// browseCursorData is the JSON-serializable keyset cursor for browse pagination.
// Phase selects the stream the cursor resumes ("dirs" or "files"; absent means a
// legacy file-only cursor). DirName is the dirs-phase keyset; SortValue/SortMode/
// LastID are the files-phase keyset. TotalFiles/TotalDirs carry the first-page
// counts forward so cursor pages do not rerun the count queries.
type browseCursorData struct {
	IncludeHidden       *bool                `json:"includeHidden,omitempty"`
	PreferencesRevision string               `json:"preferencesRevision,omitempty"`
	SortValue           string               `json:"sortValue"`
	SortMode            string               `json:"sortMode,omitempty"`
	Phase               string               `json:"phase,omitempty"`
	DirName             string               `json:"dirName,omitempty"`
	RootView            string               `json:"rootView,omitempty"`
	Sources             []browseCursorSource `json:"sources,omitempty"`
	LastID              int64                `json:"lastId"`
	TotalFiles          int                  `json:"totalFiles,omitempty"`
	TotalDirs           int                  `json:"totalDirs,omitempty"`
}

// browseCursorSource is one resolved route of a merged system root, carried
// forward so cursor pages do not rediscover the scope. Field names are short
// because they ride in every cursor.
type browseCursorSource struct {
	Path        string `json:"p"`
	IncludeDirs bool   `json:"d"`
}

// browseCursorScope is the part of a cursor that describes the browse scope
// rather than the position within it. Nil for an ordinary path browse, which
// re-derives its scope from the path on every page.
type browseCursorScope struct {
	RootView string
	Sources  []database.BrowseSource
}

func (s *browseCursorScope) apply(data *browseCursorData) {
	if s == nil {
		return
	}
	data.RootView = s.RootView
	if len(s.Sources) == 0 {
		return
	}
	data.Sources = make([]browseCursorSource, len(s.Sources))
	for i := range s.Sources {
		data.Sources[i] = browseCursorSource{
			Path:        s.Sources[i].PathPrefix,
			IncludeDirs: s.Sources[i].IncludeDirs,
		}
	}
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

func encodeBrowseCursorWithMode(
	lastID int64,
	sortValue, sortMode string,
	totalFiles int,
	scope *browseCursorScope,
) (string, error) {
	data := browseCursorData{LastID: lastID, SortValue: sortValue, SortMode: sortMode}
	if totalFiles > 0 {
		data.TotalFiles = totalFiles
	}
	scope.apply(&data)
	return encodeCursorData(&data)
}

// encodeDirCursor builds a dirs-phase cursor positioned after dirName.
func encodeDirCursor(
	dirName string, totalFiles, totalDirs int, scope *browseCursorScope,
) (string, error) {
	data := &browseCursorData{
		Phase:      browsePhaseDirs,
		DirName:    dirName,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}
	scope.apply(data)
	return encodeCursorData(data)
}

// encodeFileCursor builds a files-phase cursor from the last file's keyset.
func encodeFileCursor(
	lastID int64,
	sortValue, sortMode string,
	totalFiles, totalDirs int,
	scope *browseCursorScope,
) (string, error) {
	data := &browseCursorData{
		Phase:      browsePhaseFiles,
		SortValue:  sortValue,
		SortMode:   sortMode,
		LastID:     lastID,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}
	scope.apply(data)
	return encodeCursorData(data)
}

// encodeFilesStartCursor builds a files-phase cursor with no keyset (LastID 0),
// marking the transition from the dirs phase so the next page starts files from
// the beginning.
func encodeFilesStartCursor(
	totalFiles, totalDirs int, scope *browseCursorScope,
) (string, error) {
	data := &browseCursorData{
		Phase:      browsePhaseFiles,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}
	scope.apply(data)
	return encodeCursorData(data)
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

	switch data.Phase {
	case "", browsePhaseDirs, browsePhaseFiles:
	default:
		return nil, models.ClientErrf("invalid cursor phase: %q", data.Phase)
	}

	decoded := &database.BrowseCursor{
		LastID:     data.LastID,
		SortValue:  data.SortValue,
		SortMode:   data.SortMode,
		Phase:      data.Phase,
		DirName:    data.DirName,
		RootView:   data.RootView,
		TotalFiles: data.TotalFiles,
		TotalDirs:  data.TotalDirs,
	}
	if len(data.Sources) > maxBrowseCursorSources {
		// Cursors are unsigned client input. Each source becomes another
		// branch of the overlay statement, so an invented list would build a
		// huge query while holding one of the three browseSem slots. A merged
		// system root resolves to tens of routes; this is far above that.
		return nil, models.ClientErrf("cursor carries too many sources: %d", len(data.Sources))
	}
	if len(data.Sources) > 0 {
		decoded.Sources = make([]database.BrowseSource, len(data.Sources))
		for i := range data.Sources {
			decoded.Sources[i] = database.BrowseSource{
				PathPrefix:  data.Sources[i].Path,
				IncludeDirs: data.Sources[i].IncludeDirs,
			}
		}
	}
	return decoded, nil
}

// maxBrowseCursorSources bounds the resolved routes a cursor may carry.
const maxBrowseCursorSources = 256

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

func parseBrowseTagFilters(rawTags *[]string) ([]zapscript.TagFilter, error) {
	if rawTags == nil || len(*rawTags) == 0 {
		return nil, nil
	}
	tagFilters, err := filters.ParseTagFilters(*rawTags)
	if err != nil {
		return nil, models.ClientErrf("failed to parse tag filters: %w", err)
	}
	return tagFilters, nil
}

//nolint:gocritic // Request environment is a per-handler value.
func browseMedia(env requests.RequestEnv) (any, error) {
	endSlot := apidiag.Begin(env.Context, apidiag.ConcurrencySlot)
	defer endSlot()
	select {
	case browseSem <- struct{}{}:
		defer func() { <-browseSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}
	endSlot()

	var params models.BrowseParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid browse params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	tagFilters, err := parseBrowseTagFilters(params.Tags)
	if err != nil {
		return nil, err
	}
	env.ExcludeHidden = !filters.IncludesHidden(tagFilters, params.IncludeHidden)

	return runBrowseVisibility(&env, params.Cursor, func() (any, error) {
		return browseMediaRequest(&env, &params, tagFilters)
	})
}

//nolint:gocritic // Request environment is a per-handler value.
func browseMediaRequest(
	env *requests.RequestEnv, params *models.BrowseParams, tagFilters []zapscript.TagFilter,
) (any, error) {
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

	// No path → return root entries or an opt-in one-level contents overlay.
	if params.Path == nil || *params.Path == "" {
		rootView := browseRootViewRoutes
		if params.RootView != nil {
			rootView = *params.RootView
		}
		if cursor != nil && cursor.RootView != "" && cursor.RootView != rootView {
			return nil, models.ClientErrf("cursor does not match rootView %s", rootView)
		}
		if rootView == browseRootViewContents {
			if len(systems) != 1 {
				return nil, models.ClientErrf("rootView contents requires exactly one system")
			}
			if cursor != nil && cursor.RootView != browseRootViewContents {
				return nil, models.ClientErrf("cursor does not match rootView contents")
			}
			return browseSystemRootContents(
				env, systems, cursor, maxResults, params.Letter, sort, tagFilters,
			)
		}
		if len(systems) > 0 {
			return browseSystemRoots(env, systems)
		}
		return browseRoots(env)
	}

	path := *params.Path
	if cursor != nil && cursor.RootView != "" {
		return nil, models.ClientErrf("rootView contents cursor does not match path browse")
	}

	// Source root path: real nested folders below a host-granted root, unlike
	// every other virtual scheme, so it gets the filesystem-style dirs-then-
	// files browse below (not browseVirtual's flat listing), with its own
	// validation since filepath.Clean/Join mangle "://".
	if platforms.IsSourceScheme(path) {
		return browseSourcePath(env, path, cursor, maxResults, params.Letter, sort, systems, tagFilters)
	}

	// Virtual path (contains ://)
	if strings.Contains(path, "://") {
		return browseVirtual(env, path, cursor, maxResults, params.Letter, sort, systems, tagFilters)
	}

	// Launcher-relative path (SNES/USA): resolved to the indexed folder it
	// names, then browsed like any other filesystem path.
	resolved, isRelative, err := resolveRelativeBrowsePath(env, path, systems)
	if err != nil {
		return nil, err
	}
	if isRelative {
		path = resolved
	}

	// Filesystem path
	return browseFilesystem(env, path, cursor, maxResults, params.Letter, sort, systems, tagFilters)
}

// resolveRelativeBrowsePath turns a launcher-relative folder path, the shape
// relativePath has in browse responses (a system ID, optionally followed by a
// path below that system's launcher folder), into the absolute folder it
// names. The bool reports whether path had that shape; any other path is left
// for browseFilesystem to validate.
//
// A system's launcher folder can exist under several roots. The first one, in
// root order, that holds indexed content wins, which is the order a launch of
// the same relative path searches in.
func resolveRelativeBrowsePath(
	env *requests.RequestEnv, path string, systems []systemdefs.System,
) (resolved string, isRelative bool, err error) {
	slashed := filepath.ToSlash(path)
	if filepath.IsAbs(path) || strings.HasPrefix(slashed, "/") || env.LauncherCache == nil {
		return "", false, nil
	}
	// A path that does not survive cleaning is rejected by browseFilesystem.
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned != slashed && cleaned+"/" != slashed {
		return "", false, nil
	}

	systemPart, remainder, _ := strings.Cut(cleaned, "/")
	system, lookupErr := systemdefs.LookupSystem(systemPart)
	if lookupErr != nil {
		return "", false, nil //nolint:nilerr // not a relative path; browseFilesystem reports it
	}

	probeSystems := systems
	if len(probeSystems) == 0 {
		probeSystems = []systemdefs.System{*system}
	}
	for _, candidate := range relativeMediaPathCandidates(env, system.ID, remainder) {
		prefix := candidate + "/"
		dirCount, countErr := env.Database.MediaDB.BrowseDirCount(env.Context, database.BrowseDirCountOptions{
			ExcludeHidden: env.ExcludeHidden,
			PathPrefix:    prefix,
			Systems:       probeSystems,
		})
		if countErr != nil {
			return "", true, fmt.Errorf("error resolving relative path: %w", countErr)
		}
		if dirCount > 0 {
			return candidate, true, nil
		}
		fileCount, countErr := env.Database.MediaDB.BrowseFileCount(env.Context, database.BrowseFileCountOptions{
			ExcludeHidden: env.ExcludeHidden,
			PathPrefix:    prefix,
			Systems:       probeSystems,
		})
		if countErr != nil {
			return "", true, fmt.Errorf("error resolving relative path: %w", countErr)
		}
		if fileCount > 0 {
			return candidate, true, nil
		}
	}
	return "", true, models.ClientErrf("relative path not found: %s", cleaned)
}

// browseDirRelativePath returns the launcher-relative path of a directory:
// the system ID alone for the system's launcher folder, or the system ID
// followed by the path below it. It is nil when the directory cannot be
// attributed to exactly one system or does not sit under that system's
// launcher folders.
func browseDirRelativePath(
	env *requests.RequestEnv, dirPath string, dirSystemIDs []string, systems []systemdefs.System,
) *string {
	if env == nil || env.LauncherCache == nil || env.Platform == nil || strings.Contains(dirPath, "://") {
		return nil
	}
	var systemID string
	switch {
	case len(dirSystemIDs) == 1:
		systemID = dirSystemIDs[0]
	case len(dirSystemIDs) == 0 && len(systems) == 1:
		systemID = systems[0].ID
	default:
		return nil
	}

	if rel := mediaResponseRelativePath(env, systemID, dirPath); rel != nil {
		return rel
	}
	normalized := helpers.NormalizePathForComparison(dirPath)
	for _, candidate := range relativeMediaPathCandidates(env, systemID, "") {
		if helpers.NormalizePathForComparison(candidate) == normalized {
			return &systemID
		}
	}
	return nil
}

// browseRoots returns the top-level root entries: filesystem roots with indexed
// content and virtual scheme roots.
func browseRoots(env *requests.RequestEnv) (any, error) {
	ctx := env.Context

	rootDirs := browseRootDirs(env)

	// Get filesystem root counts
	rootCounts, err := env.Database.MediaDB.BrowseRootCounts(ctx, rootDirs, env.ExcludeHidden)
	if err != nil {
		return nil, fmt.Errorf("error getting root counts: %w", err)
	}

	// Get virtual scheme roots
	virtualSchemes, err := env.Database.MediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{
		ExcludeHidden: env.ExcludeHidden,
	})
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
	entries, err := resolveSystemRootEntries(env, systems)
	if err != nil {
		return nil, err
	}
	return models.BrowseResults{Entries: entries}, nil
}

func resolveSystemRootEntries(
	env *requests.RequestEnv,
	systems []systemdefs.System,
) ([]models.BrowseEntry, error) {
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
		ExcludeHidden: env.ExcludeHidden,
		Routes:        routes,
		Systems:       systems,
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
		if !ok {
			continue
		}
		// A route with a known zero count is empty and skipped. A degraded route
		// (CountUnknown) is known to contain media but its exact count timed out;
		// show it with no file count rather than hiding it.
		if count.FileCount == 0 && !count.CountUnknown {
			continue
		}

		entry := models.BrowseEntry{
			Name:      browseRouteDisplayName(route),
			Path:      route,
			Type:      "root",
			SystemIDs: count.SystemIDs,
		}
		if !count.CountUnknown {
			fileCount := count.FileCount
			entry.FileCount = &fileCount
		}
		if len(count.SystemIDs) == 1 {
			entry.SystemID = &count.SystemIDs[0]
		}
		entry.RelPath = browseDirRelativePath(env, route, count.SystemIDs, systems)
		if group, ok := schemeGroups[route]; ok {
			entry.Group = &group
		}
		entries = append(entries, entry)
	}

	entries = dedupeSystemRootEntries(entries)

	return entries, nil
}

func systemRootContentsSources(
	env *requests.RequestEnv,
	entries []models.BrowseEntry,
) ([]database.BrowseSource, []models.BrowseEntry) {
	physical := make([]models.BrowseEntry, 0, len(entries))
		if count.Hidden {
			entry.Tags = append(entry.Tags, hiddenDirectoryTag())
		}
	virtual := make([]models.BrowseEntry, 0)
	for i := range entries {
		switch {
		case platforms.IsSourcePath(entries[i].Path):
			// Unlike a genuinely flat virtual scheme (android://, scummvm://),
			// a rooted source path (expanded per granted root by
			// addBrowseDBSystemRoots, never the bare scheme bucket) has real
			// content of its own and merges into the page directly, the same
			// way a real RootDirs root already does - not a separate opaque
			// entry a client has to browse through first.
			physical = append(physical, entries[i])
		case strings.Contains(entries[i].Path, "://"):
			virtual = append(virtual, entries[i])
		default:
			physical = append(physical, entries[i])
		}
	}

	rootDirs := browseRootDirs(env)
	rootPriority := func(path string) int {
		for i, root := range rootDirs {
			if helpers.PathHasPrefix(path, root) {
				return i
			}
		}
		return len(rootDirs)
	}
	sortpkg.SliceStable(physical, func(i, j int) bool {
		return rootPriority(physical[i].Path) < rootPriority(physical[j].Path)
	})

	sources := make([]database.BrowseSource, 0, len(physical))
	for i := range physical {
		includeDirs := true
		for j := range physical {
			if i != j && isStrictFilesystemDescendant(physical[j].Path, physical[i].Path) {
				includeDirs = false
				break
			}
		}
		var prefix string
		if platforms.IsSourcePath(physical[i].Path) {
			// filepath.Clean mangles "://".
			prefix = strings.TrimSuffix(physical[i].Path, "/") + "/"
		} else {
			prefix = filepath.ToSlash(filepath.Clean(physical[i].Path))
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
		}
		sources = append(sources, database.BrowseSource{
			PathPrefix:  prefix,
			IncludeDirs: includeDirs,
		})
	}
	return sources, virtual
}

func browseSystemRootContents(
	env *requests.RequestEnv,
	systems []systemdefs.System,
	cursor *database.BrowseCursor,
	maxResults int,
	letter *string,
	sortOrder string,
	tags []zapscript.TagFilter,
) (any, error) {
	// Resolving the scope means discovering every route the system could live
	// under and counting each one to drop the empty ones. On MiSTer that is 21
	// candidate routes for NES, of which three hold media, and it cost 79ms of a
	// 115ms page when it ran on every page (#1460). The scope cannot change
	// between the pages of one listing, so the first page resolves it and hands
	// it to the rest through the cursor.
	var (
		sources        []database.BrowseSource
		virtualEntries []models.BrowseEntry
	)
	if cursor != nil && len(cursor.Sources) > 0 {
		sources = cursor.Sources
	} else {
		rootEntries, err := resolveSystemRootEntries(env, systems)
		if err != nil {
			return nil, err
		}
		sources, virtualEntries = systemRootContentsSources(env, rootEntries)
	}
	overlay := &database.BrowseOverlay{Sources: sources}
	scope := &browseCursorScope{RootView: browseRootViewContents, Sources: sources}
	// The merged root is the launcher's first screen for every system, but it was
	// the only browse path emitting no per-query timing, so a slow one showed up
	// only as an anonymous mediadb "browse call timing" line. Report the first
	// route as the path: the rest are on the same line via routes.
	overlayPath := ""
	if len(sources) > 0 {
		overlayPath = sources[0].PathPrefix
	}
	if cursor != nil {
		virtualEntries = nil
	}
	if len(sources) == 0 {
		return models.BrowseResults{Entries: virtualEntries}, nil
	}

	var (
		err                   error
		totalDirs, totalFiles int
	)
	if cursor != nil {
		totalDirs = cursor.TotalDirs
		totalFiles = cursor.TotalFiles
	}
	inDirsPhase := letter == nil && (cursor == nil || cursor.Phase == browsePhaseDirs)
	if inDirsPhase {
		afterName := ""
		if cursor != nil {
			afterName = cursor.DirName
		}
		started := time.Now()
		dirs, dirsErr := env.Database.MediaDB.BrowseDirectories(env.Context, database.BrowseDirectoriesOptions{
			ExcludeHidden: env.ExcludeHidden,
			Overlay:       overlay,
			AfterName:     afterName,
			Systems:       systems,
			Limit:         maxResults + 1,
		})
		logBrowseTiming("root_contents_directories", overlayPath, started, len(dirs))
		if dirsErr != nil {
			return nil, fmt.Errorf("error browsing system root contents directories: %w", dirsErr)
		}
		if cursor == nil {
			started = time.Now()
			totalDirs, err = env.Database.MediaDB.BrowseDirCount(env.Context, database.BrowseDirCountOptions{
				ExcludeHidden: env.ExcludeHidden,
				Overlay:       overlay,
				Systems:       systems,
			})
			logBrowseTiming("root_contents_dir_count", overlayPath, started, totalDirs)
			if err != nil {
				return nil, fmt.Errorf("error counting system root contents directories: %w", err)
			}
			totalFiles, err = browseRootContentsFileCount(env, sources, letter, systems, tags)
			if err != nil {
				return nil, err
			}
		}
		hasMoreDirs := len(dirs) > maxResults
		if hasMoreDirs {
			dirs = dirs[:maxResults]
			next, encErr := encodeDirCursor(
				dirs[len(dirs)-1].Name, totalFiles, totalDirs, scope,
			)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encode root contents cursor: %w", encErr)
			}
			return buildRootContentsResponse(
				env, dirs, nil, virtualEntries, maxResults, totalFiles, totalDirs, &next, true, systems, tags,
			)
		}

		remaining := maxResults - len(dirs)
		if remaining <= 0 {
			var next *string
			hasNext := totalFiles > 0
			if hasNext {
				encoded, encErr := encodeFilesStartCursor(totalFiles, totalDirs, scope)
				if encErr != nil {
					return nil, fmt.Errorf("failed to encode root contents cursor: %w", encErr)
				}
				next = &encoded
			}
			return buildRootContentsResponse(
				env, dirs, nil, virtualEntries, maxResults, totalFiles, totalDirs, next, hasNext, systems, tags,
			)
		}
		started = time.Now()
		files, filesErr := env.Database.MediaDB.BrowseFiles(env.Context, &database.BrowseFilesOptions{
			ExcludeHidden: env.ExcludeHidden,
			Overlay:       overlay,
			Limit:         remaining + 1,
			Sort:          sortOrder,
			Systems:       systems,
			Tags:          tags,
		})
		logBrowseTiming("root_contents_files", overlayPath, started, len(files))
		if filesErr != nil {
			return nil, fmt.Errorf("error browsing system root contents files: %w", filesErr)
		}
		files, next, pageErr := paginateFiles(
			files, remaining, totalFiles, totalDirs, sortOrder, scope,
		)
		if pageErr != nil {
			return nil, pageErr
		}
		return buildRootContentsResponse(
			env, dirs, files, virtualEntries, maxResults, totalFiles, totalDirs, next, next != nil, systems, tags,
		)
	}

	fileCursor := cursor
	if cursor != nil && cursor.Phase == browsePhaseFiles && cursor.LastID == 0 {
		fileCursor = nil
	}
	filesStarted := time.Now()
	files, filesErr := env.Database.MediaDB.BrowseFiles(env.Context, &database.BrowseFilesOptions{
		ExcludeHidden: env.ExcludeHidden,
		Overlay:       overlay,
		Cursor:        fileCursor,
		Limit:         maxResults + 1,
		Letter:        letter,
		Sort:          sortOrder,
		Systems:       systems,
		Tags:          tags,
	})
	logBrowseTiming("root_contents_files", overlayPath, filesStarted, len(files))
	if filesErr != nil {
		return nil, fmt.Errorf("error browsing system root contents files: %w", filesErr)
	}
	if totalFiles == 0 && (len(files) > 0 || cursor != nil) {
		totalFiles, err = browseRootContentsFileCount(env, sources, letter, systems, tags)
		if err != nil {
			return nil, err
		}
	}
	files, next, pageErr := paginateFiles(
		files, maxResults, totalFiles, totalDirs, sortOrder, scope,
	)
	if pageErr != nil {
		return nil, pageErr
	}
	return buildRootContentsResponse(
		env, nil, files, virtualEntries, maxResults, totalFiles, totalDirs, next, next != nil, systems, tags,
	)
}

func browseRootContentsFileCount(
	env *requests.RequestEnv,
	sources []database.BrowseSource,
	letter *string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) (int, error) {
	started := time.Now()
	count, err := env.Database.MediaDB.BrowseFileCount(env.Context, database.BrowseFileCountOptions{
		ExcludeHidden: env.ExcludeHidden,
		Overlay:       &database.BrowseOverlay{Sources: sources},
		Letter:        letter,
		Systems:       systems,
		Tags:          tags,
	})
	prefix := ""
	if len(sources) > 0 {
		prefix = sources[0].PathPrefix
	}
	logBrowseTiming("root_contents_file_count", prefix, started, count)
	if err != nil {
		return 0, fmt.Errorf("error counting system root contents files: %w", err)
	}
	return count, nil
}

func buildRootContentsResponse(
	env *requests.RequestEnv,
	dirs []database.BrowseDirectoryResult,
	files []database.SearchResultWithCursor,
	virtualEntries []models.BrowseEntry,
	maxResults, totalFiles, totalDirs int,
	nextCursor *string,
	hasNextPage bool,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) (any, error) {
	result, err := buildBrowseResponse(
		env, "", dirs, files, maxResults, totalFiles, totalDirs, nextCursor, hasNextPage, systems, tags,
	)
	if err != nil {
		return nil, err
	}
	browseResult, ok := result.(models.BrowseResults)
	if !ok {
		return nil, fmt.Errorf("unexpected root contents response type %T", result)
	}
	if len(virtualEntries) > 0 {
		entries := make([]models.BrowseEntry, 0, len(virtualEntries)+len(browseResult.Entries))
		entries = append(entries, virtualEntries...)
		entries = append(entries, browseResult.Entries...)
		browseResult.Entries = entries
	}
	return browseResult, nil
}

func dedupeSystemRootEntries(entries []models.BrowseEntry) []models.BrowseEntry {
	if len(entries) < 2 {
		return entries
	}

	// Route candidates can contain several ancestor levels. One pass removes an
	// intermediate route, but a grandparent evaluated against the original set
	// double-counts both that intermediate subtree and its leaf routes. Repeat on
	// the reduced set until stable so every covered ancestor is removed while
	// preserving parents with genuinely unmatched direct media.
	current := entries
	for {
		filtered := make([]models.BrowseEntry, 0, len(current))
		for i := range current {
			if systemRootEntryCoveredByDescendant(current, i) {
				continue
			}
			filtered = append(filtered, current[i])
		}
		if len(filtered) == len(current) {
			return filtered
		}
		current = filtered
	}
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
	rootDirs := browseRootDirs(env)
	// A launcher's own absolute folder is a destination, not a place to look
	// for other launchers' relative folders, so those joins use scan roots only.
	var scanRoots []string
	if env.Platform != nil {
		scanRoots = env.Platform.RootDirs(env.Config)
	}
	// A granted source root has the same per-system folder convention a real
	// RootDirs root does (mediascanner's indexing already walks it the same
	// way), so it is joined against the same relative launcher folders below
	// - the source-root equivalent of scanRoots, found the same way
	// addBrowseDBSystemRoots finds any other route: this one just also needs
	// the folder name to go deeper than the bare granted root, which has no
	// content of its own.
	var sourceRoots []string
	if reader, ok := env.Platform.(platforms.SourceRootReader); ok {
		sourceRoots, _ = reader.SourceRoots(env.Context)
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
	addSourceRoute := func(root, folder string) {
		// filepath.Join mangles "://"; root and folder already share "/" as
		// their only separator. The trailing slash matters: browseRouteCacheKey
		// leaves any "://"-containing route unchanged (unlike a real filesystem
		// path, which it appends one to), so without it here the route would
		// never match the cache's own node for this folder, which is always
		// stored with one (sourceCacheAncestorDirs).
		addRoute(strings.TrimSuffix(root, "/") + "/" + folder + "/")
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
					for _, root := range scanRoots {
						addFilesystemRoute(filepath.Join(root, folder))
					}
					for _, root := range sourceRoots {
						addSourceRoute(root, folder)
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
		database.BrowseSystemRootCandidatesOptions{Roots: rootDirs, Systems: systems, ExcludeHidden: env.ExcludeHidden},
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
				ExcludeHidden: env.ExcludeHidden,
				PathPrefix:    prefix,
				Systems:       systems,
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
				ExcludeHidden: env.ExcludeHidden,
				PathPrefix:    prefix,
				Systems:       systems,
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
		database.BrowseVirtualSchemesOptions{Systems: systems, ExcludeHidden: env.ExcludeHidden},
	)
	logBrowseTiming("system_virtual_schemes", "", virtualStarted, len(virtualSchemes))
	if err != nil {
		return fmt.Errorf("error getting system virtual routes: %w", err)
	}
	for _, scheme := range virtualSchemes {
		// The bare source scheme itself is never offered as a route: a
		// source root has real content of its own, unlike a genuinely flat
		// virtual scheme (android://, scummvm://), so it is discovered the
		// same way a real RootDirs entry already is, per system-matching
		// folder, in the loop above - see the sourceRoots join alongside
		// scanRoots. Falling back to the bare bucket here would offer a
		// second, broader, overlapping route into the same content with no
		// way to dedupe against the more specific one (dedupeSystemRootEntries
		// only compares real filesystem paths).
		if scheme.Scheme == platforms.SourceScheme+"://" {
			continue
		}
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
	tags []zapscript.TagFilter,
) (any, error) {
	// Normalize the path
	cleaned := filepath.ToSlash(filepath.Clean(path))

	// Security: reject path traversal attempts
	if cleaned != filepath.ToSlash(path) && cleaned+"/" != filepath.ToSlash(path) {
		return nil, models.ClientErrf("invalid path: contains disallowed components")
	}

	// Security: verify path is within an allowed root
	if !isPathUnderRoots(cleaned, browseRootDirs(env)) {
		return nil, models.ClientErrf("path is not within an allowed root directory")
	}

	// Ensure trailing slash for prefix matching
	prefix := cleaned
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return browsePathPrefix(env, prefix, cleaned, cursor, maxResults, letter, sort, systems, tags)
}

// browsePathPrefix pages a directory-then-files listing for an already
// validated, already slash-terminated path prefix, shared by browseFilesystem
// (a real OS path under RootDirs) and browseSourcePath (a source root path):
// everything from here on is opaque-string matching against the indexed
// database, and does not care which kind of path prefix it was given.
// displayPath is what the response and the next browse path for each child
// entry are built from.
func browsePathPrefix(
	env *requests.RequestEnv,
	prefix, displayPath string,
	cursor *database.BrowseCursor,
	maxResults int,
	letter *string,
	sort string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) (any, error) {
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
			ExcludeHidden: env.ExcludeHidden,
			PathPrefix:    prefix,
			AfterName:     afterName,
			Systems:       systems,
			Limit:         maxResults + 1,
		})
		logBrowseTiming("directories", prefix, started, len(dirs))
		if err != nil {
			return nil, fmt.Errorf("error browsing directories: %w", err)
		}

		if cursor == nil {
			started = time.Now()
			totalDirs, err = env.Database.MediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
				ExcludeHidden: env.ExcludeHidden,
				PathPrefix:    prefix,
				Systems:       systems,
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
				totalFiles, err = browseTotalFileCount(ctx, env, prefix, nil, systems, tags)
				if err != nil {
					return nil, err
				}
			}
			next, encErr := encodeDirCursor(dirs[len(dirs)-1].Name, totalFiles, totalDirs, nil)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encode cursor: %w", encErr)
			}
			return buildBrowseResponse(
				env, displayPath, dirs, nil, maxResults, totalFiles, totalDirs, &next, true, systems, tags)
		}

		// Directories are exhausted. Fill the rest of the page with the first
		// files so small folders return in a single round-trip (mixed boundary
		// page), then continue paging files from there.
		if cursor == nil {
			totalFiles, err = browseTotalFileCount(ctx, env, prefix, nil, systems, tags)
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
				encoded, encErr := encodeFilesStartCursor(totalFiles, totalDirs, nil)
				if encErr != nil {
					return nil, fmt.Errorf("failed to encode cursor: %w", encErr)
				}
				next = &encoded
			}
			return buildBrowseResponse(
				env, displayPath, dirs, nil, maxResults, totalFiles, totalDirs, next, hasNext, systems, tags)
		}

		started = time.Now()
		files, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			ExcludeHidden: env.ExcludeHidden,
			PathPrefix:    prefix,
			Limit:         remaining + 1,
			Sort:          sort,
			Systems:       systems,
			Tags:          tags,
		})
		logBrowseTiming("files", prefix, started, len(files))
		if err != nil {
			return nil, fmt.Errorf("error browsing files: %w", err)
		}
		files, next, encErr := paginateFiles(files, remaining, totalFiles, totalDirs, sort, nil)
		if encErr != nil {
			return nil, encErr
		}
		return buildBrowseResponse(
			env, displayPath, dirs, files, maxResults, totalFiles, totalDirs, next, next != nil, systems, tags)
	}

	// Files phase. A files-phase cursor with no keyset (LastID == 0) marks the
	// transition out of the dirs phase, so files start from the beginning.
	fileCursor := cursor
	if cursor != nil && cursor.Phase == browsePhaseFiles && cursor.LastID == 0 {
		fileCursor = nil
	}

	started := time.Now()
	files, err := env.Database.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		ExcludeHidden: env.ExcludeHidden,
		PathPrefix:    prefix,
		Cursor:        fileCursor,
		Limit:         maxResults + 1,
		Letter:        letter,
		Sort:          sort,
		Systems:       systems,
		Tags:          tags,
	})
	logBrowseTiming("files", prefix, started, len(files))
	if err != nil {
		return nil, fmt.Errorf("error browsing files: %w", err)
	}

	// Get total file count. First-page cursors carry this forward so loading
	// additional pages in large directories does not repeat the same count query.
	if totalFiles == 0 && (len(files) > 0 || cursor != nil) {
		totalFiles, err = browseTotalFileCount(ctx, env, prefix, letter, systems, tags)
		if err != nil {
			return nil, err
		}
	}

	files, next, encErr := paginateFiles(files, maxResults, totalFiles, totalDirs, sort, nil)
	if encErr != nil {
		return nil, encErr
	}
	return buildBrowseResponse(
		env, displayPath, nil, files, maxResults, totalFiles, totalDirs, next, next != nil, systems, tags)
}

// browseSourcePath validates a source root path (one of a host's granted
// media folders, read through a platforms.SourceRootReader) and dispatches it
// through the same dirs-then-files pagination browseFilesystem uses for a
// real OS path: a source root has real nested folders, unlike every other
// virtual scheme. filepath.Clean/Join and browseFilesystem's RootDirs-based
// validation both mangle or do not apply to a "scheme://id/..." path, so this
// validates and builds paths with source-aware helpers instead.
func browseSourcePath(
	env *requests.RequestEnv,
	path string,
	cursor *database.BrowseCursor,
	maxResults int,
	letter *string,
	sort string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) (any, error) {
	// The bare scheme is the aggregated route media.browse's root discovery
	// (BrowseVirtualSchemes) always surfaces for every granted source root
	// combined - not a reference to any one of them, so there is no single
	// root id to validate here. Each root still gets validated on its own
	// below, once a client descends into it specifically.
	if path == platforms.SourceScheme+"://" {
		if _, ok := env.Platform.(platforms.SourceRootReader); !ok {
			return nil, models.ClientErrf("platform does not support source root paths")
		}
		return browsePathPrefix(env, path, path, cursor, maxResults, letter, sort, systems, tags)
	}

	// A client may send either form, same tolerance browseFilesystem gives a
	// real path (cleaned, or cleaned with a trailing slash): segment parsing
	// itself requires no trailing slash, an empty final segment otherwise.
	displayPath := strings.TrimSuffix(path, "/")
	id, _, err := platforms.SourceLocation(displayPath)
	if err != nil {
		return nil, models.ClientErrf("invalid source path: %w", err)
	}
	reader, ok := env.Platform.(platforms.SourceRootReader)
	if !ok {
		return nil, models.ClientErrf("platform does not support source root paths")
	}
	roots, err := reader.SourceRoots(env.Context)
	if err != nil {
		return nil, fmt.Errorf("error listing source roots: %w", err)
	}
	if !slices.Contains(roots, platforms.SourceScheme+"://"+id) {
		return nil, models.ClientErrf("source root is no longer granted")
	}

	return browsePathPrefix(env, displayPath+"/", displayPath, cursor, maxResults, letter, sort, systems, tags)
}

// browseTotalFileCount returns the direct-child file count for a path prefix,
// logging the query timing.
func browseTotalFileCount(
	ctx context.Context,
	env *requests.RequestEnv,
	prefix string,
	letter *string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
) (int, error) {
	started := time.Now()
	count, err := env.Database.MediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
		ExcludeHidden: env.ExcludeHidden,
		PathPrefix:    prefix,
		Letter:        letter,
		Systems:       systems,
		Tags:          tags,
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
	scope *browseCursorScope,
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
	encoded, encErr := encodeFileCursor(
		last.MediaID, sortValue, last.SortMode, totalFiles, totalDirs, scope,
	)
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
	tags []zapscript.TagFilter,
) (any, error) {
	// Validate scheme is known
	if !isKnownVirtualScheme(env, schemePath) {
		return nil, models.ClientErrf("unknown virtual scheme: %s", schemePath)
	}

	ctx := env.Context

	opts := &database.BrowseFilesOptions{
		ExcludeHidden: env.ExcludeHidden,
		PathPrefix:    schemePath,
		Cursor:        cursor,
		Limit:         maxResults + 1,
		Letter:        letter,
		Sort:          sort,
		Systems:       systems,
		Tags:          tags,
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
		totalFiles, err = browseTotalFileCount(ctx, env, schemePath, letter, systems, tags)
		if err != nil {
			return nil, err
		}
	}

	files, next, encErr := paginateFiles(files, maxResults, totalFiles, 0, sort, nil)
	if encErr != nil {
		return nil, encErr
	}
	return buildBrowseResponse(
		env, schemePath, nil, files, maxResults, totalFiles, 0, next, next != nil, systems, tags)
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
	tagSets ...[]zapscript.TagFilter,
) (any, error) {
	var tags []zapscript.TagFilter
	if len(tagSets) > 0 {
		tags = tagSets[0]
	}

	var singletonAliases map[string]database.SingletonContainerAlias
	if len(tags) == 0 {
		singletonAliases = resolveDirSingletonAliases(env, path, dirs, systems)
	}

	entries := make([]models.BrowseEntry, 0, len(dirs)+len(files))
	// Collapsed directories shown with their own folder artwork, whose launch
	// target's cover colour would describe a different image.
	var folderArtEntries []int
	for _, dir := range dirs {
		dirPath := dir.Path
		if dirPath == "" {
			dirPath = browseChildPath(path, dir.Name)
		}
		entry := models.BrowseEntry{
			Name:      browseDirDisplayName(path, dir.Name),
			Path:      dirPath,
			Type:      "directory",
			FileCount: &dir.FileCount,
			SystemIDs: dir.SystemIDs,
			HasCover:  dir.HasCover,
		}
		if alias, ok := singletonAliases[strings.TrimSuffix(dirPath, "/")+"/"]; ok &&
			(!env.ExcludeHidden || !mediaTagsHidden(alias.Tags)) {
			result := database.SearchResultWithCursor{
				MediaID:       alias.Row.DBID,
				SystemID:      alias.Row.System.SystemID,
				Name:          browseMediaDisplayName(alias.Row.Path, alias.Row.SortName, alias.Row.Title.Name),
				Path:          alias.Row.Path,
				Tags:          alias.Tags,
				ZapScriptTags: alias.ZapScriptTags,
				HasCover:      alias.HasCover,
			}
			mediaEntry := buildMediaEntry(&result, env)
			entry.Name = mediaEntry.Name
			entry.MediaID = mediaEntry.MediaID
			entry.SystemID = mediaEntry.SystemID
			entry.RelPath = mediaEntry.RelPath
			entry.ZapScript = mediaEntry.ZapScript
			entry.Tags = mediaEntry.Tags
			entry.DisambiguatingTags = mediaEntry.DisambiguatingTags
			if alias.MultiDisc {
				entry.MultiDisc = true
				entry.DisambiguatingTags = withoutDiscTags(entry.DisambiguatingTags)
			}
			if dir.HasCover {
				folderArtEntries = append(folderArtEntries, len(entries))
			}
			entry.HasCover = entry.HasCover || mediaEntry.HasCover
		} else {
			entry.RelPath = browseDirRelativePath(env, dirPath, dir.SystemIDs, systems)
		}
		if dir.Hidden {
			// Only a listing that includes hidden entries returns the folder,
			// and it says so the way a hidden file does.
			entry.Tags = append(slices.Clip(entry.Tags), hiddenDirectoryTag())
		}
		entries = append(entries, entry)
	}

	for i := range files {
		files[i].Name = mediatags.StripStructuralSetMarkers(files[i].Name)
		entry := buildMediaEntry(&files[i], env)
		entries = append(entries, entry)
	}
	attachBrowseCoverColors(env, entries)
	for _, i := range folderArtEntries {
		entries[i].CoverColor = ""
	}

	var pagination *models.PaginationInfo
	if len(entries) > 0 {
		pagination = &models.PaginationInfo{
			NextCursor:  nextCursor,
			HasNextPage: hasNextPage,
			PageSize:    maxResults,
		}
	}

	var relPath *string
	if path != "" {
		relPath = browseDirRelativePath(env, path, nil, systems)
	}

	return models.BrowseResults{
		Path:       path,
		RelPath:    relPath,
		Entries:    entries,
		Pagination: pagination,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}, nil
}

// groupSingletonAliasCandidates buckets a page's directories by the system each
// one belongs to.
//
// A directory holding media for more than one system is left out, and cannot
// currently be anything else. BrowseDirectoryResult.FileCount is the sum across
// systems, while the resolver counts direct rows one system at a time, so the
// nested-media test it keys on compares a per-system count against a total and
// never balances. Offering the directory anyway would cost a scan to reach the
// same answer. Resolving one properly means carrying per-system counts out of
// the browse query — BrowseDirCounts already stores them per system and the
// query sums them — which is more than the case is worth while a systems filter
// is the normal way to browse: with one, the query narrows SystemIDs to that
// system and the same directory resolves like any other.
func groupSingletonAliasCandidates(
	path string,
	dirs []database.BrowseDirectoryResult,
	systems []systemdefs.System,
) (order []string, bySystem map[string][]database.SingletonAliasCandidate) {
	requested := make(map[string]struct{}, len(systems))
	for _, system := range systems {
		requested[system.ID] = struct{}{}
	}

	bySystem = make(map[string][]database.SingletonAliasCandidate, len(systems))
	for _, dir := range dirs {
		// A directory with no media has nothing to collapse to, and the
		// resolver keys its nested-media test on a non-zero count.
		if dir.FileCount <= 0 {
			continue
		}

		var systemID string
		switch {
		case len(dir.SystemIDs) == 1:
			systemID = dir.SystemIDs[0]
		case len(dir.SystemIDs) == 0 && len(systems) == 1:
			// The media fallback query reports no systems at all, so a
			// single-system request is the only thing that can attribute it.
			systemID = systems[0].ID
		default:
			// Media for several systems, or none that a single-system request
			// could attribute. Neither can be counted per system here.
			continue
		}
		if len(requested) > 0 {
			if _, ok := requested[systemID]; !ok {
				continue
			}
		}

		childDir := dir.Path
		if childDir == "" {
			childDir = browseChildPath(path, dir.Name)
		}
		if _, seen := bySystem[systemID]; !seen {
			order = append(order, systemID)
		}
		bySystem[systemID] = append(bySystem[systemID], database.SingletonAliasCandidate{
			ChildDir:  strings.TrimSuffix(filepath.ToSlash(childDir), "/") + "/",
			FileCount: dir.FileCount,
		})
	}
	return order, bySystem
}

// resolveDirSingletonAliases batch-resolves singleton container aliases for the
// page's candidate directories. Every directory holding media for exactly one
// system is offered to that system's resolver, which reads only each
// candidate's direct rows, so the container rule alone decides what collapses
// and browse agrees with media.meta and the scrapers on a disc folder of any
// size.
//
// An unfiltered page spanning several systems resolves nothing, which is the
// behaviour it has always had rather than a new restriction: the system this
// used to elect for the whole page was abandoned as soon as a second one
// appeared. What changed is that a directory which cannot be attributed no
// longer decides the page, only itself.
//
// The bound stays because of one page. Browsing a media root lists a directory
// per installed system, each with a recursive file count in the hundreds, and
// nothing in BrowseDirectoryResult tells that page apart from a genuine mixed
// page of two disc folders — so grouping it freely would turn the cheapest page
// in the API into a resolver batch per installed system. A client that wants
// aliases across systems names the systems, which bounds the work to what it
// asked for. Lifting the restriction means recognising a system's own launcher
// root, which env.LauncherCache and Platform.RootDirs can both answer; with
// those excluded the media-root page has no candidates and a mixed page becomes
// safe to resolve per system.
func resolveDirSingletonAliases(
	env *requests.RequestEnv,
	path string,
	dirs []database.BrowseDirectoryResult,
	systems []systemdefs.System,
) map[string]database.SingletonContainerAlias {
	if len(dirs) == 0 || env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}

	order, bySystem := groupSingletonAliasCandidates(path, dirs, systems)
	if len(order) == 0 {
		return nil
	}
	if len(systems) == 0 && len(order) > 1 {
		log.Debug().Str("path", path).Int("systems", len(order)).
			Msg("browse singleton alias resolution skipped for unfiltered multi-system page")
		return nil
	}

	started := time.Now()
	var singletonAliases map[string]database.SingletonContainerAlias
	candidateCount := 0
	for _, systemID := range order {
		candidates := bySystem[systemID]
		candidateCount += len(candidates)

		system, sysErr := env.Database.MediaDB.FindSystemBySystemID(systemID)
		if sysErr != nil {
			log.Debug().Err(sysErr).Str("system", systemID).Msg("browse singleton alias system lookup failed")
			continue
		}
		aliases, aliasErr := env.Database.MediaDB.ResolveSingletonContainerAliases(
			env.Context, system.DBID, candidates,
		)
		if aliasErr != nil {
			// One system failing leaves the rest of the page resolvable.
			log.Debug().Err(aliasErr).Str("path", path).Str("system", systemID).
				Msg("browse singleton alias batch resolution failed")
			continue
		}
		if len(aliases) == 0 {
			continue
		}
		aliases = preferLastPlayedDiscs(env, systemID, system.DBID, candidates, aliases)
		if singletonAliases == nil {
			singletonAliases = make(map[string]database.SingletonContainerAlias, len(aliases))
		}
		for i := range aliases {
			singletonAliases[aliases[i].ChildDir] = aliases[i]
		}
	}
	log.Debug().
		Str("path", path).
		Int("systems", len(order)).
		Int("candidates", candidateCount).
		Int("aliases", len(singletonAliases)).
		Dur("duration", time.Since(started)).
		Msg("browse singleton alias resolution timing")
	return singletonAliases
}

// lastPlayedDiscHistoryLimit bounds how far back a multi-disc folder's last
// played disc is looked for: the most recently played distinct media of the
// system. A disc played longer ago than that resolves to the first disc.
const lastPlayedDiscHistoryLimit = 100

// preferLastPlayedDiscs re-resolves the page's multi-disc folders whose most
// recently played disc is not the one the container rule picked, so the entry
// launches the disc the user left off on. Only a page that holds a multi-disc
// folder reads play history, and only folders with a played disc are resolved
// again. Any failure leaves the first-disc aliases in place.
func preferLastPlayedDiscs(
	env *requests.RequestEnv,
	systemID string,
	systemDBID int64,
	candidates []database.SingletonAliasCandidate,
	aliases []database.SingletonContainerAlias,
) []database.SingletonContainerAlias {
	if env.Database.UserDB == nil {
		return aliases
	}
	multiDisc := make(map[string]int)
	for i := range aliases {
		if aliases[i].MultiDisc {
			multiDisc[aliases[i].ChildDir] = i
		}
	}
	if len(multiDisc) == 0 {
		return aliases
	}

	history, err := env.Database.UserDB.GetDistinctMediaHistory(
		env.Context, []string{systemID}, 0, lastPlayedDiscHistoryLimit,
	)
	if err != nil {
		log.Debug().Err(err).Str("system", systemID).Msg("browse last played disc lookup failed")
		return aliases
	}
	// History is newest first, so the first path seen for a folder is the
	// disc last played from it. History keeps the path a launch was given;
	// media paths are canonical.
	lastPlayed := make(map[string]string, len(multiDisc))
	for i := range history {
		mediaPath := pathutil.CanonicalMediaPath(history[i].MediaPath)
		childDir := container.ParentDir(mediaPath)
		if _, ok := multiDisc[childDir]; !ok {
			continue
		}
		if _, seen := lastPlayed[childDir]; !seen {
			lastPlayed[childDir] = mediaPath
		}
	}

	var again []database.SingletonAliasCandidate
	for i := range candidates {
		preferred, ok := lastPlayed[candidates[i].ChildDir]
		if !ok || preferred == aliases[multiDisc[candidates[i].ChildDir]].Row.Path {
			continue
		}
		candidate := candidates[i]
		candidate.PreferredPath = preferred
		again = append(again, candidate)
	}
	if len(again) == 0 {
		return aliases
	}
	preferred, err := env.Database.MediaDB.ResolveSingletonContainerAliases(env.Context, systemDBID, again)
	if err != nil {
		log.Debug().Err(err).Str("system", systemID).Msg("browse last played disc resolution failed")
		return aliases
	}
	for i := range preferred {
		if index, ok := multiDisc[preferred[i].ChildDir]; ok {
			aliases[index] = preferred[i]
		}
	}
	return aliases
}

// browseChildPath builds the next browse path for a child directory one level
// below parent. filepath.Join mangles a source path's "://", so a source
// parent is built with plain string concatenation instead: parent and name
// already share "/" as their only separator either way.
func browseChildPath(parent, name string) string {
	if platforms.IsSourceScheme(parent) {
		return strings.TrimSuffix(parent, "/") + "/" + name
	}
	return filepath.ToSlash(filepath.Join(parent, name))
}

// browseDirDisplayName returns a directory's display name, decoded when it is
// a source path segment: those are stored percent-escaped, the same way a
// multi-segment virtual path's segments always are (virtualpath.
// CreateVirtualPathSegments), since BrowseDirectoryResult.Name otherwise comes
// straight from a raw indexed Path substring. An undecodable name (never
// expected, since every source path is escaped at index time) is shown as-is
// rather than dropped.
func browseDirDisplayName(parentPath, name string) string {
	if !platforms.IsSourceScheme(parentPath) {
		return name
	}
	decoded, err := url.PathUnescape(name)
	if err != nil {
		return name
	}
	return decoded
}

func browseMediaDisplayName(path, sortName, titleName string) string {
	if sortName != "" {
		return mediatags.StripStructuralSetMarkers(sortName)
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

// withoutDiscTags drops disc-number tags from a multi-disc directory's
// disambiguating tags. They tell the launch target apart from its sibling
// discs, which the directory entry stands for as a whole.
func withoutDiscTags(tagInfos []database.TagInfo) []database.TagInfo {
	kept := make([]database.TagInfo, 0, len(tagInfos))
	for _, tag := range tagInfos {
		if tag.Type != string(mediatags.TagTypeDisc) {
			kept = append(kept, tag)
		}
	}
	return kept
}

// attachBrowseCoverColors sets coverColor on the page's media entries and
// collapsed single-game directories with one lookup for the whole page.
func attachBrowseCoverColors(env *requests.RequestEnv, entries []models.BrowseEntry) {
	if env == nil || env.Database == nil {
		return
	}
	ids := make([]int64, 0, len(entries))
	for i := range entries {
		if entries[i].MediaID > 0 {
			ids = append(ids, entries[i].MediaID)
		}
	}
	colors := mediaCoverColors(env.Context, env.Database.MediaDB, ids)
	if len(colors) == 0 {
		return
	}
	for i := range entries {
		entries[i].CoverColor = colors[entries[i].MediaID]
	}
}

// buildMediaEntry converts a SearchResultWithCursor into a BrowseEntry of type "media".
func buildMediaEntry(
	result *database.SearchResultWithCursor,
	env *requests.RequestEnv,
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

	entry.RelPath = mediaResponseRelativePath(env, result.SystemID, result.Path)

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

// browseRootDirs returns the directories a client may browse: the platform's
// scan roots plus the absolute folders launchers declare for themselves. A
// custom launcher's media_dirs need not sit under a scan root, and without this
// its media indexes and launches while the directory holding it cannot be
// listed or opened.
func browseRootDirs(env *requests.RequestEnv) []string {
	var roots []string
	if env.Platform != nil {
		roots = env.Platform.RootDirs(env.Config)
	}
	if env.LauncherCache == nil {
		return roots
	}

	launchers := env.LauncherCache.GetAllLaunchers()
	for i := range launchers {
		for _, folder := range launchers[i].Folders {
			if !filepath.IsAbs(folder) || isPathUnderRoots(folder, roots) {
				continue
			}
			roots = append(roots, folder)
		}
	}
	return roots
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
