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

package mediadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	dbtags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// fetchAndAttachTags fetches tags for a slice of search results and attaches them to the results.
// This helper consolidates duplicated tag-fetching logic across multiple search functions.
// Uses UNION of file-level (MediaTags) and title-level (MediaTitleTags) queries for indexable joins.
// Modifies results in-place.
func fetchAndAttachTags(
	ctx context.Context,
	db sqlQueryable,
	results []database.SearchResultWithCursor,
) error {
	if len(results) == 0 {
		return nil
	}

	// Extract media IDs from results
	mediaIDs := make([]int64, len(results))
	for i, result := range results {
		mediaIDs[i] = result.MediaID
	}

	// Query tags for all media IDs from both MediaTags (file-level) and
	// MediaTitleTags (title-level). Uses UNION of two indexed queries
	// instead of an OR JOIN which SQLite can't optimize.
	placeholders := prepareVariadic("?", ",", len(mediaIDs))
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?"
	tagsQuery := `
		SELECT MediaDBID, Tag, Type FROM (
			SELECT
				Media.DBID as MediaDBID,
				Tags.Tag,
				TagTypes.Type
			FROM Media
			JOIN MediaTags ON MediaTags.MediaDBID = Media.DBID
			JOIN Tags ON Tags.DBID = MediaTags.TagDBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE Media.DBID IN (` + placeholders + `)

			UNION

			SELECT
				Media.DBID as MediaDBID,
				Tags.Tag,
				TagTypes.Type
			FROM Media
			JOIN MediaTitleTags ON MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID
			JOIN Tags ON Tags.DBID = MediaTitleTags.TagDBID
			JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
			WHERE Media.DBID IN (` + placeholders + `)
		)
		ORDER BY MediaDBID, Type, Tag`

	// UNION needs the media ID list twice (once per leg)
	tagsArgs := make([]any, 0, len(mediaIDs)*2)
	for _, id := range mediaIDs {
		tagsArgs = append(tagsArgs, id)
	}
	for _, id := range mediaIDs {
		tagsArgs = append(tagsArgs, id)
	}

	tagsStmt, err := db.PrepareContext(ctx, tagsQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare tags query: %w", err)
	}
	defer func() {
		if closeErr := tagsStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tags statement")
		}
	}()

	tagsRows, err := tagsStmt.QueryContext(ctx, tagsArgs...)
	if err != nil {
		return fmt.Errorf("failed to execute tags query: %w", err)
	}
	defer func() {
		if closeErr := tagsRows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tags rows")
		}
	}()

	// Create a map of MediaID -> Tags for fast lookup
	tagsMap := make(map[int64][]database.TagInfo)
	for tagsRows.Next() {
		var mediaID int64
		var tag, tagType string
		if scanErr := tagsRows.Scan(&mediaID, &tag, &tagType); scanErr != nil {
			return fmt.Errorf("failed to scan tags result: %w", scanErr)
		}

		// Append tag to the slice for this media ID
		tagInfo := database.TagInfo{
			Tag:  dbtags.UnpadTagValue(tag),
			Type: tagType,
		}
		tagsMap[mediaID] = append(tagsMap[mediaID], tagInfo)
	}
	if err = tagsRows.Err(); err != nil {
		return fmt.Errorf("tags rows iteration error: %w", err)
	}

	// Merge tags into results
	for i := range results {
		if tagList, exists := tagsMap[results[i].MediaID]; exists {
			results[i].Tags = tagList
		} else {
			// Initialize empty tags slice for media with no tags
			results[i].Tags = []database.TagInfo{}
		}
	}

	// Compute disambiguating ZapScript tags in-memory.
	// A tag type is disambiguating when results sharing the same title name
	// have different values for that tag type (e.g., "2" vs "4" for players).
	computeZapScriptTags(results)

	return nil
}

