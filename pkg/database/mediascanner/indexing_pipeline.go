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
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	platformsshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// PathFragmentParams contains parameters for GetPathFragments.
type PathFragmentParams struct {
	Config              *config.Instance
	Path                string
	SystemID            string
	MediaType           slugs.MediaType
	ProvidedName        string
	PrefixPolicy        browseprefix.Policy
	NoExt               bool
	StripLeadingNumbers bool
}

type MediaPathFragments struct {
	Path         string
	FileName     string
	Title        string
	DisplayTitle string
	Slug         string
	SlugTokens   []string
	Ext          string
	Tags         []string
}

// StageMediaPathParams contains parameters for StageMediaPath.
type StageMediaPathParams struct {
	Config       *config.Instance
	DB           database.MediaDBI
	Path         string
	SystemID     string
	MediaType    slugs.MediaType
	ProvidedName string
	PrefixPolicy browseprefix.Policy
	NoExt        bool
}

// StageMediaPath parses one scanned file into its media fragments and appends
// them to the scanner staging tables. No database reads happen here: existence
// checks, tag diffing, and missing-state all run set-based in
// ReconcileStagedSystem once the system's files are staged, so scanner memory
// does not grow with either library or database size.
func StageMediaPath(params *StageMediaPathParams) error {
	pf := GetPathFragments(&PathFragmentParams{
		Config:       params.Config,
		Path:         params.Path,
		NoExt:        params.NoExt,
		PrefixPolicy: params.PrefixPolicy,
		SystemID:     params.SystemID,
		MediaType:    params.MediaType,
		ProvidedName: params.ProvidedName,
	})

	metadata := mediadb.GenerateSlugMetadataFromTokens(params.MediaType, pf.Title, pf.Slug, pf.SlugTokens)

	staged := database.ScanStagedMedia{
		Path:          pf.Path,
		ParentDir:     mediadb.ParentDirForMediaPath(pf.Path),
		Slug:          pf.Slug,
		TitleName:     pf.Title,
		SortName:      pf.DisplayTitle,
		SecondarySlug: metadata.SecondarySlug,
		SlugLength:    metadata.SlugLength,
		SlugWordCount: metadata.SlugWordCount,
		Tags:          stagedTagsFromFragments(&pf, params.Config),
	}
	if err := params.DB.StageScannedMedia(&staged); err != nil {
		return fmt.Errorf("error staging media path %s: %w", pf.Path, err)
	}
	return nil
}

// stagedTagsFromFragments converts the parsed filename tags (and the extension
// pseudo-tag) into staged type/value pairs. Values are the natural (unpadded)
// form: a filename may spell a numeric segment with or without leading zeros
// ("rev:2" vs "rev:02"); unpadding here and re-padding at the DB write site
// collapses both onto the stored form so reconcile joins match exactly and no
// phantom tag churn marks titles touched on an unchanged re-index.
func stagedTagsFromFragments(pf *MediaPathFragments, cfg *config.Instance) []database.ScanStagedTag {
	staged := make([]database.ScanStagedTag, 0, len(pf.Tags)+1)

	// Extension tag only if filename tags are enabled.
	if pf.Ext != "" && (cfg == nil || cfg.FilenameTags()) {
		staged = append(staged, database.ScanStagedTag{
			Type:  string(tags.TagTypeExtension),
			Value: strings.TrimPrefix(pf.Ext, "."),
		})
	}

	for _, rawTagStr := range pf.Tags {
		tagStr := tags.UnpadTagValue(rawTagStr)
		tagType, tagValue, found := strings.Cut(tagStr, ":")
		if !found || tagType == "" || tagValue == "" {
			log.Trace().Msgf("skipping malformed tag: %s", tagStr)
			continue
		}
		staged = append(staged, database.ScanStagedTag{Type: tagType, Value: tagValue})
	}
	return staged
}

// SeedCanonicalTags seeds the database with canonical GameDataBase-style
// hierarchical tag types and values (e.g. "genre:sports:wrestling",
// "players:2:vs"). Definitions live in the tags package. Runs set-based inside
// its own transaction; already-present rows are left untouched.
func SeedCanonicalTags(ctx context.Context, db database.MediaDBI) error {
	if err := db.BeginTransaction(false); err != nil {
		return fmt.Errorf("failed to begin transaction for seeding tags: %w", err)
	}
	if err := db.SeedCanonicalTagDefinitions(ctx); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction after tag seeding failure")
		}
		return fmt.Errorf("failed to seed canonical tags: %w", err)
	}
	if err := db.CommitTransaction(); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction after commit failure")
		}
		return fmt.Errorf("failed to commit tag seeding transaction: %w", err)
	}
	return nil
}

