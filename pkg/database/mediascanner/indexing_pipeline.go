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
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scummvmnames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/gameid"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	Source       *platforms.MediaSource
	DB           database.MediaDBI
	Path         string
	SystemID     string
	MediaType    slugs.MediaType
	ProvidedName string
	PrefixPolicy browseprefix.Policy
	NoExt        bool
}

// StageMediaPath parses one scanned file into its media fragments and appends
// them to the scanner staging tables. Tags, missing state, and most existence
// checks run set-based in ReconcileStagedSystem once the system's files are
// staged, so scanner memory does not grow with library or database size.
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

	var source *database.ScanStagedSource
	if params.Source != nil {
		var sourceErr error
		source, sourceErr = normalizeScanSource(pf.Path, params.Source)
		if sourceErr != nil {
			log.Warn().Err(sourceErr).Str("system", params.SystemID).Str("path", pf.Path).
				Msg("ignoring invalid media source provenance")
		}
	}
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
		Properties:    stagedPropertiesFromPath(params.DB, params.SystemID, pf.Path),
		Source:        source,
	}
	if err := params.DB.StageScannedMedia(&staged); err != nil {
		return fmt.Errorf("error staging media path %s: %w", pf.Path, err)
	}
	return nil
}

// maxSourceGroupLength bounds a scanner-supplied source group, which comes from
// launcher configuration such as a ScummVM game ID.
const maxSourceGroupLength = 256

func normalizeScanSource(mediaPath string, source *platforms.MediaSource) (*database.ScanStagedSource, error) {
	if !strings.Contains(mediaPath, "://") {
		return nil, errors.New("source provenance requires a virtual media path")
	}
	if source.Kind != platforms.MediaSourceFile && source.Kind != platforms.MediaSourceDirectory {
		return nil, fmt.Errorf("invalid source kind %q", source.Kind)
	}
	if virtualpath.ContainsControlChar(source.Path) || virtualpath.ContainsControlChar(source.Root) {
		return nil, errors.New("source path contains control character")
	}
	if len(source.Group) > maxSourceGroupLength || virtualpath.ContainsControlChar(source.Group) {
		return nil, errors.New("invalid source group")
	}
	path := filepath.Clean(source.Path)
	root := filepath.Clean(source.Root)
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) {
		return nil, errors.New("source path and root must be absolute")
	}
	if filepath.Dir(root) == root {
		return nil, errors.New("source root cannot be a filesystem root")
	}
	if !helpers.PathHasPrefix(path, root) {
		return nil, errors.New("source path is outside source root")
	}
	return &database.ScanStagedSource{
		Path: path, Key: helpers.NormalizePathForComparison(path), Root: root, Kind: string(source.Kind),
		Group: source.Group,
	}, nil
}