// computeZapScriptTags determines which tags are disambiguating across sibling
// variants (results with the same Name) and populates ZapScriptTags on each result.
// A tag type is disambiguating when multiple results sharing the same Name have
// different values for that tag type.
//
// KNOWN LIMITATION: This operates on a single page of results, so siblings split
// across pages won't trigger disambiguation here. The app writes the ZapScript
// string from search results directly to tags, so a bare @system/name could be
// written when siblings exist on other pages. In practice this is rare — siblings
// are adjacent in sort order — and the resolver handles bare commands via its
// multi-strategy search. A proper fix would require a DB query per title group.
func computeZapScriptTags(results []database.SearchResultWithCursor) {
	if len(results) == 0 {
		return
	}

	// Group results by SystemID+Name to find siblings (same title within a system)
	nameGroups := make(map[string][]int) // "SystemID/Name" → indices into results
	for i := range results {
		key := results[i].SystemID + "/" + results[i].Name
		nameGroups[key] = append(nameGroups[key], i)
	}

	for _, indices := range nameGroups {
		// For each eligible tag type, collect distinct values across siblings
		disambiguating := make(map[string]bool) // tag types that need disambiguation

		for _, tagType := range database.ZapScriptTagTypes {
			values := make(map[string]bool)
			for _, idx := range indices {
				for _, tag := range results[idx].Tags {
					if tag.Type == tagType {
						values[tag.Tag] = true
					}
				}
			}
			if len(values) > 1 {
				disambiguating[tagType] = true
			}
		}

		// Populate ZapScriptTags with only disambiguating tags for each result
		for _, idx := range indices {
			var zapTags []database.TagInfo
			for _, tag := range results[idx].Tags {
				if disambiguating[tag.Type] {
					zapTags = append(zapTags, tag)
				}
			}
			results[idx].ZapScriptTags = zapTags
			if results[idx].ZapScriptTags == nil {
				results[idx].ZapScriptTags = []database.TagInfo{}
			}
		}
	}
}

