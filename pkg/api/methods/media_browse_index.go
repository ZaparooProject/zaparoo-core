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
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// Future work for media.browse.index (Phase 1 is what ships here):
//
// Phase 1 (current): buckets are derived from the first character of
// Media.SortName via the shared bucketer (BrowseNameFirstChar /
// browseBucketKeyExpr), giving Latin/numeric buckets A-Z, 0-9, #. Browse uses a
// case-insensitive, punctuation-aware natural collation that keeps those buckets
// contiguous. SortName has no phonetic normalization, so CJK titles all land in
// '#'. The response reports scheme "latin".
//
// Phase 2 (later, Core-side, no client change): populate a phonetic sort key at
// index time and bucket by pinyin initial (Chinese), kana row (Japanese), or
// hangul initial (Korean). Korean is computable from the codepoint; Chinese needs
// a Han->pinyin table; Japanese needs reading (yomi) data and is the hardest.
// The vehicle is a stored column populated in Go at index time. Swapping it in
// touches only the shared bucketer; facet, letter filter, and seek cursor follow.
// Response then reports scheme "pinyin"/"kana"/"hangul"/"mixed".
//
// Forward-compatibility is already baked in so Phase 2 needs no client change:
// scheme and key are opaque, label is separate from key, and jumping is done via
// the opaque per-bucket cursor (never the letter filter), so CJK bucket keys
// never have to widen the A-Z/0-9/# letter-filter vocabulary.
//
// Folders of folders: on CD systems each game is a directory, which
// media.browse returns as a "directory" entry carrying the game's media id. Such
// a folder has no direct files, so the file facet is empty. In that case, and
// only then, the buckets are computed over the scope's directory entries and
// the response says entryType "directory" (BrowseIndex's DirectoryFallback).
// A scope with any direct file keeps the file facet.
//
// Performance notes (measured on MiSTer, ARM32): the facet is a covering-index
// scan over one ParentDir partition plus a transient btree for the folded-bucket
// GROUP BY, ~0.3-0.6s for ~1k-1.4k direct files. The first call into a folder
// also pays resolveBrowseSortMode's prefix-policy path scan (shared with
// media.browse, cached after). If genuinely large *flat* partitions appear, the
// lever is a per-(path,systems,sort) cache tied to the media DB generation
// (DBConfigBrowseIndexVersion), invalidated on reindex — deliberately not added
// yet since real catalogs fold large collections into letter subdirectories.

// HandleMediaBrowseIndex handles the media.browse.index API method. It returns
// the ordered first-character buckets for a browse scope, each with a count and
// a ready-to-use seek cursor, so a client can draw a "jump to letter" rail and
// jump into the full ordered list in one round trip. media.browse itself is
// unchanged: the per-bucket cursor is a normal browse cursor.
func HandleMediaBrowseIndex(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use API param
	log.Debug().Msg("received media browse index request")

	result, err := browseMediaIndex(env)
	if err != nil && errors.Is(err, context.Canceled) {
		// The client navigated away mid-request. Expected and high-volume, so
		// keep it out of Sentry (mirrors HandleMediaBrowse).
		return nil, fmt.Errorf("%w", models.QuietClientErr(err))
	}
	return result, err
}

