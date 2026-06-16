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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

// Future work for media.browse.index (Phase 1 is what ships here):
//
// Phase 1 (current): buckets are derived from the first character of
// Media.SortName via the shared bucketer (BrowseNameFirstChar /
// browseBucketKeyExpr), giving Latin/numeric buckets A-Z, 0-9, #. SortName has
// no phonetic normalization, so CJK titles all land in '#' — identical to how
// media.browse already orders them, so no regression. The response reports
// scheme "latin".
//
// Phase 2 (later, Core-side, no client change): populate a normalized,
// case-folded sort key at index time and bucket/sort on it instead of SortName.
// This is the same change for two problems:
//   - Latin: SortName is BINARY-collated, so lowercase-initial titles sort after
//     'Z' and would strand under the wrong bucket. A case-folded key fixes the
//     ordering wrinkle.
//   - CJK: bucket by pinyin initial (Chinese), kana row (Japanese), or hangul
//     initial (Korean). Korean is computable from the codepoint; Chinese needs a
//     Han->pinyin table; Japanese needs reading (yomi) data and is the hardest.
// The vehicle is a *stored* column populated in Go at index time (a generated
// column cannot run the phonetic transforms), indexed like SortName. Swapping it
// in touches only the one shared bucketer; the facet, the letter filter, and the
// seek cursor follow automatically. No data backfill — it fills on the next
// manual reindex; pre-reindex rows fall back to SortName bucketing. The response
// then reports scheme "pinyin"/"kana"/"hangul"/"mixed".
//
// Forward-compatibility is already baked in so Phase 2 needs no client change:
// scheme and key are opaque, label is separate from key, and jumping is done via
// the opaque per-bucket cursor (never the letter filter), so CJK bucket keys
// never have to widen the A-Z/0-9/# letter-filter vocabulary.
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

func browseMediaIndex(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	select {
	case browseSem <- struct{}{}:
		defer func() { <-browseSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}

	var params models.BrowseParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid browse index params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

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

	// No path → root listing. A letter rail is not meaningful for roots.
	if params.Path == nil || *params.Path == "" {
		return emptyBrowseIndex(), nil
	}

	prefix, err := resolveBrowseIndexPrefix(&env, *params.Path)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	result, err := env.Database.MediaDB.BrowseIndex(env.Context, database.BrowseIndexOptions{
		PathPrefix: prefix,
		Sort:       sortOrder,
		Systems:    systems,
	})
	logBrowseTiming("index", prefix, started, len(result.Buckets))
	if err != nil {
		return nil, fmt.Errorf("error building browse index: %w", err)
	}

	return buildBrowseIndexResponse(result)
}

// resolveBrowseIndexPrefix validates the requested path and returns the DB path
// prefix to scope the facet by, mirroring the security checks in
// browseFilesystem/browseVirtual.
func resolveBrowseIndexPrefix(env *requests.RequestEnv, path string) (string, error) {
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

	var rootDirs []string
	if env.Platform != nil {
		rootDirs = env.Platform.RootDirs(env.Config)
	}
	if !isPathUnderRoots(cleaned, rootDirs) {
		return "", models.ClientErrf("path is not within an allowed root directory")
	}

	prefix := cleaned
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix, nil
}

func emptyBrowseIndex() models.BrowseIndexResults {
	return models.BrowseIndexResults{
		Scheme: "none",
		Groups: []models.BrowseIndexGroup{},
	}
}

func buildBrowseIndexResponse(result database.BrowseIndexResult) (any, error) {
	groups := make([]models.BrowseIndexGroup, 0, len(result.Buckets))
	for i := range result.Buckets {
		bucket := &result.Buckets[i]
		var cursor string
		if !bucket.AtStart {
			encoded, err := encodeBrowseCursorWithMode(
				bucket.LastID, bucket.SortValue, result.SortMode, result.TotalFiles,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to encode browse index cursor: %w", err)
			}
			cursor = encoded
		}
		groups = append(groups, models.BrowseIndexGroup{
			Key:    bucket.Key,
			Label:  bucket.Key,
			Cursor: cursor,
			Count:  bucket.Count,
		})
	}

	return models.BrowseIndexResults{
		Scheme:     result.Scheme,
		Groups:     groups,
		TotalFiles: result.TotalFiles,
	}, nil
}
