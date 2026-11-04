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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// Live browse cursor phases. They are distinct from the indexed browse
// phases, so a cursor from one mode is rejected by the other.
const (
	browsePhaseLiveDirs  = "live-dirs"
	browsePhaseLiveFiles = "live-files"
)

// maxLiveBrowseEntries bounds how many directory entries one live browse
// reads. Every page re-reads the directory, so this caps the work and memory
// a single huge folder can cost.
const maxLiveBrowseEntries = 10000

// liveBrowseEntry is one directory entry read from disk that the live browse
// can show.
type liveBrowseEntry struct {
	// name is the entry's file name, unique within the directory and the
	// cursor key.
	name string
	// path is the entry's own path, the one it launches by.
	path string
	// target is the symlink-resolved path, used to match indexed media.
	target   string
	launcher platforms.Launcher
	isDir    bool
}

// displayName is the name shown for an entry: files drop their extension.
func (e *liveBrowseEntry) displayName() string {
	if e.isDir {
		return e.name
	}
	return liveBrowseDisplayName(e.name)
}

func liveBrowseDisplayName(fileName string) string {
	if ext := filepath.Ext(fileName); ext != "" && ext != fileName {
		return strings.TrimSuffix(fileName, ext)
	}
	return fileName
}

// liveBrowseRoots holds the browse roots with symlinks resolved. A live
// browse checks every symlink-resolved path against them, comparing case
// exactly: the lexical root check folds case, which would let a
// differently cased sibling of a root through on a case-sensitive
// filesystem.
type liveBrowseRoots struct {
	resolved []string
}

func newLiveBrowseRoots(roots []string) liveBrowseRoots {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		resolved = append(resolved, realRoot)
	}
	return liveBrowseRoots{resolved: resolved}
}

// find returns the deepest resolved root holding realPath, a path with
// symlinks already resolved, and realPath relative to it.
func (r liveBrowseRoots) find(realPath string) (root, rel string, ok bool) {
	for _, candidate := range r.resolved {
		candidateRel, err := filepath.Rel(candidate, realPath)
		if err != nil || candidateRel == ".." ||
			strings.HasPrefix(candidateRel, ".."+string(filepath.Separator)) {
			continue
		}
		if !ok || len(candidate) > len(root) {
			root, rel, ok = candidate, candidateRel, true
		}
	}
	return root, rel, ok
}

func (r liveBrowseRoots) contains(realPath string) bool {
	_, _, ok := r.find(realPath)
	return ok
}

// liveBrowseSem bounds concurrent live browses. A directory read cannot be
// cancelled, so a stalled mount holds its slot until the read returns. Live
// browses get their own slots so a stalled read never blocks indexed browse.
var liveBrowseSem = make(chan struct{}, 2)

func browseLiveLimited(env *requests.RequestEnv, params *models.BrowseParams) (any, error) {
	endSlot := apidiag.Begin(env.Context, apidiag.ConcurrencySlot)
	defer endSlot()
	select {
	case liveBrowseSem <- struct{}{}:
		defer func() { <-liveBrowseSem }()
	case <-env.Context.Done():
		return nil, fmt.Errorf("live browse cancelled: %w", env.Context.Err())
	}
	endSlot()
	return browseLive(env, params)
}

