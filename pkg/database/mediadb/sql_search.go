// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	slugsutil "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// fetchAndAttachTags fetches tags for a slice of search results and attaches them to the results.
// This helper consolidates duplicated tag-fetching logic across multiple search functions.
// Uses LEFT JOIN to handle tags with missing TypeDBID defensively.
// If extractYear is true, also extracts 4-digit year tags into the Year field.
// Modifies results in-place.
func fetchAndAttachTags(
	ctx context.Context,
	db *sql.DB,
	results []database.SearchResultWithCursor,
	extractYear bool,
) error {
	if len(results) == 0 {
		return nil
	}

	// Extract media IDs from results
	mediaIDs := make([]int64, len(results))
	for i, result := range results {
		mediaIDs[i] = result.MediaID
	}

	// Query tags for all media IDs using LEFT JOIN for robustness
	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?"
	tagsQuery := `
		SELECT
			MediaTags.MediaDBID,
			Tags.Tag,
			COALESCE(TagTypes.Type, '') as Type
		FROM MediaTags
		INNER JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		LEFT JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID IN (` +
		prepareVariadic("?", ",", len(mediaIDs)) +
		`) ORDER BY MediaTags.MediaDBID, TagTypes.Type, Tags.Tag`

	tagsArgs := make([]any, len(mediaIDs))
	for i, id := range mediaIDs {
		tagsArgs[i] = id
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
			Tag:  tag,
			Type: tagType,
		}
		tagsMap[mediaID] = append(tagsMap[mediaID], tagInfo)
	}
	if err = tagsRows.Err(); err != nil {
		return fmt.Errorf("tags rows iteration error: %w", err)
	}

	// Merge tags into results and optionally extract year tag
	for i := range results {
		if tags, exists := tagsMap[results[i].MediaID]; exists {
			results[i].Tags = tags

			// Extract 4-digit year tag if requested
			if extractYear {
				for _, tag := range tags {
					if tag.Type == "year" && len(tag.Tag) == 4 {
						// Validate it's actually 4 digits
						isYear := true
						for _, ch := range tag.Tag {
							if ch < '0' || ch > '9' {
								isYear = false
								break
							}
						}
						if isYear {
							results[i].Year = &tag.Tag
							break
						}
					}
				}
			}
		} else {
			// Initialize empty tags slice for media with no tags
			results[i].Tags = []database.TagInfo{}
		}
	}

	return nil
}