//nolint:gocritic // Request environment is a per-handler value.
func browseMediaIndex(env requests.RequestEnv) (any, error) {
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
			log.Warn().Err(err).Msg("invalid browse index params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	tagFilters, err := parseBrowseTagFilters(params.Tags)
	if err != nil {
		return nil, err
	}
	env.ExcludeHidden = !filters.IncludesHidden(tagFilters, params.IncludeHidden)

	// media.browse.index never takes a cursor: every call is a fresh request,
	// so a preferences change mid-call always gets one rerun inside Core
	// rather than an error handed back to the client.
	return runBrowseVisibility(&env, nil, func() (any, error) {
		return browseMediaIndexRequest(&env, &params, tagFilters)
	})
}

//nolint:gocritic // Request environment is a per-handler value.
func browseMediaIndexRequest(
	env *requests.RequestEnv, params *models.BrowseParams, tagFilters []zapscript.TagFilter,
) (any, error) {
	var sortOrder string
	if params.Sort != nil {
		sortOrder = *params.Sort
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

	// No path normally means route listing, where a letter rail is not
	// meaningful. The contents root view is an immediate media listing and uses
	// the same ordered physical sources as media.browse.
	if params.Path == nil || *params.Path == "" {
		rootView := browseRootViewRoutes
		if params.RootView != nil {
			rootView = *params.RootView
		}
		if rootView != browseRootViewContents {
			return emptyBrowseIndex(), nil
		}
		if len(systems) != 1 {
			return nil, models.ClientErrf("rootView contents requires exactly one system")
		}
		rootEntries, rootErr := resolveSystemRootEntries(env, systems)
		if rootErr != nil {
			return nil, rootErr
		}
		sources, _ := systemRootContentsSources(env, rootEntries)
		if len(sources) == 0 {
			return emptyBrowseIndex(), nil
		}
		started := time.Now()
		result, indexErr := env.Database.MediaDB.BrowseIndex(env.Context, database.BrowseIndexOptions{
			ExcludeHidden:     env.ExcludeHidden,
			Overlay:           &database.BrowseOverlay{Sources: sources},
			Sort:              sortOrder,
			Systems:           systems,
			Tags:              tagFilters,
			DirectoryFallback: true,
		})
		logBrowseTiming("root_contents_index", "", started, len(result.Buckets))
		if indexErr != nil {
			return nil, fmt.Errorf("error building root contents browse index: %w", indexErr)
		}
		return buildBrowseIndexResponse(&result, &browseCursorScope{
			RootView: browseRootViewContents,
			Sources:  sources,
		})
	}

	path := *params.Path
	if !platforms.IsSourceScheme(path) && !strings.Contains(path, "://") {
		resolved, isRelative, resolveErr := resolveRelativeBrowsePath(env, path, systems)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if isRelative {
			path = resolved
		}
	}
	prefix, err := resolveBrowseIndexPrefix(env, path)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	result, err := env.Database.MediaDB.BrowseIndex(env.Context, database.BrowseIndexOptions{
		ExcludeHidden: env.ExcludeHidden,
		PathPrefix:    prefix,
		Sort:          sortOrder,
		Systems:       systems,
		Tags:          tagFilters,
		// A flat virtual scheme lists no directories (browseVirtual); every
		// other path pages directories ahead of files (browsePathPrefix).
		DirectoryFallback: browsePathListsDirectories(path),
	})
	logBrowseTiming("index", prefix, started, len(result.Buckets))
	if err != nil {
		return nil, fmt.Errorf("error building browse index: %w", err)
	}

	return buildBrowseIndexResponse(&result, nil)
}

// resolveBrowseIndexPrefix validates the requested path and returns the DB path
// prefix to scope the facet by, mirroring the security checks in
// browseFilesystem/browseVirtual.
func resolveBrowseIndexPrefix(env *requests.RequestEnv, path string) (string, error) {
	if platforms.IsSourceScheme(path) {
		return resolveSourceIndexPrefix(env, path)
	}
	if strings.Contains(path, "://") {
		if !isKnownVirtualScheme(env, path) {
			return "", models.ClientErrf("unknown virtual scheme: %s", path)
		}
		return path, nil
	}

	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned != filepath.ToSlash(path) && cleaned+"/" != filepath.ToSlash(path) {
		return "", models.ClientErrf("invalid path: contains disallowed components")
	}

	if !isPathUnderRoots(cleaned, browseRootDirs(env)) {
		return "", models.ClientErrf("path is not within an allowed root directory")
	}

	prefix := cleaned
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix, nil
}

// resolveSourceIndexPrefix is resolveBrowseIndexPrefix's source-root
// counterpart, validated the same way browseSourcePath validates one: the
// root must still be granted, since filepath-based root validation does not
// apply to a "scheme://id/..." path.
func resolveSourceIndexPrefix(env *requests.RequestEnv, path string) (string, error) {
	// Same bare-scheme aggregated route as browseSourcePath: no single root
	// id to validate.
	if path == platforms.SourceScheme+"://" {
		if _, ok := env.Platform.(platforms.SourceRootReader); !ok {
			return "", models.ClientErrf("platform does not support source root paths")
		}
		return path, nil
	}

	// Same trailing-slash tolerance as browseSourcePath: segment parsing
	// itself requires no trailing slash.
	trimmed := strings.TrimSuffix(path, "/")
	id, _, err := platforms.SourceLocation(trimmed)
	if err != nil {
		return "", models.ClientErrf("invalid source path: %w", err)
	}
	reader, ok := env.Platform.(platforms.SourceRootReader)
	if !ok {
		return "", models.ClientErrf("platform does not support source root paths")
	}
	roots, err := reader.SourceRoots(env.Context)
	if err != nil {
		return "", fmt.Errorf("error listing source roots: %w", err)
	}
	if !slices.Contains(roots, platforms.SourceScheme+"://"+id) {
		return "", models.ClientErrf("source root is no longer granted")
	}
	return trimmed + "/", nil
}

// browsePathListsDirectories reports whether media.browse serves path through
// the directories-then-files listing, mirroring the dispatch in
// browseMediaRequest: a source root path and a filesystem path do, a flat
// virtual scheme does not.
func browsePathListsDirectories(path string) bool {
	return platforms.IsSourceScheme(path) || !strings.Contains(path, "://")
}

func emptyBrowseIndex() models.BrowseIndexResults {
	return models.BrowseIndexResults{
		Scheme:    "none",
		EntryType: models.BrowseIndexEntryTypeMedia,
		Groups:    []models.BrowseIndexGroup{},
	}
}

// browseIndexBucketCursor encodes the media.browse cursor that starts a page
// at the bucket. A file bucket seeks the files phase by keyset. A directory
// bucket is the dirs-phase cursor media.browse itself hands out after the
// preceding directory, carrying the same totals, so the page it opens runs on
// through the remaining directories exactly as a paged browse would.
func browseIndexBucketCursor(
	result *database.BrowseIndexResult,
	bucket *database.BrowseIndexBucket,
	scope *browseCursorScope,
) (string, error) {
	if bucket.AtStart {
		return "", nil
	}
	if result.Directories {
		return encodeDirCursor(bucket.AfterDirName, result.TotalFiles, result.TotalDirs, scope)
	}
	return encodeBrowseCursorWithMode(
		bucket.LastID, bucket.SortValue, result.SortMode, result.TotalFiles, scope,
	)
}

func buildBrowseIndexResponse(
	result *database.BrowseIndexResult,
	scope *browseCursorScope,
) (any, error) {
	groups := make([]models.BrowseIndexGroup, 0, len(result.Buckets))
	for i := range result.Buckets {
		bucket := &result.Buckets[i]
		cursor, err := browseIndexBucketCursor(result, bucket, scope)
		if err != nil {
			return nil, fmt.Errorf("failed to encode browse index cursor: %w", err)
		}
		groups = append(groups, models.BrowseIndexGroup{
			Key:    bucket.Key,
			Label:  bucket.Key,
			Cursor: cursor,
			Count:  bucket.Count,
			Offset: bucket.Offset,
		})
	}

	entryType := models.BrowseIndexEntryTypeMedia
	if result.Directories {
		entryType = models.BrowseIndexEntryTypeDirectory
	}
	return models.BrowseIndexResults{
		Scheme:     result.Scheme,
		EntryType:  entryType,
		Groups:     groups,
		TotalFiles: result.TotalFiles,
	}, nil
}