// browseLive lists a directory straight from disk instead of from MediaDB.
// It serves folders the index never walks, such as MiSTer menu folders of
// MGL files. Folders are listed first, then files a launcher accepts.
// Matching a file to indexed media is optional enrichment: a missing or busy
// database leaves the entry plain rather than failing the page.
func browseLive(env *requests.RequestEnv, params *models.BrowseParams) (any, error) {
	if params.Path == nil || *params.Path == "" {
		return nil, models.ClientErrf("live browse requires a path")
	}
	if params.RootView != nil {
		return nil, models.ClientErrf("live browse does not support rootView")
	}
	if params.Tags != nil && len(*params.Tags) > 0 {
		return nil, models.ClientErrf("live browse does not support tag filters")
	}
	if params.Systems != nil && len(*params.Systems) > 0 {
		return nil, models.ClientErrf("live browse does not support system filters")
	}

	path := *params.Path
	if strings.Contains(path, "://") {
		return nil, models.ClientErrf("live browse requires a filesystem path")
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned != filepath.ToSlash(path) && cleaned+"/" != filepath.ToSlash(path) {
		return nil, models.ClientErrf("invalid path: contains disallowed components")
	}
	rootDirs := browseRootDirs(env)
	if !isPathUnderRoots(cleaned, rootDirs) {
		return nil, models.ClientErrf("path is not within an allowed root directory")
	}
	roots := newLiveBrowseRoots(rootDirs)

	maxResults := defaultMaxResults
	if params.MaxResults != nil && *params.MaxResults > 0 {
		maxResults = *params.MaxResults
	}
	sortMode := ""
	if params.Sort != nil {
		sortMode = *params.Sort
	}
	var letter string
	if params.Letter != nil {
		letter = strings.ToUpper(*params.Letter)
	}
	cursor, err := decodeLiveBrowseCursor(params.Cursor, cleaned, letter, sortMode)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	dirs, files, err := readLiveBrowseDir(env, cleaned, roots)
	logBrowseTiming("live_read", cleaned, started, len(dirs)+len(files))
	if err != nil {
		return nil, err
	}

	// The letter filter targets files, so a letter query lists no folders,
	// matching the indexed browse.
	if letter != "" {
		dirs = nil
		files = slices.DeleteFunc(files, func(e liveBrowseEntry) bool {
			return mediadb.BrowseNameFirstChar(e.displayName()) != letter
		})
	}

	slices.SortFunc(dirs, func(a, b liveBrowseEntry) int {
		return mediadb.CompareBrowseDirectoryNames(a.name, b.name)
	})
	compareFiles := liveBrowseFileOrder(sortMode)
	slices.SortFunc(files, func(a, b liveBrowseEntry) int {
		return compareFiles(a.name, b.name)
	})

	start := 0
	if cursor != nil {
		switch cursor.Phase {
		case browsePhaseLiveDirs:
			start = len(dirs)
			for i := range dirs {
				if mediadb.CompareBrowseDirectoryNames(dirs[i].name, cursor.DirName) > 0 {
					start = i
					break
				}
			}
		case browsePhaseLiveFiles:
			start = len(dirs) + len(files)
			for i := range files {
				if compareFiles(files[i].name, cursor.DirName) > 0 {
					start = len(dirs) + i
					break
				}
			}
		}
	}

	ordered := make([]liveBrowseEntry, 0, len(dirs)+len(files))
	ordered = append(ordered, dirs...)
	ordered = append(ordered, files...)
	end := min(start+maxResults, len(ordered))
	page := ordered[start:end]

	var pagination *models.PaginationInfo
	if end < len(ordered) {
		next, encErr := encodeLiveBrowseCursor(&page[len(page)-1], cleaned, letter, sortMode)
		if encErr != nil {
			return nil, encErr
		}
		pagination = &models.PaginationInfo{
			NextCursor:  &next,
			HasNextPage: true,
			PageSize:    maxResults,
		}
	} else if cursor != nil {
		pagination = &models.PaginationInfo{PageSize: maxResults}
	}

	return models.BrowseResults{
		Path:       cleaned,
		Entries:    buildLiveBrowseEntries(env, page),
		TotalFiles: len(files),
		TotalDirs:  len(dirs),
		Pagination: pagination,
	}, nil
}

// liveBrowseFileOrder returns the file comparator for a sort mode. Name
// modes order by the shown name, falling back to the full file name so the
// order stays total; filename modes order by the full file name.
func liveBrowseFileOrder(sortMode string) func(a, b string) int {
	byName := func(a, b string) int {
		if order := mediadb.CompareBrowseDirectoryNames(
			liveBrowseDisplayName(a), liveBrowseDisplayName(b),
		); order != 0 {
			return order
		}
		return mediadb.CompareBrowseDirectoryNames(a, b)
	}
	switch sortMode {
	case "name-desc":
		return func(a, b string) int { return byName(b, a) }
	case "filename-asc":
		return mediadb.CompareBrowseDirectoryNames
	case "filename-desc":
		return func(a, b string) int { return mediadb.CompareBrowseDirectoryNames(b, a) }
	default:
		return byName
	}
}

// readLiveBrowseDir reads dir and returns the folders and launchable files
// in it. Dot files are hidden, as are symlinks that are broken or resolve
// outside the browse roots.
func readLiveBrowseDir(
	env *requests.RequestEnv, dir string, roots liveBrowseRoots,
) (dirs, files []liveBrowseEntry, err error) {
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, models.ClientErrf("path not found: %s", dir)
		}
		return nil, nil, fmt.Errorf("error resolving browse path: %w", err)
	}
	rootPath, rel, ok := roots.find(realDir)
	if !ok {
		return nil, nil, models.ClientErrf("path is not within an allowed root directory")
	}

	// Open the resolved directory through its root, so a path component
	// swapped for a symlink after the check above cannot lead outside it.
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error opening browse root: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", rootPath).Msg("error closing live browse root")
		}
	}()
	f, err := root.Open(rel)
	if err != nil {
		return nil, nil, fmt.Errorf("error opening browse path: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", dir).Msg("error closing live browse directory")
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("error reading browse path: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, models.ClientErrf("path is not a directory: %s", dir)
	}

	dirEntries, err := f.ReadDir(maxLiveBrowseEntries)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("error reading browse path: %w", err)
	}
	if len(dirEntries) == maxLiveBrowseEntries {
		log.Warn().Str("path", dir).Int("limit", maxLiveBrowseEntries).
			Msg("live browse directory exceeds entry limit; listing truncated")
	}

	var launchers []platforms.Launcher
	if env.LauncherCache != nil {
		launchers = env.LauncherCache.GetAllLaunchers()
	}
	matcher := helpers.NewLauncherMatcherForLaunchers(env.Config, env.Platform, launchers).ForDirectory(dir)

	for _, de := range dirEntries {
		if ctxErr := env.Context.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("live browse cancelled: %w", ctxErr)
		}
		name := de.Name()
		if name == "" || name[0] == '.' {
			continue
		}
		entry := liveBrowseEntry{
			name: name,
			path: filepath.ToSlash(filepath.Join(dir, name)),
		}
		entry.target = entry.path

		mode := de.Type()
		if mode&fs.ModeSymlink != 0 {
			target, linkErr := filepath.EvalSymlinks(entry.path)
			if linkErr != nil {
				continue
			}
			if !roots.contains(target) {
				continue
			}
			target = filepath.ToSlash(target)
			targetInfo, statErr := os.Stat(entry.path)
			if statErr != nil {
				continue
			}
			entry.target = target
			mode = targetInfo.Mode().Type()
		}

		switch {
		case mode.IsDir():
			entry.isDir = true
			dirs = append(dirs, entry)
		case mode.IsRegular():
			launcher, ok := liveBrowseLauncher(env, matcher, entry.path)
			if !ok {
				continue
			}
			entry.launcher = launcher
			files = append(files, entry)
		}
	}
	return dirs, files, nil
}

