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
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// browseListingKeySeparator joins a key's list values. It cannot appear in a
// system ID, a tag, or an indexed path.
const browseListingKeySeparator = "\x00"

// browseListingScope is what a listing lists, as the listing functions know it.
type browseListingScope struct {
	prefix           string
	sources          []database.BrowseSource
	merged           bool
	listsDirectories bool
}

func (s *browseListingScope) overlay() *database.BrowseOverlay {
	if !s.merged {
		return nil
	}
	return &database.BrowseOverlay{Sources: s.sources}
}

// browsePosition is one media.browse request's place in its listing. A listing
// function calls resolve before it reads, which turns a request by position into
// a cursor carrying the phase and the totals, so the listing's ordinary phases
// serve the page, encode its next cursor and cross from directories to files.
// Only dirOffset and fileOffset, and skipping the recounts when positioned, tell
// such a page from one reached by cursor. It belongs to one run of a request,
// since a visibility rerun has to start over.
type browsePosition struct {
	cache       *database.BrowseListingCache
	offset      *int
	anchorPath  *string
	anchorIndex *int
	sort        string
	revision    string
	systems     []systemdefs.System
	tags        []zapscript.TagFilter
	key         database.BrowseListingKey
	ticket      uint64
	maxResults  int
	start       int
	dirOffset   int
	fileOffset  int
	// tracked marks a page that reports where it starts: a cursorless page with
	// no letter. listed says the request reached a listing, not the route list.
	tracked    bool
	listed     bool
	positioned bool
}

// validateBrowsePosition rejects what a page by position cannot be combined with.
// Reading a position needs a whole listing in its own order, which a cursor, a
// letter filter and a live read of the disk each take away.
func validateBrowsePosition(params *models.BrowseParams) error {
	var name string
	switch {
	case params.Offset != nil:
		name = "offset"
	case params.AnchorPath != nil:
		name = "anchorPath"
	default:
		return nil
	}
	if params.Offset != nil && params.AnchorPath != nil {
		return models.ClientErrf("offset cannot be combined with anchorPath")
	}
	if params.Cursor != nil && *params.Cursor != "" {
		return models.ClientErrf("%s cannot be combined with cursor", name)
	}
	if params.Letter != nil {
		return models.ClientErrf("%s cannot be combined with letter", name)
	}
	if params.Live {
		return models.ClientErrf("%s cannot be combined with live", name)
	}
	if (params.Path == nil || *params.Path == "") &&
		(params.RootView == nil || *params.RootView != browseRootViewContents) {
		return models.ClientErrf("%s requires a path or rootView contents", name)
	}
	return nil
}

func newBrowsePosition(
	params *models.BrowseParams,
	cursor *database.BrowseCursor,
	revision, sortOrder string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
	maxResults int,
) *browsePosition {
	return &browsePosition{
		offset:     params.Offset,
		anchorPath: params.AnchorPath,
		revision:   revision,
		sort:       sortOrder,
		systems:    systems,
		tags:       tags,
		maxResults: maxResults,
		tracked:    cursor == nil && params.Letter == nil,
	}
}

// browseListingKey names a listing in the media database's BrowseListingCache,
// so media.browse and media.browse.index share an entry.
func browseListingKey(
	scope *browseListingScope,
	sortOrder string,
	systems []systemdefs.System,
	tags []zapscript.TagFilter,
	revision string,
	excludeHidden bool,
) database.BrowseListingKey {
	if sortOrder == "" {
		sortOrder = "name-asc"
	}
	key := database.BrowseListingKey{
		Path:          scope.prefix,
		Sort:          sortOrder,
		Revision:      revision,
		IncludeHidden: !excludeHidden,
	}
	if scope.merged {
		key.RootView = browseRootViewContents
		parts := make([]string, 0, len(scope.sources))
		for i := range scope.sources {
			parts = append(parts, fmt.Sprintf("%s|%t", scope.sources[i].PathPrefix, scope.sources[i].IncludeDirs))
		}
		key.Sources = strings.Join(parts, browseListingKeySeparator)
	}
	systemIDs := make([]string, 0, len(systems))
	for i := range systems {
		systemIDs = append(systemIDs, systems[i].ID)
	}
	key.Systems = strings.Join(systemIDs, browseListingKeySeparator)
	tagKeys := make([]string, 0, len(tags))
	for i := range tags {
		tagKeys = append(tagKeys, fmt.Sprintf("%v|%s|%s", tags[i].Operator, tags[i].Type, tags[i].Value))
	}
	key.Tags = strings.Join(tagKeys, browseListingKeySeparator)
	return key
}

// cachedBrowseIndex returns a listing's letter index, building it once.
func cachedBrowseIndex(
	env *requests.RequestEnv,
	key *database.BrowseListingKey,
	revision string,
	opts *database.BrowseIndexOptions,
) (database.BrowseIndexResult, error) {
	cache := env.Database.MediaDB.BrowseListingCache()
	ticket := cache.Begin(revision)
	if cached, ok := cache.Index(key); ok {
		return cached, nil
	}
	result, err := env.Database.MediaDB.BrowseIndex(env.Context, *opts)
	if err != nil {
		return result, fmt.Errorf("browse index: %w", err)
	}
	cache.StoreIndex(key, ticket, &result)
	return result, nil
}