func sqlSearchMediaPathExact(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	path string,
) ([]database.SearchResult, error) {
	// query == path
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}
	slug := helpers.SlugifyPath(path)

	results := make([]database.SearchResult, 0, 1)
	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	args = append(args, slug, path)

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
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
		`)
		and MediaTitles.Slug = ?
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

func sqlSearchMediaPathParts(
	ctx context.Context,
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
) ([]database.SearchResult, error) {
	results := make([]database.SearchResult, 0, 250)

	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
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
		`)
		and `+
		prepareVariadic(" MediaTitles.Slug like ? ", " and ", len(parts))+
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

	rows, err := stmt.QueryContext(ctx,
		args...,
	)
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
	db *sql.DB,
	systems []systemdefs.System,
	parts []string,
	tags []database.TagFilter,
	letter *string,
	cursor *int64,
	limit int,
	searchByName bool,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, limit)
	if len(systems) == 0 {
		return nil, errors.New("no systems provided for media search")
	}

	// Search for anything in systems on blank query
	if len(parts) == 0 {
		parts = []string{""}
	}

	args := make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}

	// Add cursor condition if provided
	cursorCondition := ""
	if cursor != nil {
		cursorCondition = " AND Media.DBID > ? "
		args = append(args, *cursor)
	}

	tagFilterClauses, tagFilterArgs := BuildTagFilterSQL(tags)
	tagFilterCondition := ""
	if len(tagFilterClauses) > 0 {
		tagFilterCondition = " AND " + strings.Join(tagFilterClauses, " AND ")
	}

	// Add letter filtering condition
	letterFilterCondition := ""
	if letter != nil && *letter != "" {
		letterValue := strings.ToUpper(*letter)
		switch {
		case letterValue == "0-9":
			// Filter for games starting with numbers
			letterFilterCondition = " AND UPPER(SUBSTR(MediaTitles.Name, 1, 1)) BETWEEN '0' AND '9' "
		case letterValue == "#":
			// Filter for games starting with symbols (not letters or numbers)
			letterFilterCondition = " AND UPPER(SUBSTR(MediaTitles.Name, 1, 1)) NOT BETWEEN 'A' AND 'Z' " +
				"AND UPPER(SUBSTR(MediaTitles.Name, 1, 1)) NOT BETWEEN '0' AND '9' "
		case len(letterValue) == 1 && letterValue >= "A" && letterValue <= "Z":
			// Filter for games starting with specific letter
			letterFilterCondition = " AND UPPER(SUBSTR(MediaTitles.Name, 1, 1)) = ? "
			args = append(args, letterValue)
		}
	}

	// Two-query approach to avoid expensive GROUP BY temporary B-trees
	// Query 1: Get media items without tags (fast, no GROUP BY)
	// Search field: use Name for non-Latin queries, Slug for normalized Latin queries
	searchField := "MediaTitles.Slug"
	if searchByName {
		searchField = "MediaTitles.Name"
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
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
		`)
		AND ` +
		prepareVariadic(" "+searchField+" like ? ", " and ", len(parts)) +
		cursorCondition +
		tagFilterCondition +
		letterFilterCondition +
		` LIMIT ?`

	mediaArgs := append([]any(nil), args...)        // Copy args
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

	// Fetch and attach tags for all results (including year extraction)
	if err := fetchAndAttachTags(ctx, db, results, true); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySlug(
	ctx context.Context,
	db *sql.DB,
	systemID string,
	slug string,
	tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)
	// Slugify the input slug to match how slugs are stored in the database
	slugified := slugsutil.SlugifyString(slug)
	args = append(args, systemID, slugified)

	whereConditions := []string{
		"Systems.SystemID = ?",
		"MediaTitles.Slug = ?",
	}

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
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
		LIMIT 50
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
	if err := fetchAndAttachTags(ctx, db, results, false); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySecondarySlug(
	ctx context.Context,
	db *sql.DB,
	systemID string,
	secondarySlug string,
	tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)
	// Slugify the input secondary slug to match how slugs are stored in the database
	slugified := slugsutil.SlugifyString(secondarySlug)
	args = append(args, systemID, slugified)

	whereConditions := []string{
		"Systems.SystemID = ?",
		"MediaTitles.SecondarySlug = ?",
	}

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
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
		LIMIT 50
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
	if err := fetchAndAttachTags(ctx, db, results, false); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySlugPrefix(
	ctx context.Context,
	db *sql.DB,
	systemID string,
	slugPrefix string,
	tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)
	args := make([]any, 0, 2+len(tags)*2)
	// Slugify the input slug prefix to match how slugs are stored in the database
	slugified := slugsutil.SlugifyString(slugPrefix)
	args = append(args, systemID, slugified+"%")

	whereConditions := []string{
		"Systems.SystemID = ?",
		"MediaTitles.Slug LIKE ?",
	}

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
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
		LIMIT 50
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
	if err := fetchAndAttachTags(ctx, db, results, false); err != nil {
		return results, err
	}

	return results, nil
}

func sqlSearchMediaBySlugIn(
	ctx context.Context,
	db *sql.DB,
	systemID string,
	slugList []string,
	tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	results := make([]database.SearchResultWithCursor, 0, 10)

	// Handle empty slugs slice
	if len(slugList) == 0 {
		return results, nil
	}

	// Slugify all input slugs to match how slugs are stored in the database
	slugified := make([]string, 0, len(slugList))
	for _, slug := range slugList {
		s := slugsutil.SlugifyString(slug)
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

	whereConditions := []string{
		"Systems.SystemID = ?",
		"MediaTitles.Slug IN (" + prepareVariadic("?", ",", len(slugified)) + ")",
	}

	tagClauses, tagArgs := BuildTagFilterSQL(tags)
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
		LIMIT 50
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
	if err := fetchAndAttachTags(ctx, db, results, false); err != nil {
		return results, err
	}

	return results, nil
}

func sqlRandomGame(ctx context.Context, db *sql.DB, system *systemdefs.System) (database.SearchResult, error) {
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
	ctx context.Context, db *sql.DB, query *database.MediaQuery,
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