// liveBrowseLauncher picks the launcher a file would launch with, applying
// the same scan-only resolution and allow-list check as a launch, so a file
// that cannot launch is not listed.
func liveBrowseLauncher(
	env *requests.RequestEnv, matcher *helpers.DirLauncherMatcher, path string,
) (platforms.Launcher, bool) {
	best, ok := matcher.BestLauncher(path)
	if !ok {
		return platforms.Launcher{}, false
	}
	launcher, err := helpers.ResolveLaunchableLauncher(&best, path)
	if err != nil {
		return platforms.Launcher{}, false
	}
	if launcher.AllowListOnly && (env.Config == nil || !env.Config.IsLauncherFileAllowed(path)) {
		return platforms.Launcher{}, false
	}
	return launcher, true
}

// liveMediaMatch is the indexed media row a live file entry resolved to.
type liveMediaMatch struct {
	row      database.MediaPathID
	hasCover bool
}

// buildLiveBrowseEntries turns one page of disk entries into browse entries,
// matching files against MediaDB by their own path or symlink target.
func buildLiveBrowseEntries(env *requests.RequestEnv, page []liveBrowseEntry) []models.BrowseEntry {
	matches := matchLiveBrowseMedia(env, page)
	entries := make([]models.BrowseEntry, 0, len(page))
	for i := range page {
		e := &page[i]
		if e.isDir {
			entries = append(entries, models.BrowseEntry{
				Name: e.name,
				Path: e.path,
				Type: "directory",
			})
			continue
		}

		zapScript := zapscript.Command{
			Name: zapscript.ZapScriptCmdLaunch,
			Args: []string{e.path},
		}.String()
		entry := models.BrowseEntry{
			Name:      e.displayName(),
			Path:      e.path,
			Type:      "media",
			ZapScript: &zapScript,
		}
		systemID := e.launcher.SystemID
		if match, ok := matches[e.path]; ok {
			entry.MediaID = match.row.DBID
			entry.HasCover = match.hasCover
			systemID = match.row.SystemID
		}
		if systemID != "" {
			entry.SystemID = &systemID
			entry.RelPath = mediaResponseRelativePath(env, systemID, e.path)
		}
		entries = append(entries, entry)
	}
	attachBrowseCoverColors(env, entries)
	return entries
}