// resolve places the request in the listing. It returns the cursor the listing
// runs with, which is the one it was given unless the request is by position and
// the position was found. An anchor the listing does not show leaves a plain
// first page, with no error.
func (p *browsePosition) resolve(
	env *requests.RequestEnv, scope *browseListingScope, cursor *database.BrowseCursor,
) (*database.BrowseCursor, error) {
	if !p.tracked {
		return cursor, nil
	}
	p.listed = true
	p.key = browseListingKey(scope, p.sort, p.systems, p.tags, p.revision, env.ExcludeHidden)
	p.cache = env.Database.MediaDB.BrowseListingCache()
	p.ticket = p.cache.Begin(p.revision)

	var anchor database.BrowseAnchorResult
	switch {
	case p.anchorPath != nil:
		// With no routes there is nothing to look in, and an empty overlay does
		// not mean the same thing to the database as no overlay.
		if scope.merged && len(scope.sources) == 0 {
			return cursor, nil
		}
		started := time.Now()
		var err error
		anchor, err = env.Database.MediaDB.BrowseAnchorPosition(env.Context, &database.BrowseAnchorOptions{
			ExcludeHidden:   env.ExcludeHidden,
			Overlay:         scope.overlay(),
			PathPrefix:      scope.prefix,
			Path:            filepath.ToSlash(*p.anchorPath),
			Sort:            p.sort,
			Systems:         p.systems,
			Tags:            p.tags,
			ListDirectories: scope.listsDirectories,
		})
		logBrowseTiming("anchor", scope.prefix, started, anchor.Position)
		if err != nil {
			return nil, fmt.Errorf("error locating browse anchor: %w", err)
		}
		if !anchor.Found {
			return cursor, nil
		}
	case p.offset == nil:
		return cursor, nil
	}

	totals, err := p.totals(env, scope)
	if err != nil {
		return nil, err
	}

	if p.anchorPath != nil {
		index := anchor.Position
		if !anchor.IsDirectory {
			index += totals.Dirs
		}
		p.anchorIndex = &index
		p.start = index / p.maxResults * p.maxResults
	} else {
		p.start = *p.offset
	}
	p.positioned = true

	if p.start < totals.Dirs {
		p.dirOffset = p.start
		return &database.BrowseCursor{
			Phase: browsePhaseDirs, TotalDirs: totals.Dirs, TotalFiles: totals.Files,
		}, nil
	}
	// Clamped so a position far past the end cannot ask SQLite to walk further
	// than the listing goes, or overflow a branch's limit.
	p.fileOffset = min(p.start-totals.Dirs, totals.Files)
	return &database.BrowseCursor{
		Phase: browsePhaseFiles, TotalDirs: totals.Dirs, TotalFiles: totals.Files,
	}, nil
}

// totals reuses the counts of an earlier page of the listing. The count over a
// large merged listing is the slow part of a page, so it only runs when no page
// has reported the totals yet.
func (p *browsePosition) totals(
	env *requests.RequestEnv, scope *browseListingScope,
) (database.BrowseListingTotals, error) {
	if totals, ok := p.cache.Totals(&p.key); ok {
		return totals, nil
	}
	if scope.merged && len(scope.sources) == 0 {
		return database.BrowseListingTotals{}, nil
	}

	var totals database.BrowseListingTotals
	var err error
	if scope.listsDirectories {
		started := time.Now()
		totals.Dirs, err = env.Database.MediaDB.BrowseDirCount(env.Context, database.BrowseDirCountOptions{
			ExcludeHidden: env.ExcludeHidden,
			PathPrefix:    scope.prefix,
			Overlay:       scope.overlay(),
			Systems:       p.systems,
		})
		logBrowseTiming("position_dir_count", scope.prefix, started, totals.Dirs)
		if err != nil {
			return totals, fmt.Errorf("error getting directory count: %w", err)
		}
	}
	started := time.Now()
	totals.Files, err = env.Database.MediaDB.BrowseFileCount(env.Context, database.BrowseFileCountOptions{
		ExcludeHidden: env.ExcludeHidden,
		PathPrefix:    scope.prefix,
		Overlay:       scope.overlay(),
		Systems:       p.systems,
		Tags:          p.tags,
	})
	logBrowseTiming("position_file_count", scope.prefix, started, totals.Files)
	if err != nil {
		return totals, fmt.Errorf("error getting file count: %w", err)
	}
	p.cache.StoreTotals(&p.key, p.ticket, totals)
	return totals, nil
}

// finish reports the position on the finished page. A first page gives the
// listing its cached totals. A page by position always carries pagination, even
// past the end of the listing, because the client asked where it landed.
func (p *browsePosition) finish(result any, err error) (any, error) {
	if err != nil || !p.listed {
		return result, err
	}
	response, ok := result.(models.BrowseResults)
	if !ok {
		return result, nil
	}
	if !p.positioned {
		p.cache.StoreTotals(&p.key, p.ticket, database.BrowseListingTotals{
			Dirs:  response.TotalDirs,
			Files: response.TotalFiles,
		})
		if response.Pagination == nil {
			response.Pagination = &models.PaginationInfo{PageSize: p.maxResults}
		}
		first := 0
		response.Pagination.Offset = &first
		return response, nil
	}
	if response.Pagination == nil {
		response.Pagination = &models.PaginationInfo{PageSize: p.maxResults}
	}
	if response.Entries == nil {
		response.Entries = []models.BrowseEntry{}
	}
	start := p.start
	response.Pagination.Offset = &start
	response.Pagination.AnchorIndex = p.anchorIndex
	return response, nil
}