func stagedPropertiesFromPath(db database.MediaDBI, systemID, path string) []database.ScanStagedProperty {
	if !gameid.IsCandidate(path, systemID) {
		return nil
	}

	property := string(tags.TagPropertyGameID)
	has, err := db.HasMediaPropertyForPath(context.Background(), systemID, path, property)
	if err != nil {
		log.Warn().Err(err).Str("system", systemID).Str("path", path).Msg("failed to check existing gameid property")
	} else if has {
		log.Debug().Str("system", systemID).Str("path", path).Msg("gameid property already indexed")
		return nil
	}

	started := time.Now()
	log.Debug().Str("system", systemID).Str("path", path).Msg("gameid identification started")
	id, err := gameid.IdentifyPathForSystem(path, systemID)
	duration := time.Since(started)
	if err != nil {
		log.Debug().
			Err(err).
			Str("system", systemID).
			Str("path", path).
			Dur("duration", duration).
			Msg("gameid identification skipped")
		return nil
	}
	if id == "" {
		log.Debug().Str("system", systemID).Str("path", path).Dur("duration", duration).Msg("gameid not found")
		return nil
	}

	log.Debug().Str("system", systemID).Str("path", path).Str("gameid", id).Dur("duration", duration).
		Msg("gameid identified")

	return []database.ScanStagedProperty{{
		Type: string(tags.TagTypeProperty),
		Name: property,
		Text: id,
	}}
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

		switch {
		case platformsshared.IsStandardSchemeForDecoding(scheme):
			// For http/https, extract the extension for tag creation
			// ParseTitleFromFilename will strip it from the display title later
			ext := strings.ToLower(filepath.Ext(f.FileName))
			if helpers.IsValidExtension(ext) {
				f.Ext = ext
			} else {
				f.Ext = ""
			}
		case platformsshared.IsFileBackedScheme(scheme):
			// A file-backed scheme (source) names a file below a folder Core
			// cannot open directly. FilenameFromPath already stripped its
			// extension from f.FileName, as for a filesystem path, so recover
			// it from the path's own leaf.
			ext := strings.ToLower(helpers.GetPathInfo(f.Path).Extension)
			if helpers.IsValidExtension(ext) {
				f.Ext = ext
			} else {
				f.Ext = ""
			}
		default:
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

	// Use pre-resolved media type if provided, otherwise look up from system ID.
	mediaType := params.MediaType
	if mediaType == "" {
		mediaType = slugs.MediaTypeGame // Default to Game
		if params.SystemID != "" {
			if system, err := systemdefs.GetSystem(params.SystemID); err == nil {
				mediaType = system.GetMediaType()
			}
		}
	}

	fileNameForTitle := f.FileName
	tagSource := f.FileName
	trimmedName := strings.TrimSpace(params.ProvidedName)
	// catalogResolved is true once a set or game list has already named the
	// file, so a folder or file name that happens to look like a numbered or
	// dated prefix cannot overwrite it below.
	catalogResolved := false
	if entry, ok := arcadeSet(params.SystemID, f.FileName, f.Ext, trimmedName); ok {
		// A MAME set archive is named by its set ("dkong"), not its game: the
		// title comes from MAME's description. Its variant notes become tags
		// only as far as they name a region, set or revision, so same-title
		// clones stay distinguishable; the catalog adds the year.
		trimmedName = ""
		catalogResolved = true
		fileNameForTitle = entry.Title
		if notes := arcadenames.VariantNotes(entry.Title); notes != "" {
			tagSource += " " + notes
		}
		if len(entry.Year) == 4 && entry.Year[0] >= '1' && entry.Year[0] <= '2' {
			tagSource += " (" + entry.Year + ")"
		}
	}
	if title, ok := scummVMTitle(
		params.SystemID, f.FileName, f.Ext, trimmedName, parentFolder(params.Path),
	); ok {
		// A ScummVM launch file is named by its game ID ("sky"): the title
		// comes from ScummVM's own name for the game.
		trimmedName = ""
		catalogResolved = true
		fileNameForTitle = title
	}
	if trimmedName != "" {
		f.Title = trimmedName
		f.DisplayTitle = trimmedName
	} else {
		prefixPolicy := params.PrefixPolicy
		if !prefixPolicy.Enabled && params.StripLeadingNumbers {
			prefixPolicy = browseprefix.Policy{Kind: browseprefix.KindRank, Enabled: true}
		}
		if stripped, ok := browseprefix.StripWithPolicy(f.FileName, prefixPolicy); ok && !catalogResolved {
			fileNameForTitle = stripped
		}
		f.Title = tags.ParseTitleFromFilenameForMedia(fileNameForTitle, false, mediaType)
		// A directory whose files mostly open with a number gets that prefix
		// stripped, which is right for "01 - Track" and wrong where the number
		// is the whole title: "1942 (W, Rev B)" strips to " (W, Rev B)" and
		// parses to nothing. A nameless title is not cosmetic - it reaches
		// play history, where an account refuses the session and fails the
		// whole upload batch. Keep the unstripped name rather than none.
		if f.Title == "" && fileNameForTitle != f.FileName {
			fileNameForTitle = f.FileName
			f.Title = tags.ParseTitleFromFilenameForMedia(fileNameForTitle, false, mediaType)
		}
		f.DisplayTitle = tags.ParseDisplayTitleFromFilenameForMedia(fileNameForTitle, false, mediaType)
	}
	if f.DisplayTitle == "" {
		f.DisplayTitle = f.Title
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
		f.Tags = getTagsFromFileName(tagSource, mediaType)
	} else {
		f.Tags = []string{}
	}

	return f
}

// scummVMTitle names a ScummVM launch file whose name is its game ID. The
// folder it sits in breaks ties between engines sharing an ID and, for an ID
// the catalog does not know, stands in as the title when it is not just the
// system's own folder. A name the source provided wins unless it is only the
// file's own name again.
func scummVMTitle(systemID, fileName, ext, providedName, folder string) (string, bool) {
	if !scummvmnames.IsTargetFile(systemID, ext) {
		return "", false
	}
	if providedName != "" && !strings.EqualFold(providedName, fileName) &&
		!strings.EqualFold(providedName, fileName+ext) {
		return "", false
	}
	if title, ok := scummvmnames.Title(fileName, folder); ok {
		return title, true
	}
	if !scummvmnames.LooksLikeID(fileName) || folder == "" || strings.EqualFold(folder, systemID) {
		return "", false
	}
	return folder, true
}

// parentFolder is the name of the folder holding a media file, or "" at a
// root: the segment above a source path's last one, or a filesystem path's
// own directory base name.
func parentFolder(mediaPath string) string {
	if _, segments, err := platforms.SourceLocation(mediaPath); err == nil {
		if len(segments) < 2 {
			return ""
		}
		return segments[len(segments)-2]
	}
	if helpers.ReURI.MatchString(mediaPath) {
		return ""
	}
	dir := filepath.Dir(mediaPath)
	if dir == "." || filepath.Dir(dir) == dir {
		return ""
	}
	return filepath.Base(dir)
}

// arcadeSet finds the catalog record for an arcade set archive. A name the
// source provided wins unless it is only the file's own name again.
func arcadeSet(systemID, fileName, ext, providedName string) (arcadenames.Entry, bool) {
	if !arcadenames.IsArcadeSystem(systemID) || !arcadenames.SetArchive(ext) {
		return arcadenames.Entry{}, false
	}
	if providedName != "" && !strings.EqualFold(providedName, fileName) &&
		!strings.EqualFold(providedName, fileName+ext) {
		return arcadenames.Entry{}, false
	}
	return arcadenames.Lookup(fileName)
}