func getTagsFromFileName(filename string, mediaType slugs.MediaType) []string {
	canonicalStructs := tags.ParseFilenameToCanonicalTagsForMedia(filename, mediaType)

	// Convert CanonicalTag structs to "type:value" format for database compatibility
	canonicalTags := make([]string, 0, len(canonicalStructs))
	for _, ct := range canonicalStructs {
		canonicalTags = append(canonicalTags, ct.String())
	}

	return canonicalTags
}

func GetPathFragments(params *PathFragmentParams) MediaPathFragments {
	f := MediaPathFragments{}

	f.Path = pathutil.CanonicalMediaPath(params.Path)

	// Use FilenameFromPath for virtual paths to get URL-decoded names
	// For regular paths, extract basename manually
	if helpers.ReURI.MatchString(params.Path) {
		// For URIs, FilenameFromPath returns the decoded last path segment, which may include an extension for http/s
		f.FileName = helpers.FilenameFromPath(f.Path)

		// Check the scheme to decide if we should extract an extension
		schemeEnd := strings.Index(f.Path, "://")
		scheme := ""
		if schemeEnd > 0 {
			scheme = strings.ToLower(f.Path[:schemeEnd])
		}

		if platformsshared.IsStandardSchemeForDecoding(scheme) {
			// For http/https, extract the extension for tag creation
			// ParseTitleFromFilename will strip it from the display title later
			ext := strings.ToLower(filepath.Ext(f.FileName))
			if helpers.IsValidExtension(ext) {
				f.Ext = ext
			} else {
				f.Ext = ""
			}
		} else {
			// For custom schemes (steam, kodi, etc.), there is no extension
			f.Ext = ""
		}
	} else {
		fileBase := filepath.Base(f.Path)
		// Skip extension extraction if params.NoExt is true or extract normally
		if params.NoExt {
			f.Ext = ""
		} else {
			f.Ext = strings.ToLower(filepath.Ext(f.Path))
			if !helpers.IsValidExtension(f.Ext) {
				f.Ext = ""
			}
		}
		f.FileName, _ = strings.CutSuffix(fileBase, f.Ext)
	}

	fileNameForTitle := f.FileName
	trimmedName := strings.TrimSpace(params.ProvidedName)
	if trimmedName != "" {
		f.Title = trimmedName
		f.DisplayTitle = trimmedName
	} else {
		prefixPolicy := params.PrefixPolicy
		if !prefixPolicy.Enabled && params.StripLeadingNumbers {
			prefixPolicy = browseprefix.Policy{Kind: browseprefix.KindRank, Enabled: true}
		}
		if stripped, ok := browseprefix.StripWithPolicy(f.FileName, prefixPolicy); ok {
			fileNameForTitle = stripped
		}
		f.Title = tags.ParseTitleFromFilename(fileNameForTitle, false)
		f.DisplayTitle = tags.ParseDisplayTitleFromFilename(fileNameForTitle, false)
	}
	if f.DisplayTitle == "" {
		f.DisplayTitle = f.Title
	}

	// Use pre-resolved media type if provided, otherwise look up from system ID
	mediaType := params.MediaType
	if mediaType == "" {
		mediaType = slugs.MediaTypeGame // Default to Game
		if params.SystemID != "" {
			if system, err := systemdefs.GetSystem(params.SystemID); err == nil {
				mediaType = system.GetMediaType()
			}
		}
	}

	// SlugifyWithTokens computes both slug and tokens in a single pass,
	// avoiding redundant re-slugification in StageMediaPath.
	slugResult := slugs.SlugifyWithTokens(mediaType, f.Title)
	f.Slug = slugResult.Slug
	f.SlugTokens = slugResult.Tokens

	// For non-Latin titles that don't produce a slug, store the lowercase
	// original title. This ensures Slug is never empty while the search
	// logic (mediadb.go) falls back to the Name field for these cases.
	if f.Slug == "" {
		if trimmedName != "" {
			f.Slug = strings.ToLower(trimmedName)
		} else {
			f.Slug = strings.ToLower(fileNameForTitle)
		}
	}

	// Extract tags from filename only if enabled in config (default to enabled for nil config)
	if params.Config == nil || params.Config.FilenameTags() {
		f.Tags = getTagsFromFileName(f.FileName, mediaType)
	} else {
		f.Tags = []string{}
	}

	return f
}
