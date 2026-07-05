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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// scanDynamicTagTypes are the open-ended tag types whose values the scanner may
// create as new Tags rows during reconcile (arbitrary values like "rev:7-2502"
// or an unseen file extension). All other types are restricted to the canonical
// pre-seeded set: staged values with no matching Tags row simply produce no link.
var scanDynamicTagTypes = []string{
	string(tags.TagTypeRev),
	string(tags.TagTypeDeveloper),
	string(tags.TagTypePublisher),
	string(tags.TagTypeCredit),
	string(tags.TagTypeBuildDate),
	string(tags.TagTypeTrack),
	string(tags.TagTypeExtension),
}

// scanReconcileStep is one named statement in the reconcile sequence.
type scanReconcileStep struct {
	step  string
	query string
	args  []any
}

// scanStaleLinkFilter is the shared predicate selecting a staged media's tag
// links that the scanner owns and that are absent from the staged desired set.
// Non-scanner types (user tags, cover/scrape properties, scraper-exclusive
// metadata, scraper run markers) are never treated as stale — deleting them
// here would silently wipe scraped data on every re-index. Must stay in sync
// with sqlGetNonScannerTagDBIDs.
const scanStaleLinkFilter = `
	FROM Media m
	JOIN ScanStage s ON s.Path = m.Path
	JOIN MediaTags mt ON mt.MediaDBID = m.DBID
	JOIN Tags t ON t.DBID = mt.TagDBID
	JOIN TagTypes tt ON tt.DBID = t.TypeDBID
	WHERE m.SystemDBID = ?
	  AND tt.Type NOT IN (?, ?, ?, ?, ?)
	  AND tt.Type NOT LIKE ?
	  AND tt.Type NOT LIKE ?
	  AND NOT EXISTS (
		SELECT 1 FROM ScanStageTags st
		WHERE st.Path = m.Path AND st.TagType = tt.Type AND st.Tag = t.Tag
	  )`

func scanNonScannerTypeArgs(systemDBID int64) []any {
	return []any{
		systemDBID,
		string(tags.TagTypeUser),
		string(tags.TagTypeProperty),
		string(tags.TagTypeRating),
		string(tags.TagTypeGenre),
		string(tags.TagTypeGameFamily),
		string(tags.ScraperType("")) + "%",
		string(tags.ScraperRunType("")) + "%",
	}
}

func sqlErrorIsMissingScanStage(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table: Scan")
}