func sqlSearchMediaPathExact(
	ctx context.Context,
	db sqlQueryable,
	systems []systemdefs.System,
	path string,
) ([]database.SearchResult, error) {
	// query == path
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	results := make([]database.SearchResult, 0, 1)
	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	args = append(args, path)

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	stmt, err := db.PrepareContext(ctx, `
		select
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (`+
		prepareVariadic("?", ",", len(systems))+
		`)
		and Media.Path = ?
		LIMIT 1
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media path exact search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx,
		args...,
	)
	if err != nil {
		return results, fmt.Errorf("failed to execute media path exact search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Name,
			&result.Path,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaPathParts(
	ctx context.Context,
	db sqlQueryable,
	systems []systemdefs.System,
	variantGroups [][]string,
) ([]database.SearchResult, error) {
	results := make([]database.SearchResult, 0, 250)

	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// Build system ID args
	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}

	// Build AND-of-ORs WHERE clause for variant groups
	groupClauses := make([]string, 0, len(variantGroups))
	variantArgs := make([]any, 0, len(variantGroups)*4)

	for _, variants := range variantGroups {
		if len(variants) == 0 {
			continue
		}

		orConditions := make([]string, 0, len(variants)*2)

		// Add OR conditions for each slug variant
		for _, variant := range variants {
			orConditions = append(orConditions, "MediaTitles.Slug LIKE ?")
			variantArgs = append(variantArgs, "%"+variant+"%")

			// Also search SecondarySlug
			orConditions = append(orConditions, "MediaTitles.SecondarySlug LIKE ?")
			variantArgs = append(variantArgs, "%"+variant+"%")
		}

		// Combine OR conditions for this part into a group
		groupClauses = append(groupClauses, "("+strings.Join(orConditions, " OR ")+")")
	}

	// If no variant groups (shouldn't happen), search for anything
	variantCondition := ""
	if len(groupClauses) > 0 {
		variantCondition = " AND " + strings.Join(groupClauses, " AND ")
	}

	//nolint:gosec // Safe: WHERE clause built from sanitized components
	stmt, err := db.PrepareContext(ctx, `
		select
			Systems.SystemID,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (`+
		prepareVariadic("?", ",", len(systems))+
		`)`+
		variantCondition+
		` LIMIT 250
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media path parts search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	// Assemble final args: systems → variants
	finalArgs := append([]any(nil), args...)      // System IDs
	finalArgs = append(finalArgs, variantArgs...) // Variant args

	rows, err := stmt.QueryContext(ctx, finalArgs...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media path parts search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Path,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		result.Name = helpers.FilenameFromPath(result.Path)
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaWithFilters(
	ctx context.Context,
	db sqlQueryable,
	systems []systemdefs.System,
	variantGroups [][]string,
	rawWords []string,
	tags []zapscript.TagFilter,
	letter *string,
	cursor *int64,
	limit int,
	includeName bool,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, limit)
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// Build system ID args
	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}

	// Build AND-of-ORs WHERE clause for variant groups
	// Each word gets OR'd across its slug variants (and optionally Name)
	// Then all words are AND'd together
	groupClauses := make([]string, 0, len(variantGroups))
	variantArgs := make([]any, 0, len(variantGroups)*4) // Estimate: ~4 variants per word

	for wordIdx, variants := range variantGroups {
		if len(variants) == 0 {
			continue
		}

		orConditions := make([]string, 0, len(variants)*2+1)

		// Add OR conditions for each slug variant
		for _, variant := range variants {
			orConditions = append(orConditions, "MediaTitles.Slug LIKE ?")
			variantArgs = append(variantArgs, "%"+variant+"%")

			// Also search SecondarySlug (already indexed, helps with title decomposition)
			orConditions = append(orConditions, "MediaTitles.SecondarySlug LIKE ?")
			variantArgs = append(variantArgs, "%"+variant+"%")
		}

		// Include Name search for this word if needed (non-Latin or includeName flag)
		if includeName && wordIdx < len(rawWords) && rawWords[wordIdx] != "" {
			orConditions = append(orConditions, "MediaTitles.Name LIKE ?")
			variantArgs = append(variantArgs, "%"+rawWords[wordIdx]+"%")
		}

		// Combine OR conditions for this word into a group
		groupClauses = append(groupClauses, "("+strings.Join(orConditions, " OR ")+")")
	}

	// If no variant groups (empty query), search for anything
	variantCondition := ""
	if len(groupClauses) > 0 {
		variantCondition = " AND " + strings.Join(groupClauses, " AND ")
	}

	// Add cursor condition if provided
	cursorCondition := ""
	if cursor != nil {
		cursorCondition = " AND Media.DBID > ? "
		variantArgs = append(variantArgs, *cursor)
	}

	tagFilterClauses, tagFilterArgs := BuildTagFilterSQL(tags)
	tagFilterCondition := ""
	if len(tagFilterClauses) > 0 {
		tagFilterCondition = " AND " + strings.Join(tagFilterClauses, " AND ")
	}

	// Add letter filtering condition
	letterFilterCondition := ""
	letterClauses, letterArgs := BuildLetterFilterSQL(letter, "MediaTitles.Name")
	if len(letterClauses) > 0 {
		letterFilterCondition = " AND " + strings.Join(letterClauses, " AND ")
		variantArgs = append(variantArgs, letterArgs...)
	}

	//nolint:gosec // Safe: WHERE clause built from sanitized components, no direct user input interpolation
	mediaQuery := `
		SELECT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)` +
		variantCondition +
		cursorCondition +
		tagFilterCondition +
		letterFilterCondition +
		` LIMIT ?`

	// Assemble final args: systems → variants → tag filters → limit
	mediaArgs := append([]any(nil), args...)        // System IDs
	mediaArgs = append(mediaArgs, variantArgs...)   // Variant args (includes cursor, letter if present)
	mediaArgs = append(mediaArgs, tagFilterArgs...) // Add tag filter args
	mediaArgs = append(mediaArgs, limit)

	mediaStmt, err := db.PrepareContext(ctx, mediaQuery)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media query: %w", err)
	}
	defer func() {
		if closeErr := mediaStmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close media statement")
		}
	}()

	mediaRows, err := mediaStmt.QueryContext(ctx, mediaArgs...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media query: %w", err)
	}
	defer func() {
		if closeErr := mediaRows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close media rows")
		}
	}()

	// Collect media items
	for mediaRows.Next() {
		result := database.SearchResultWithCursor{}
		if scanErr := mediaRows.Scan(&result.SystemID, &result.Name, &result.Path, &result.MediaID); scanErr != nil {
			return results, fmt.Errorf("failed to scan media result: %w", scanErr)
		}
		results = append(results, result)
	}
	if err = mediaRows.Err(); err != nil {
		return results, fmt.Errorf("media rows iteration error: %w", err)
	}

	// Fetch and attach tags for all results
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaByTitleDBIDs(
	ctx context.Context,
	db sqlQueryable,
	titleDBIDs []int64,
	tags []zapscript.TagFilter,
	letter *string,
	cursor *int64,
	limit int,
) ([]database.SearchResultWithCursor, error) {
	if len(titleDBIDs) == 0 {
		return []database.SearchResultWithCursor{}, nil
	}

	args := make([]any, 0, len(titleDBIDs)+10)
	for _, id := range titleDBIDs {
		args = append(args, id)
	}
	titleCondition := "MediaTitles.DBID IN (" +
		prepareVariadic("?", ",", len(titleDBIDs)) + ")"

	// Build additional filter conditions
	var extraConditions []string
	var extraArgs []any

	if cursor != nil {
		extraConditions = append(extraConditions, "Media.DBID > ?")
		extraArgs = append(extraArgs, *cursor)
	}

	tagFilterClauses, tagFilterArgs := BuildTagFilterSQL(tags)
	extraConditions = append(extraConditions, tagFilterClauses...)
	extraArgs = append(extraArgs, tagFilterArgs...)

	letterClauses, letterArgs := BuildLetterFilterSQL(letter, "MediaTitles.Name")
	extraConditions = append(extraConditions, letterClauses...)
	extraArgs = append(extraArgs, letterArgs...)

	whereExtra := ""
	if len(extraConditions) > 0 {
		whereExtra = " AND " + strings.Join(extraConditions, " AND ")
	}

	//nolint:gosec // Safe: WHERE clause built from sanitized components
	query := `
		SELECT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID
		FROM MediaTitles
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE ` + titleCondition + whereExtra + `
		ORDER BY Media.DBID ASC
		LIMIT ?`

	finalArgs := make([]any, 0, len(args)+len(extraArgs)+1)
	finalArgs = append(finalArgs, args...)
	finalArgs = append(finalArgs, extraArgs...)
	finalArgs = append(finalArgs, limit)

	rows, err := db.QueryContext(ctx, query, finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query by title DBIDs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]database.SearchResultWithCursor, 0, min(limit, 100))
	for rows.Next() {
		var r database.SearchResultWithCursor
		if scanErr := rows.Scan(&r.SystemID, &r.Name, &r.Path, &r.MediaID); scanErr != nil {
			return nil, fmt.Errorf("failed to scan result: %w", scanErr)
		}
		results = append(results, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return nil, err
	}

	return results, nil
}