// matchLiveBrowseMedia resolves each file on the page to an indexed media row
// in one lookup, keyed by the entry's own path. A file's own path wins over
// its symlink target, and a path indexed under more than one system is left
// unmatched because it cannot name one game.
func matchLiveBrowseMedia(env *requests.RequestEnv, page []liveBrowseEntry) map[string]liveMediaMatch {
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}
	var paths []string
	seen := make(map[string]bool)
	add := func(path string) {
		canonical := pathutil.CanonicalMediaPath(path)
		if !seen[canonical] {
			seen[canonical] = true
			paths = append(paths, canonical)
		}
	}
	for i := range page {
		if page[i].isDir {
			continue
		}
		add(page[i].path)
		add(page[i].target)
	}
	if len(paths) == 0 {
		return nil
	}

	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	rows, err := env.Database.MediaDB.FindMediaIDsByPaths(ctx, paths)
	if err != nil {
		log.Debug().Err(err).Msg("could not match live browse entries to indexed media")
		return nil
	}
	byPath := make(map[string]database.MediaPathID, len(rows))
	ambiguous := make(map[string]bool)
	for _, row := range rows {
		if row.DBID <= 0 {
			continue
		}
		if existing, ok := byPath[row.Path]; ok && existing.DBID != row.DBID {
			ambiguous[row.Path] = true
			continue
		}
		byPath[row.Path] = row
	}

	matches := make(map[string]liveMediaMatch)
	var refs []database.MediaRef
	for i := range page {
		if page[i].isDir {
			continue
		}
		for _, candidate := range []string{page[i].path, page[i].target} {
			canonical := pathutil.CanonicalMediaPath(candidate)
			if ambiguous[canonical] {
				break
			}
			row, ok := byPath[canonical]
			if !ok {
				continue
			}
			matches[page[i].path] = liveMediaMatch{row: row}
			refs = append(refs, database.MediaRef{MediaDBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID})
			break
		}
	}
	if len(refs) == 0 {
		return matches
	}

	covers, err := env.Database.MediaDB.GetMediaCoverStatus(ctx, refs)
	if err != nil {
		log.Debug().Err(err).Msg("could not get cover status for live browse entries")
		return matches
	}
	for path, match := range matches {
		match.hasCover = covers[match.row.DBID]
		matches[path] = match
	}
	return matches
}

// encodeLiveBrowseCursor builds the cursor after last. It records the path,
// letter and sort it was built for, since resuming a name keyset under any
// other scope would silently skip entries.
func encodeLiveBrowseCursor(last *liveBrowseEntry, path, letter, sortMode string) (string, error) {
	phase := browsePhaseLiveFiles
	if last.isDir {
		phase = browsePhaseLiveDirs
	}
	return encodeCursorData(&browseCursorData{
		Phase:    phase,
		DirName:  last.name,
		SortMode: sortMode,
		Path:     path,
		Letter:   letter,
	})
}

func decodeLiveBrowseCursor(raw *string, path, letter, sortMode string) (*browseCursorData, error) {
	if raw == nil || *raw == "" {
		return nil, nil //nolint:nilnil // empty cursor is valid
	}
	b, err := base64.StdEncoding.DecodeString(*raw)
	if err != nil {
		return nil, models.ClientErrf("invalid cursor format: %w", err)
	}
	var data browseCursorData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, models.ClientErrf("invalid cursor data: %w", err)
	}
	switch data.Phase {
	case browsePhaseLiveDirs, browsePhaseLiveFiles:
	default:
		return nil, models.ClientErrf("cursor is not a live browse cursor")
	}
	if data.SortMode != sortMode {
		return nil, models.ClientErrf("cursor does not match sort %q", sortMode)
	}
	if data.Path != path {
		return nil, models.ClientErrf("cursor does not match path %q", path)
	}
	if data.Letter != letter {
		return nil, models.ClientErrf("cursor does not match letter %q", letter)
	}
	return &data, nil
}