// sqlEnsureScanStagingTables recreates scanner scratch tables when a copied or
// partially migrated media.db has the scan-staging migration marked applied but
// the tables are absent. They hold no durable user data, so creating missing
// tables is safer than failing an otherwise recoverable index resume.
func sqlEnsureScanStagingTables(ctx context.Context, db sqlQueryable) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ScanStage (
			Path          TEXT PRIMARY KEY,
			ParentDir     TEXT NOT NULL,
			Slug          TEXT NOT NULL,
			TitleName     TEXT NOT NULL,
			SortName      TEXT NOT NULL,
			SlugLength    INTEGER NOT NULL,
			SlugWordCount INTEGER NOT NULL,
			SecondarySlug TEXT
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS scanstage_slug_idx ON ScanStage(Slug)`,
		`CREATE TABLE IF NOT EXISTS ScanStageTags (
			Path    TEXT NOT NULL,
			TagType TEXT NOT NULL,
			Tag     TEXT NOT NULL,
			PRIMARY KEY (Path, TagType, Tag)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS scanstagetags_type_tag_path_idx ON ScanStageTags(TagType, Tag, Path)`,
		`CREATE TABLE IF NOT EXISTS ScanTouchedTitles (
			TitleDBID INTEGER PRIMARY KEY
		) WITHOUT ROWID`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure scan staging schema: %w", err)
		}
	}
	return nil
}

// sqlClearScanStage empties all scanner staging tables. Called before staging a
// system (clearing any rows a crashed run left behind) and after its reconcile.
func sqlClearScanStage(ctx context.Context, db sqlQueryable) error {
	for _, table := range []string{"ScanStageTags", "ScanStage", "ScanTouchedTitles"} {
		start := time.Now()
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			if sqlErrorIsMissingScanStage(err) {
				log.Warn().Err(err).Msg("scan staging tables missing; recreating scratch schema")
				if ensureErr := sqlEnsureScanStagingTables(ctx, db); ensureErr != nil {
					return fmt.Errorf("failed to recreate scan staging tables after missing %s: %w", table, ensureErr)
				}
				return sqlClearScanStage(ctx, db)
			}
			return fmt.Errorf("failed to clear staging table %s: %w", table, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			log.Warn().Str("table", table).Dur("elapsed", elapsed).Msg("scan staging clear took longer than expected")
		}
	}
	return sqlEnsureScanStagingTables(ctx, db)
}

func sqlScanStageCount(ctx context.Context, db sqlQueryable) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ScanStage").Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count staged scan rows: %w", err)
	}
	return count, nil
}

const scanFlagMissingBatchSize = 5000

// scanReconcileExec runs one reconcile statement with a cancellation check first,
// returning the affected row count.
func scanReconcileExec(ctx context.Context, db sqlQueryable, systemID, step, query string, args ...any) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("scan reconcile cancelled before %s: %w", step, err)
	}
	started := time.Now()
	res, err := db.ExecContext(ctx, query, args...)
	elapsed := time.Since(started)
	if err != nil {
		log.Warn().Str("system", systemID).Str("step", step).Dur("elapsed", elapsed).Msg("scan reconcile step failed")
		return 0, fmt.Errorf("scan reconcile %s failed: %w", step, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("scan reconcile %s: failed to read affected rows: %w", step, err)
	}
	logScanReconcileStep(systemID, step, affected, elapsed)
	return affected, nil
}

func logScanReconcileStep(systemID, step string, affected int64, elapsed time.Duration) {
	logEvent := log.Debug()
	if elapsed > 5*time.Second {
		logEvent = log.Warn()
	}
	logEvent.Str("system", systemID).
		Str("step", step).
		Int64("rowsAffected", affected).
		Dur("elapsed", elapsed).
		Msg("scan reconcile step completed")
}

func sqlFlagMissingMedia(ctx context.Context, db sqlQueryable, systemID string, systemDBID int64) (int64, error) {
	const step = "flag missing media"
	totalStart := time.Now()
	totalAffected := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return totalAffected, fmt.Errorf("scan reconcile cancelled before %s: %w", step, err)
		}
		chunkStart := time.Now()
		res, err := db.ExecContext(ctx, `
			WITH missing AS (
				SELECT m.DBID
				FROM Media m
				WHERE m.SystemDBID = ? AND m.IsMissing = 0
				  AND NOT EXISTS (SELECT 1 FROM ScanStage s WHERE s.Path = m.Path)
				LIMIT ?
			)
			UPDATE Media SET IsMissing = 1
			WHERE DBID IN (SELECT DBID FROM missing)`, systemDBID, scanFlagMissingBatchSize)
		chunkElapsed := time.Since(chunkStart)
		if err != nil {
			log.Warn().
				Str("system", systemID).
				Str("step", step).
				Dur("elapsed", chunkElapsed).
				Msg("scan reconcile step failed")
			return totalAffected, fmt.Errorf("scan reconcile %s failed: %w", step, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return totalAffected, fmt.Errorf("scan reconcile %s: failed to read affected rows: %w", step, err)
		}
		totalAffected += affected
		if affected > 0 {
			logEvent := log.Debug()
			if chunkElapsed > 5*time.Second {
				logEvent = log.Warn()
			}
			logEvent.Str("system", systemID).
				Str("step", step).
				Int64("batchRows", affected).
				Int64("rowsAffected", totalAffected).
				Dur("elapsed", chunkElapsed).
				Msg("scan reconcile chunk completed")
		}
		if affected < scanFlagMissingBatchSize {
			break
		}
	}
	logScanReconcileStep(systemID, step, totalAffected, time.Since(totalStart))
	return totalAffected, nil
}

// sqlResolveScanSystem returns the DBID of the system row for systemID, creating
// it when absent and anything is staged. found is false when the system has no
// row and nothing is staged (nothing to reconcile).
func sqlResolveScanSystem(ctx context.Context, db sqlQueryable, systemID string) (dbid int64, found bool, err error) {
	err = db.QueryRowContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?", systemID).Scan(&dbid)
	switch {
	case err == nil:
		return dbid, true, nil
	case errors.Is(err, sql.ErrNoRows):
		staged, countErr := sqlScanStageCount(ctx, db)
		if countErr != nil {
			return 0, false, countErr
		}
		if staged == 0 {
			return 0, false, nil
		}
		res, insErr := db.ExecContext(ctx,
			"INSERT INTO Systems (SystemID, Name) VALUES (?, ?)", systemID, systemID)
		if insErr != nil {
			return 0, false, fmt.Errorf("failed to insert system %s: %w", systemID, insErr)
		}
		dbid, insErr = res.LastInsertId()
		if insErr != nil {
			return 0, false, fmt.Errorf("failed to read inserted system DBID for %s: %w", systemID, insErr)
		}
		return dbid, true, nil
	default:
		return 0, false, fmt.Errorf("failed to resolve system %s: %w", systemID, err)
	}
}

// sqlReconcileStagedSystem folds the staged scan of one system into the media
// tables with set-based statements, so reconcile memory is independent of both
// the library size and the number of existing rows. It must run inside the
// scanner's open transaction (db is the tx) after the staging inserters have
// been flushed. The statement order matters:
//
//  1. titles insert / rename (media upsert joins on them)
//  2. touched-title captures that depend on the PRE-upsert state (new media,
//     title reassignment, missing-state flips)
//  3. media upsert + missing flags
//  4. new dynamic tags, tag-add capture, link insert
//  5. stale-link capture + delete
//  6. disambiguation recompute over the touched set, staging cleared
//
// Reconcile is idempotent: re-running the same staged set is a no-op, which is
// what makes crash-resume of a half-indexed system safe without any preload.
//
// With opts.IncompleteScan, step 3 skips the missing flags (and step 2 skips
// the newly-missing capture): the staged set is known to be a subset of the
// library, so absence from it is not evidence a file is gone.
func sqlReconcileStagedSystem( //nolint:gocognit,funlen // linear statement sequence
	ctx context.Context, db sqlQueryable, systemID string, opts database.ScanReconcileOpts,
) (database.ScanReconcileStats, error) {
	stats := database.ScanReconcileStats{}
	started := time.Now()
	defer func() {
		log.Debug().Str("system", systemID).Dur("elapsed", time.Since(started)).Msg("scan reconcile completed")
	}()
	log.Debug().Str("system", systemID).Bool("incompleteScan", opts.IncompleteScan).Msg("scan reconcile started")

	systemDBID, found, err := sqlResolveScanSystem(ctx, db, systemID)
	if err != nil {
		return stats, err
	}
	if !found {
		return stats, nil
	}
	stats.SystemKnown = true
	stats.SystemDBID = systemDBID

	// New titles: one row per staged slug not yet present for this system. The
	// per-slug representative row is the lowest path, so multi-file titles pick
	// their metadata deterministically.
	stats.TitlesInserted, err = scanReconcileExec(ctx, db, systemID, "insert titles", `
		INSERT INTO MediaTitles (SystemDBID, Slug, Name, SlugLength, SlugWordCount, SecondarySlug)
		SELECT ?, s.Slug, s.TitleName, s.SlugLength, s.SlugWordCount, NULLIF(s.SecondarySlug, '')
		FROM ScanStage s
		WHERE s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = s.Slug)
		  AND NOT EXISTS (
			SELECT 1 FROM MediaTitles t WHERE t.SystemDBID = ? AND t.Slug = s.Slug
		  )`, systemDBID, systemDBID)
	if err != nil {
		return stats, err
	}

	// Refresh canonical names on existing titles when the scan derives a
	// different one (filename cleanup, parser changes).
	stats.TitlesRenamed, err = scanReconcileExec(ctx, db, systemID, "rename titles", `
		UPDATE MediaTitles SET Name = (
			SELECT s.TitleName FROM ScanStage s
			WHERE s.Slug = MediaTitles.Slug
			  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
		)
		WHERE SystemDBID = ?
		  AND Slug IN (SELECT Slug FROM ScanStage)
		  AND Name <> (
			SELECT s.TitleName FROM ScanStage s
			WHERE s.Slug = MediaTitles.Slug
			  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
		  )`, systemDBID)
	if err != nil {
		return stats, err
	}

	// Touched-title captures against the pre-upsert state. Each feeds the
	// disambiguation recompute at the end; INSERT OR IGNORE dedupes across
	// captures. New media touch their (staged) title; a title reassignment
	// touches both the losing and gaining title; a missing-state flip in either
	// direction touches the owning title.
	preUpsertCaptures := []scanReconcileStep{
		{
			step: "capture new media titles",
			query: `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT t.DBID FROM ScanStage s
			JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
			WHERE NOT EXISTS (SELECT 1 FROM Media m WHERE m.SystemDBID = ? AND m.Path = s.Path)`,
			args: []any{systemDBID, systemDBID},
		},
		{
			step: "capture reassigned titles",
			query: `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT m.MediaTitleDBID FROM Media m
			JOIN ScanStage s ON s.Path = m.Path
			JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
			WHERE m.SystemDBID = ? AND m.MediaTitleDBID <> t.DBID`,
			args: []any{systemDBID, systemDBID},
		},
		{
			step: "capture gaining titles",
			query: `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT t.DBID FROM Media m
			JOIN ScanStage s ON s.Path = m.Path
			JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
			WHERE m.SystemDBID = ? AND m.MediaTitleDBID <> t.DBID`,
			args: []any{systemDBID, systemDBID},
		},
		{
			step: "capture re-found titles",
			query: `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT m.MediaTitleDBID FROM Media m
			JOIN ScanStage s ON s.Path = m.Path
			WHERE m.SystemDBID = ? AND m.IsMissing = 1`,
			args: []any{systemDBID},
		},
	}
	if !opts.IncompleteScan {
		preUpsertCaptures = append(preUpsertCaptures, scanReconcileStep{
			step: "capture newly missing titles",
			query: `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT m.MediaTitleDBID FROM Media m
			WHERE m.SystemDBID = ? AND m.IsMissing = 0
			  AND NOT EXISTS (SELECT 1 FROM ScanStage s WHERE s.Path = m.Path)`,
			args: []any{systemDBID},
		})
	}
	for _, capture := range preUpsertCaptures {
		if _, err = scanReconcileExec(ctx, db, systemID, capture.step, capture.query, capture.args...); err != nil {
			return stats, err
		}
	}

	// Media upsert: insert new rows, and update existing rows only when a
	// tracked field actually differs (title reassignment, parent dir move,
	// sort name change, or a missing row re-found on disk).
	stats.MediaUpserted, err = scanReconcileExec(ctx, db, systemID, "upsert media", `
		INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
		SELECT t.DBID, ?, s.Path, s.ParentDir, s.SortName, 0
		FROM ScanStage s
		JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
		WHERE true
		ON CONFLICT (SystemDBID, Path) DO UPDATE SET
			MediaTitleDBID = excluded.MediaTitleDBID,
			ParentDir      = excluded.ParentDir,
			SortName       = excluded.SortName,
			IsMissing      = 0
		WHERE MediaTitleDBID <> excluded.MediaTitleDBID
		   OR ParentDir <> excluded.ParentDir
		   OR SortName <> excluded.SortName
		   OR IsMissing <> 0`, systemDBID, systemDBID)
	if err != nil {
		return stats, err
	}

	// Anything on record for this system but absent from the scan is missing —
	// unless collection errored, in which case absence proves nothing. Chunk the
	// update so pathological path changes (hundreds of thousands of stale rows)
	// report progress and honour cancellation between batches instead of spending
	// minutes in one opaque SQLite statement.
	if !opts.IncompleteScan {
		stats.MediaMissing, err = sqlFlagMissingMedia(ctx, db, systemID, systemDBID)
		if err != nil {
			return stats, err
		}
	}

	// Create Tags rows for staged values of the open-ended types that don't
	// exist yet. Other staged types must match a pre-seeded canonical tag or
	// they produce no link.
	dynamicHolders := prepareVariadic("?", ",", len(scanDynamicTagTypes))
	dynamicArgs := make([]any, len(scanDynamicTagTypes))
	for i, t := range scanDynamicTagTypes {
		dynamicArgs[i] = t
	}
	//nolint:gosec // dynamicHolders is only "?" placeholders.
	stats.TagsInserted, err = scanReconcileExec(ctx, db, systemID, "insert dynamic tags", fmt.Sprintf(`
		INSERT INTO Tags (TypeDBID, Tag)
		SELECT DISTINCT tt.DBID, st.Tag
		FROM ScanStageTags st
		JOIN TagTypes tt ON tt.Type = st.TagType
		WHERE st.TagType IN (%s)
		  AND NOT EXISTS (SELECT 1 FROM Tags t WHERE t.TypeDBID = tt.DBID AND t.Tag = st.Tag)`,
		dynamicHolders), dynamicArgs...)
	if err != nil {
		return stats, err
	}

	// A tag added to an existing media changes its title's disambiguation.
	// Runs after the media upsert so MediaTitleDBID reflects any reassignment.
	if _, err = scanReconcileExec(ctx, db, systemID, "capture tag additions", `
		INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
		SELECT m.MediaTitleDBID
		FROM ScanStageTags st
		JOIN Media m ON m.SystemDBID = ? AND m.Path = st.Path
		JOIN TagTypes tt ON tt.Type = st.TagType
		JOIN Tags t ON t.TypeDBID = tt.DBID AND t.Tag = st.Tag
		WHERE NOT EXISTS (
			SELECT 1 FROM MediaTags mt WHERE mt.MediaDBID = m.DBID AND mt.TagDBID = t.DBID
		)`, systemDBID); err != nil {
		return stats, err
	}

	stats.TagLinksAdded, err = scanReconcileExec(ctx, db, systemID, "insert tag links", `
		INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID)
		SELECT m.DBID, t.DBID
		FROM ScanStageTags st
		JOIN Media m ON m.SystemDBID = ? AND m.Path = st.Path
		JOIN TagTypes tt ON tt.Type = st.TagType
		JOIN Tags t ON t.TypeDBID = tt.DBID AND t.Tag = st.Tag`, systemDBID)
	if err != nil {
		return stats, err
	}

	// Stale scanner-owned links on staged media: capture the owning titles,
	// then delete the links.
	if _, err = scanReconcileExec(ctx, db, systemID, "capture stale tag titles",
		"INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID) SELECT m.MediaTitleDBID"+scanStaleLinkFilter,
		scanNonScannerTypeArgs(systemDBID)...); err != nil {
		return stats, err
	}
	stats.TagLinksDeleted, err = scanReconcileExec(ctx, db, systemID, "delete stale tag links",
		"DELETE FROM MediaTags WHERE (MediaDBID, TagDBID) IN (SELECT mt.MediaDBID, mt.TagDBID"+
			scanStaleLinkFilter+")",
		scanNonScannerTypeArgs(systemDBID)...)
	if err != nil {
		return stats, err
	}

	readTouchedStart := time.Now()
	touched, err := sqlReadScanTouchedTitles(ctx, db)
	readTouchedElapsed := time.Since(readTouchedStart)
	if err != nil {
		return stats, err
	}
	stats.TouchedTitles = int64(len(touched))
	log.Debug().
		Str("system", systemID).
		Int("titleCount", len(touched)).
		Dur("elapsed", readTouchedElapsed).
		Msg("scan reconcile touched titles loaded")
	if len(touched) > 0 {
		if err = ctx.Err(); err != nil {
			return stats, fmt.Errorf("scan reconcile cancelled before disambiguation recompute: %w", err)
		}
		disambiguationStart := time.Now()
		if err = sqlRecomputeTitleDisambiguation(ctx, db, touched); err != nil {
			return stats, fmt.Errorf("scan reconcile disambiguation recompute failed: %w", err)
		}
		disambiguationElapsed := time.Since(disambiguationStart)
		logEvent := log.Debug()
		if disambiguationElapsed > 5*time.Second {
			logEvent = log.Warn()
		}
		logEvent.Str("system", systemID).
			Int("titleCount", len(touched)).
			Dur("elapsed", disambiguationElapsed).
			Msg("scan reconcile disambiguation recompute completed")
	}

	if clearErr := sqlClearScanStage(ctx, db); clearErr != nil {
		return stats, clearErr
	}
	return stats, nil
}

func sqlReadScanTouchedTitles(ctx context.Context, db sqlQueryable) ([]int64, error) {
	rows, err := db.QueryContext(ctx, "SELECT TitleDBID FROM ScanTouchedTitles")
	if err != nil {
		return nil, fmt.Errorf("failed to read touched titles: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("failed to scan touched title DBID: %w", scanErr)
		}
		ids = append(ids, id)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed reading touched titles: %w", rowsErr)
	}
	return ids, nil
}

// sqlSeedCanonicalTags ensures every canonical tag type and value exists,
// set-based: one anti-joined insert for types, then chunked anti-joined inserts
// for values. Replaces the per-row ScanState-driven seeding.
func sqlSeedCanonicalTags(ctx context.Context, db sqlQueryable) error {
	type typeRow struct {
		name        string
		isExclusive bool
	}
	// Dedupe within the statement: the NOT EXISTS anti-join only sees rows
	// already in the table, not other rows of the same INSERT ... SELECT.
	seenTypes := map[string]struct{}{}
	typeRows := make([]typeRow, 0, len(tags.CanonicalTagDefinitions)+2)
	addType := func(tagType tags.TagType) {
		name := string(tagType)
		if _, ok := seenTypes[name]; ok {
			return
		}
		seenTypes[name] = struct{}{}
		typeRows = append(typeRows, typeRow{name, tags.IsExclusiveType(tagType)})
	}
	addType(tags.TagTypeUnknown)
	addType(tags.TagTypeExtension)
	for tagType := range tags.CanonicalTagDefinitions {
		addType(tagType)
	}

	var sb strings.Builder
	args := make([]any, 0, len(typeRows)*2)
	for i, row := range typeRows {
		if i > 0 {
			_, _ = sb.WriteString(",")
		}
		_, _ = sb.WriteString("(?,?)")
		args = append(args, row.name, row.isExclusive)
	}
	//nolint:gosec // Only "(?,?)" placeholder groups are interpolated.
	query := fmt.Sprintf(`
		WITH v(Type, IsExclusive) AS (VALUES %s)
		INSERT INTO TagTypes (Type, IsExclusive)
		SELECT v.Type, v.IsExclusive FROM v
		WHERE NOT EXISTS (SELECT 1 FROM TagTypes tt WHERE tt.Type = v.Type)`, sb.String())
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to seed canonical tag types: %w", err)
	}

	type tagRow struct {
		typeName string
		value    string
	}
	seenTags := map[string]struct{}{}
	tagRows := make([]tagRow, 0, 1400)
	addTag := func(typeName, value string) {
		key := typeName + "\x00" + value
		if _, ok := seenTags[key]; ok {
			return
		}
		seenTags[key] = struct{}{}
		tagRows = append(tagRows, tagRow{typeName: typeName, value: value})
	}
	addTag(string(tags.TagTypeUnknown), "unknown")
	for tagType, values := range tags.CanonicalTagDefinitions {
		for _, value := range values {
			addTag(string(tagType), tags.PadTagValue(strings.ToLower(string(value))))
		}
	}

	const chunkSize = 400
	for start := 0; start < len(tagRows); start += chunkSize {
		end := min(start+chunkSize, len(tagRows))
		chunk := tagRows[start:end]

		sb.Reset()
		args = args[:0]
		for i, row := range chunk {
			if i > 0 {
				_, _ = sb.WriteString(",")
			}
			_, _ = sb.WriteString("(?,?)")
			args = append(args, row.typeName, row.value)
		}
		//nolint:gosec // Only "(?,?)" placeholder groups are interpolated.
		query := fmt.Sprintf(`
			WITH v(Type, Tag) AS (VALUES %s)
			INSERT INTO Tags (TypeDBID, Tag)
			SELECT tt.DBID, v.Tag FROM v
			JOIN TagTypes tt ON tt.Type = v.Type
			WHERE NOT EXISTS (SELECT 1 FROM Tags t WHERE t.TypeDBID = tt.DBID AND t.Tag = v.Tag)`,
			sb.String())
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to seed canonical tags: %w", err)
		}
	}
	return nil
}