func sqlSearchMediaBySlug(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	slug string,
	tags []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)

	// Lookup mediaType for the system to ensure consistent slugification
	mediaType := slugs.MediaTypeGame // Default
	if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
		mediaType = system.GetMediaType()
	}

	// Slugify the input slug to match how slugs are stored in the database
	slugified := slugs.Slugify(mediaType, slug)
	args = append(args, systemID, slugified)

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
	whereConditions := make([]string, 0, 2+len(tagClauses))
	whereConditions = append(whereConditions, "Systems.SystemID = ?", "MediaTitles.Slug = ?")
	whereConditions = append(whereConditions, tagClauses...)
	args = append(args, tagArgs...)

	//nolint:gosec // Safe: all user input goes through parameterized queries
	stmt, err := db.PrepareContext(ctx, `
		SELECT
			DISTINCT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID as MediaID
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE `+strings.Join(whereConditions, " AND ")+`
		ORDER BY MediaTitles.Name
		LIMIT `+strconv.Itoa(defaultSlugSearchLimit)+`
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media by slug search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media by slug search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Name,
			&result.Path,
			&result.MediaID,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return results, err
	}

	// Fetch and attach tags for all results
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySecondarySlug(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	secondarySlug string,
	tags []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)

	// Lookup mediaType for the system to ensure consistent slugification
	mediaType := slugs.MediaTypeGame // Default
	if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
		mediaType = system.GetMediaType()
	}

	// Slugify the input secondary slug to match how slugs are stored in the database
	slugified := slugs.Slugify(mediaType, secondarySlug)
	args = append(args, systemID, slugified)

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
	whereConditions := make([]string, 0, 2+len(tagClauses))
	whereConditions = append(whereConditions, "Systems.SystemID = ?", "MediaTitles.SecondarySlug = ?")
	whereConditions = append(whereConditions, tagClauses...)
	args = append(args, tagArgs...)

	//nolint:gosec // Safe: all user input goes through parameterized queries
	stmt, err := db.PrepareContext(ctx, `
		SELECT
			DISTINCT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID as MediaID
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE `+strings.Join(whereConditions, " AND ")+`
		ORDER BY MediaTitles.Name
		LIMIT `+strconv.Itoa(defaultSlugSearchLimit)+`
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media by secondary slug search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media by secondary slug search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Name,
			&result.Path,
			&result.MediaID,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return results, err
	}

	// Fetch and attach tags for all results
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySlugPrefix(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	slugPrefix string,
	tags []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)

	// Lookup mediaType for the system to ensure consistent slugification
	mediaType := slugs.MediaTypeGame // Default
	if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
		mediaType = system.GetMediaType()
	}

	// Slugify the input slug prefix to match how slugs are stored in the database
	slugified := slugs.Slugify(mediaType, slugPrefix)
	args = append(args, systemID, slugified+"%")

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
	whereConditions := make([]string, 0, 2+len(tagClauses))
	whereConditions = append(whereConditions, "Systems.SystemID = ?", "MediaTitles.Slug LIKE ?")
	whereConditions = append(whereConditions, tagClauses...)
	args = append(args, tagArgs...)

	//nolint:gosec // Safe: all user input goes through parameterized queries
	stmt, err := db.PrepareContext(ctx, `
		SELECT
			DISTINCT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID as MediaID
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE `+strings.Join(whereConditions, " AND ")+`
		ORDER BY MediaTitles.Name
		LIMIT `+strconv.Itoa(defaultSlugSearchLimit)+`
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media by slug prefix search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media by slug prefix search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Name,
			&result.Path,
			&result.MediaID,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return results, err
	}

	// Fetch and attach tags for all results
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySlugIn(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	slugList []string,
	tags []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)

	// Handle empty slugs slice
	if len(slugList) == 0 {
		return results, nil
	}

	// Lookup mediaType for the system to ensure consistent slugification
	mediaType := slugs.MediaTypeGame // Default
	if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
		mediaType = system.GetMediaType()
	}

	// Slugify all input slugs to match how slugs are stored in the database
	slugified := make([]string, 0, len(slugList))
	for _, slug := range slugList {
		s := slugs.Slugify(mediaType, slug)
		if s != "" {
			slugified = append(slugified, s)
		}
	}

	if len(slugified) == 0 {
		return results, nil
	}

	args := make([]any, 0, 1+len(slugified)+len(tags)*2)
	args = append(args, systemID)
	for _, slug := range slugified {
		args = append(args, slug)
	}

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
	whereConditions := make([]string, 0, 2+len(tagClauses))
	whereConditions = append(whereConditions,
		"Systems.SystemID = ?",
		"MediaTitles.Slug IN ("+prepareVariadic("?", ",", len(slugified))+")",
	)
	whereConditions = append(whereConditions, tagClauses...)
	args = append(args, tagArgs...)

	//nolint:gosec // Safe: all user input goes through parameterized queries
	stmt, err := db.PrepareContext(ctx, `
		SELECT
			DISTINCT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID as MediaID
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE `+strings.Join(whereConditions, " AND ")+`
		ORDER BY MediaTitles.Name
		LIMIT `+strconv.Itoa(defaultSlugSearchLimit)+`
	`)
	if err != nil {
		return results, fmt.Errorf("failed to prepare media by slug IN search statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return results, fmt.Errorf("failed to execute media by slug IN search query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		result := database.SearchResultWithCursor{}
		if scanErr := rows.Scan(
			&result.SystemID,
			&result.Name,
			&result.Path,
			&result.MediaID,
		); scanErr != nil {
			return results, fmt.Errorf("failed to scan search result: %w", scanErr)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return results, err
	}

	// Fetch and attach tags for all results
	if err := fetchAndAttachTags(ctx, db, results); err != nil {
		return results, err
	}

	return results, nil
}

// sqlGetRandomMediaForTitle returns a random media entry for the given title DBID.
func sqlGetRandomMediaForTitle(ctx context.Context, db sqlQueryable, titleDBID int64) (database.SearchResult, error) {
	var row database.SearchResult
	err := db.QueryRowContext(ctx, `
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Media.MediaTitleDBID = ?
		ORDER BY RANDOM() LIMIT 1
	`, titleDBID).Scan(&row.SystemID, &row.Path)
	if err != nil {
		return row, fmt.Errorf("failed to get random media for title %d: %w", titleDBID, err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, nil
}

func sqlRandomGame(ctx context.Context, db sqlQueryable, system *systemdefs.System) (database.SearchResult, error) {
	var row database.SearchResult

	// Step 1: Get count, min DBID, and max DBID for this system
	statsQuery := `
		SELECT COUNT(*), COALESCE(MIN(Media.DBID), 0), COALESCE(MAX(Media.DBID), 0)
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Systems.SystemID = ?
	`
	var count int
	var minDBID, maxDBID int64
	err := db.QueryRowContext(ctx, statsQuery, system.ID).Scan(&count, &minDBID, &maxDBID)
	if err != nil {
		return row, fmt.Errorf("failed to get media stats for system: %w", err)
	}

	if count == 0 {
		return row, sql.ErrNoRows
	}

	// Step 2: Generate random DBID within the range
	// This approach is O(log n) instead of O(n) for OFFSET
	randomOffset, err := helpers.RandomInt(int(maxDBID - minDBID + 1))
	if err != nil {
		return row, fmt.Errorf("failed to generate random DBID offset: %w", err)
	}
	targetDBID := minDBID + int64(randomOffset)

	// Step 3: Get the first media item with DBID >= targetDBID
	selectQuery := `
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		WHERE Systems.SystemID = ? AND Media.DBID >= ?
		ORDER BY Media.DBID ASC
		LIMIT 1
	`
	err = db.QueryRowContext(ctx, selectQuery, system.ID, targetDBID).Scan(
		&row.SystemID,
		&row.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// If no row found >= targetDBID (gap in DBID sequence), try wrapping to beginning
		selectQuery = `
			SELECT Systems.SystemID, Media.Path
			FROM Media
			INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
			INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
			WHERE Systems.SystemID = ? AND Media.DBID < ?
			ORDER BY Media.DBID DESC
			LIMIT 1
		`
		err = db.QueryRowContext(ctx, selectQuery, system.ID, targetDBID).Scan(
			&row.SystemID,
			&row.Path,
		)
	}
	if err != nil {
		return row, fmt.Errorf("failed to scan random game row using DBID approach: %w", err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, nil
}

// sqlRandomGameWithQueryAndStats returns a random game matching the query along with the computed statistics.
func sqlRandomGameWithQueryAndStats(
	ctx context.Context, db sqlQueryable, query *database.MediaQuery,
) (database.SearchResult, MediaStats, error) {
	var row database.SearchResult
	var stats MediaStats

	// Use shared helper to build WHERE clause and arguments
	whereClause, args := buildMediaQueryWhereClause(query)

	// Step 1: Get count, min DBID, and max DBID for this query
	//nolint:gosec // whereClause is built from safe conditions, no user input
	statsQuery := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(MIN(Media.DBID), 0), COALESCE(MAX(Media.DBID), 0)
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		%s
	`, whereClause)

	err := db.QueryRowContext(ctx, statsQuery, args...).Scan(&stats.Count, &stats.MinDBID, &stats.MaxDBID)
	if err != nil {
		return row, stats, fmt.Errorf("failed to get media stats for query: %w", err)
	}

	if stats.Count == 0 {
		return row, stats, sql.ErrNoRows
	}

	// Step 2: Generate random DBID within the range
	randomOffset, err := helpers.RandomInt(int(stats.MaxDBID - stats.MinDBID + 1))
	if err != nil {
		return row, stats, fmt.Errorf("failed to generate random DBID offset: %w", err)
	}
	targetDBID := stats.MinDBID + int64(randomOffset)

	// Step 3: Get the first media item with DBID >= targetDBID
	//nolint:gosec // whereClause is built from safe conditions, no user input
	selectQuery := fmt.Sprintf(`
		SELECT Systems.SystemID, Media.Path
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
		%s AND Media.DBID >= ?
		ORDER BY Media.DBID ASC
		LIMIT 1
	`, whereClause)

	args = append(args, targetDBID)
	err = db.QueryRowContext(ctx, selectQuery, args...).Scan(
		&row.SystemID,
		&row.Path,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// If no row found >= targetDBID (gap in DBID sequence), try wrapping to beginning
		selectQuery = fmt.Sprintf(`
			SELECT Systems.SystemID, Media.Path
			FROM Media
			INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
			INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
			%s AND Media.DBID < ?
			ORDER BY Media.DBID DESC
			LIMIT 1
		`, whereClause)
		args[len(args)-1] = targetDBID
		err = db.QueryRowContext(ctx, selectQuery, args...).Scan(
			&row.SystemID,
			&row.Path,
		)
	}
	if err != nil {
		return row, stats, fmt.Errorf("failed to scan random game row with query: %w", err)
	}
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, stats, nil
}
