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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	string(tags.TagTypePatch),
	string(tags.TagTypeTrack),
	string(tags.TagTypeExtension),
}

// scanReconcileStep is one named statement in the reconcile sequence.
type scanReconcileStep struct {
	step  string
	query string
	args  []any
}

type canonicalTypeRow struct {
	name        string
	isExclusive bool
}

type canonicalTagRow struct {
	typeName string
	value    string
}

// scanStaleLinkFilter is the shared predicate selecting a staged media's tag
// links that the scanner owns and that are absent from the staged desired set.
// Non-scanner types (user tags, cover/scrape properties, scraper-exclusive
// metadata, scraper run markers) are never treated as stale — deleting them
// here would silently wipe scraped data on every re-index. Must stay in sync
// with sqlGetNonScannerTagDBIDs.
const (
	scanFlagMissingBatchSize = 5000
	// A row here writes to roughly nine Media indexes (vs. scanFlagMissingBatchSize's
	// single-column flip on rows an existing index already located), so a smaller
	// chunk keeps a chunk under logScanReconcileStep's 5s warn threshold at the
	// ~1.1-1.6ms/row rate seen at every observed system scale — see
	// sqlUpsertStagedMedia and #1279.
	scanUpsertMediaBatchSize = 2000
	// Title-scoped disambiguation performs poorly on large media databases even
	// for modest touched sets because SQLite repeatedly plans around a long ID
	// scope. During scan reconcile, recomputing the current system in one pass is
	// faster and still bounded; keep single-title updates scoped for tiny changes.
	scanSystemDisambiguationThreshold = 1
	// #1279: on a 66k-file system this filter (used by "capture stale tag
	// titles" and "delete stale tag links") and "capture tag additions" cost
	// 205s combined finding zero rows. EXPLAIN QUERY PLAN at 50k synthetic
	// scale (pkg/database/mediascanner/reconcile_query_plan_test.go,
	// ZAPAROO_RECONCILE_EQP=1) shows both drive from Media filtered by an
	// indexed SystemDBID search, not an unindexed full scan — so the cost is
	// the query's shape, not a missing index: it must touch every existing
	// (media, tag) pair for the system to prove none are stale, which is
	// O(existing corpus) regardless of what changed. sqlite_stat1 is also
	// stale for ScanStage/ScanStageTags mid-reconcile (they're cleared and
	// repopulated every system, so ANALYZE never captures them), which likely
	// affects join order elsewhere too — see the upsert media CROSS JOIN
	// below for a case where that mattered. Candidate direction for a future
	// round: start from ScanStageTags/ScanStage (bounded to what's staged)
	// instead of Media, if the stale-detection semantics allow it.
	scanStaleLinkFilter = `
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
)

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
		`CREATE TABLE IF NOT EXISTS ScanStageProperties (
			Path         TEXT NOT NULL,
			PropertyType TEXT NOT NULL,
			Property     TEXT NOT NULL,
			Text         TEXT NOT NULL,
			PRIMARY KEY (Path, PropertyType, Property)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS scanstageproperties_property_idx
			ON ScanStageProperties(PropertyType, Property, Text)`,
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
	for _, table := range []string{"ScanStageProperties", "ScanStageTags", "ScanStage", "ScanTouchedTitles"} {
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

// logScanReconcileStep is the single choke point every set-based reconcile step
// logs through. A step slower than the threshold is warned rather than debugged
// so it survives at the default log level.
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

// chunkedStepTiming carries the parts of a chunked step's wall time that must
// not be reported as that step's SQL cost.
//
// pacing is time parked in the scanner's throttle between chunks. The scanner
// already counts that in its own throttle total, so folding it into the step
// would double-count it and make a step look expensive when it was only
// waiting. bounds is the per-chunk cursor lookup — real SQL, but a different
// statement from the step's main exec, and worth separating because it runs
// once per chunk (33 times on a 66k-file system) and is invisible today.
//
// Both are subtracted from the step's reported figure and reported on their
// own; see the step-timings line in sqlReconcileStagedSystem.
type chunkedStepTiming struct {
	bounds time.Duration
	pacing time.Duration
}

func sqlFlagMissingMedia(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	systemDBID int64,
	yield func() error,
) (int64, chunkedStepTiming, error) {
	const step = "flag missing media"
	totalStart := time.Now()
	totalAffected := int64(0)
	var timing chunkedStepTiming
	for {
		if err := ctx.Err(); err != nil {
			return totalAffected, timing, fmt.Errorf("scan reconcile cancelled before %s: %w", step, err)
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
			return totalAffected, timing, fmt.Errorf("scan reconcile %s failed: %w", step, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return totalAffected, timing, fmt.Errorf("scan reconcile %s: failed to read affected rows: %w", step, err)
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
		if yield != nil {
			pacingStart := time.Now()
			yieldErr := yield()
			timing.pacing += time.Since(pacingStart)
			if yieldErr != nil {
				return totalAffected, timing, fmt.Errorf("scan reconcile pacing after %s failed: %w", step, yieldErr)
			}
		}
		if affected < scanFlagMissingBatchSize {
			break
		}
	}
	logScanReconcileStep(systemID, step, totalAffected, time.Since(totalStart)-timing.pacing)
	return totalAffected, timing, nil
}

// sqlUpsertStagedMedia folds ScanStage rows into Media in Path-ordered chunks
// rather than one unchunked statement covering the whole system. On a system
// large enough to matter this reconcile step showed super-linear cost as one
// statement (see #1279): each chunked exec bounds Media's per-statement index
// maintenance to scanUpsertMediaBatchSize rows.
//
// The chunk cursor is scanUpsertMediaBatchSize forward positions on
// ScanStage.Path (that table's own WITHOUT ROWID primary key), found with a
// cheap bounds query before each upsert exec. This can't reuse
// sqlFlagMissingMedia's loop shape: that step is self-draining (each chunk's
// UPDATE removes those rows from the next chunk's predicate), but staged rows
// here still match the source query after being upserted, so a bare
// LIMIT-only loop would reprocess the same rows forever. RowsAffected also
// can't drive the loop — ON CONFLICT ... DO UPDATE ... WHERE <changed> reports
// 0 for a chunk that changed nothing, even when more chunks remain — so the
// cursor advances independently of it.
func sqlUpsertStagedMedia(
	ctx context.Context,
	db sqlQueryable,
	systemID string,
	systemDBID int64,
	yield func() error,
) (int64, chunkedStepTiming, error) {
	const step = "upsert media"
	totalStart := time.Now()
	totalAffected := int64(0)
	var timing chunkedStepTiming
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return totalAffected, timing, fmt.Errorf("scan reconcile cancelled before %s: %w", step, err)
		}

		var staged int64
		var upperBound sql.NullString
		boundsStart := time.Now()
		boundsErr := db.QueryRowContext(ctx, `
			SELECT COUNT(*), MAX(Path) FROM (
				SELECT Path FROM ScanStage WHERE Path > ? ORDER BY Path LIMIT ?
			)`, cursor, scanUpsertMediaBatchSize).Scan(&staged, &upperBound)
		timing.bounds += time.Since(boundsStart)
		if boundsErr != nil {
			return totalAffected, timing,
				fmt.Errorf("scan reconcile %s: failed to read chunk bounds: %w", step, boundsErr)
		}
		if staged == 0 || !upperBound.Valid {
			break
		}

		chunkStart := time.Now()
		// CROSS JOIN (not JOIN): SQLite's planner otherwise drives this from
		// MediaTitles regardless of the Path range — verified by EXPLAIN QUERY
		// PLAN at 50k scale (see reconcile_query_plan_test.go in mediascanner)
		// even with a realistic ~2000-row chunk, which would have made every
		// chunk rescan the whole system's titles instead of just its slice of
		// ScanStage. CROSS JOIN is otherwise identical to JOIN in SQLite; it
		// only disables join reordering, pinning ScanStage — already bounded
		// to this chunk by the range predicate below — as the outer loop.
		res, err := db.ExecContext(ctx, `
			INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
			SELECT t.DBID, ?, s.Path, s.ParentDir, s.SortName, 0
			FROM ScanStage s
			CROSS JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
			WHERE s.Path > ? AND s.Path <= ?
			ON CONFLICT (SystemDBID, Path) DO UPDATE SET
				MediaTitleDBID = excluded.MediaTitleDBID,
				ParentDir      = excluded.ParentDir,
				SortName       = excluded.SortName,
				IsMissing      = 0
			WHERE MediaTitleDBID <> excluded.MediaTitleDBID
			   OR ParentDir <> excluded.ParentDir
			   OR SortName <> excluded.SortName
			   OR IsMissing <> 0`, systemDBID, systemDBID, cursor, upperBound.String)
		chunkElapsed := time.Since(chunkStart)
		if err != nil {
			log.Warn().
				Str("system", systemID).
				Str("step", step).
				Dur("elapsed", chunkElapsed).
				Msg("scan reconcile step failed")
			return totalAffected, timing, fmt.Errorf("scan reconcile %s failed: %w", step, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return totalAffected, timing, fmt.Errorf("scan reconcile %s: failed to read affected rows: %w", step, err)
		}
		totalAffected += affected

		// Unlike sqlFlagMissingMedia's chunk log, this one is NOT gated on
		// affected > 0: a chunk that changes nothing and still costs seconds is
		// exactly the pathology #1279 asks this telemetry to catch.
		logEvent := log.Debug()
		if chunkElapsed > 5*time.Second {
			logEvent = log.Warn()
		}
		logEvent.Str("system", systemID).
			Str("step", step).
			Int64("batchStaged", staged).
			Int64("batchRows", affected).
			Int64("rowsAffected", totalAffected).
			Dur("elapsed", chunkElapsed).
			Msg("scan reconcile chunk completed")

		if yield != nil {
			pacingStart := time.Now()
			yieldErr := yield()
			timing.pacing += time.Since(pacingStart)
			if yieldErr != nil {
				return totalAffected, timing, fmt.Errorf("scan reconcile pacing after %s failed: %w", step, yieldErr)
			}
		}

		cursor = upperBound.String
	}
	logScanReconcileStep(systemID, step, totalAffected, time.Since(totalStart)-timing.pacing)
	return totalAffected, timing, nil
}

// sqlResolveScanSystem returns the DBID of the system row for systemID, creating
// it when absent and anything is staged. found is false when the system has no
// row and nothing is staged (nothing to reconcile).
//
// created reports that this call inserted the Systems row, i.e. the system had
// no rows of its own anywhere in the database a moment ago. Media and
// MediaTitles both declare FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID)
// and foreign keys are enforced on every connection, so no Systems row means no
// Media, MediaTitles, MediaTags or MediaProperties for it either. Reconcile uses
// that to skip the steps that exist purely to reconcile against pre-existing
// rows — see freshSystem in sqlReconcileStagedSystem.
type scanSystemRef struct {
	dbid int64
	// found is false when the system has no row and nothing is staged.
	found bool
	// created is true when this call inserted the row, i.e. the system is fresh.
	created bool
}

func sqlResolveScanSystem(ctx context.Context, db sqlQueryable, systemID string) (scanSystemRef, error) {
	var dbid int64
	err := db.QueryRowContext(ctx, "SELECT DBID FROM Systems WHERE SystemID = ?", systemID).Scan(&dbid)
	switch {
	case err == nil:
		return scanSystemRef{dbid: dbid, found: true}, nil
	case errors.Is(err, sql.ErrNoRows):
		staged, countErr := sqlScanStageCount(ctx, db)
		if countErr != nil {
			return scanSystemRef{}, countErr
		}
		if staged == 0 {
			return scanSystemRef{}, nil
		}
		res, insErr := db.ExecContext(ctx,
			"INSERT INTO Systems (SystemID, Name) VALUES (?, ?)", systemID, systemID)
		if insErr != nil {
			return scanSystemRef{}, fmt.Errorf("failed to insert system %s: %w", systemID, insErr)
		}
		dbid, insErr = res.LastInsertId()
		if insErr != nil {
			return scanSystemRef{}, fmt.Errorf("failed to read inserted system DBID for %s: %w", systemID, insErr)
		}
		return scanSystemRef{dbid: dbid, found: true, created: true}, nil
	default:
		return scanSystemRef{}, fmt.Errorf("failed to resolve system %s: %w", systemID, err)
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
	// One line carrying every step's elapsed ms, emitted at info so it survives
	// where the individual debug lines may not. Round 6 lost a whole system's
	// profile to a log rotation and most per-chunk lines to truncated captures;
	// a single summary line per system is recoverable where 34 chunk lines are
	// not.
	//
	// Every step here excludes pacing: execStep records before it yields, and
	// the two chunked steps that yield internally report their pacing
	// separately (see chunkedStepTiming). Pacing is carried as its own entry so
	// the entries still reconstruct wall time.
	//
	// The final entry is unattributed = wall time minus everything named. Round
	// 9 measured the named steps missing 22.3% of reconcile in aggregate and up
	// to 45% on individual systems, which is invisible without this term — the
	// same blind spot the scanner's own gap timers closed one level up. Any
	// step added below must go through recordStep or it reappears here.
	stepTimings := make([]string, 0, 16)
	var namedTotal, pacingTotal time.Duration
	recordStep := func(step string, elapsed time.Duration) {
		namedTotal += elapsed
		stepTimings = append(stepTimings, step+"="+strconv.FormatInt(elapsed.Milliseconds(), 10))
	}
	// recordChunkedStep reports a step that yields inside its own loop: its SQL
	// cost with bounds and pacing removed, plus the bounds lookups on their own.
	// Pacing joins the run-wide total rather than becoming a per-step entry —
	// one number per system is enough to check against the scanner's throttle.
	recordChunkedStep := func(step string, elapsed time.Duration, timing chunkedStepTiming) {
		recordStep(step, elapsed-timing.bounds-timing.pacing)
		if timing.bounds > 0 {
			recordStep(step+" bounds", timing.bounds)
		}
		pacingTotal += timing.pacing
	}
	defer func() {
		elapsed := time.Since(started)
		log.Debug().Str("system", systemID).Dur("elapsed", elapsed).Msg("scan reconcile completed")
		if len(stepTimings) > 0 {
			entries := make([]string, 0, len(stepTimings)+2)
			entries = append(entries, stepTimings...)
			entries = append(entries,
				"pacing="+strconv.FormatInt(pacingTotal.Milliseconds(), 10),
				"unattributed="+strconv.FormatInt((elapsed-namedTotal-pacingTotal).Milliseconds(), 10))
			log.Info().
				Str("system", systemID).
				Dur("elapsed", elapsed).
				Str("steps", strings.Join(entries, " ")).
				Msg("scan reconcile step timings")
		}
	}()
	log.Debug().Str("system", systemID).Bool("incompleteScan", opts.IncompleteScan).Msg("scan reconcile started")

	execStep := func(step, query string, args ...any) (int64, error) {
		stepStart := time.Now()
		affected, execErr := scanReconcileExec(ctx, db, systemID, step, query, args...)
		recordStep(step, time.Since(stepStart))
		if execErr != nil || opts.Yield == nil {
			return affected, execErr
		}
		pacingStart := time.Now()
		yieldErr := opts.Yield()
		pacingTotal += time.Since(pacingStart)
		if yieldErr != nil {
			return affected, fmt.Errorf("scan reconcile pacing after %s failed: %w", step, yieldErr)
		}
		return affected, nil
	}

	resolveStart := time.Now()
	systemRef, err := sqlResolveScanSystem(ctx, db, systemID)
	recordStep("resolve system", time.Since(resolveStart))
	if err != nil {
		return stats, err
	}
	if !systemRef.found {
		return stats, nil
	}
	systemDBID := systemRef.dbid
	freshSystem := systemRef.created
	stats.SystemKnown = true
	stats.SystemDBID = systemDBID

	// freshSystem means this reconcile created the system's Systems row, so it
	// owned no Media, MediaTitles or MediaTags beforehand (see
	// sqlResolveScanSystem). Every step below that exists to reconcile against
	// pre-existing rows is then provably a no-op and is skipped. On a first index
	// of a large system these were a third of its reconcile time while affecting
	// zero rows (#1279). Three of them are no-ops for less obvious reasons than
	// "their inputs are empty", so each carries its own justification at the skip
	// site. Re-indexing an existing library never takes this path: the Systems row
	// already exists, including for the system redone on crash-resume.
	if freshSystem {
		log.Debug().Str("system", systemID).
			Msg("scan reconcile fresh system: skipping pre-existing-state steps")
	}

	// New titles: one row per staged slug not yet present for this system. The
	// per-slug representative row is the lowest path, so multi-file titles pick
	// their metadata deterministically.
	stats.TitlesInserted, err = execStep("insert titles", `
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

	// Refresh canonical names and secondary slugs on existing titles when the
	// scan derives different ones (filename cleanup, parser changes). Triggers
	// on either column changing, since SearchMediaBySecondarySlug would
	// otherwise keep matching against a stale secondary slug for rows that
	// only get here via the Name branch.
	//
	// Skipped on a fresh system, and note the reason is not "no rows to update":
	// "insert titles" just created one row per staged slug. It is that those rows
	// were written from the same min-ScanStage.Path expression this statement
	// compares against, so both branches of the WHERE are false for every one of
	// them. ScanStage.Path is the primary key, so the MIN subquery is
	// single-valued and the two sides cannot disagree.
	if !freshSystem {
		stats.TitlesRenamed, err = execStep("rename titles", `
		UPDATE MediaTitles SET
			Name = (
				SELECT s.TitleName FROM ScanStage s
				WHERE s.Slug = MediaTitles.Slug
				  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
			),
			SecondarySlug = (
				SELECT NULLIF(s.SecondarySlug, '') FROM ScanStage s
				WHERE s.Slug = MediaTitles.Slug
				  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
			)
		WHERE SystemDBID = ?
		  AND Slug IN (SELECT Slug FROM ScanStage)
		  AND (
			Name <> (
				SELECT s.TitleName FROM ScanStage s
				WHERE s.Slug = MediaTitles.Slug
				  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
			)
			OR SecondarySlug IS NOT (
				SELECT NULLIF(s.SecondarySlug, '') FROM ScanStage s
				WHERE s.Slug = MediaTitles.Slug
				  AND s.Path = (SELECT MIN(s2.Path) FROM ScanStage s2 WHERE s2.Slug = MediaTitles.Slug)
			)
		  )`, systemDBID)
		if err != nil {
			return stats, err
		}
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
			WITH staged_titles AS (
				SELECT Slug, COUNT(*) AS staged_count FROM ScanStage GROUP BY Slug
			),
			new_titles AS (
				SELECT DISTINCT s.Slug FROM ScanStage s
				WHERE NOT EXISTS (SELECT 1 FROM Media m WHERE m.SystemDBID = ? AND m.Path = s.Path)
			)
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT t.DBID FROM new_titles nt
			JOIN staged_titles st ON st.Slug = nt.Slug
			JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = nt.Slug
			WHERE (SELECT COUNT(*) FROM Media m2 WHERE m2.MediaTitleDBID = t.DBID AND m2.IsMissing = 0)
				+ st.staged_count > 1`,
			args: []any{systemDBID, systemDBID},
		},
	}
	// The remaining captures all read FROM Media for this system, which is still
	// empty at this point in a fresh reconcile (the upsert runs below), so they
	// select nothing. "capture new media titles" above is deliberately NOT
	// skipped: it is productive on a fresh system, and "capture tag additions"
	// further down depends on it having populated ScanTouchedTitles — see the
	// comment there before changing either.
	if !freshSystem {
		preUpsertCaptures = append(preUpsertCaptures,
			scanReconcileStep{
				step: "capture reassigned titles",
				query: `
				INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
				SELECT m.MediaTitleDBID FROM Media m
				JOIN ScanStage s ON s.Path = m.Path
				JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
				WHERE m.SystemDBID = ? AND m.MediaTitleDBID <> t.DBID`,
				args: []any{systemDBID, systemDBID},
			},
			scanReconcileStep{
				step: "capture gaining titles",
				query: `
				INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
				SELECT t.DBID FROM Media m
				JOIN ScanStage s ON s.Path = m.Path
				JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
				WHERE m.SystemDBID = ? AND m.MediaTitleDBID <> t.DBID`,
				args: []any{systemDBID, systemDBID},
			},
			scanReconcileStep{
				step: "capture re-found titles",
				query: `
				INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
				SELECT m.MediaTitleDBID FROM Media m
				JOIN ScanStage s ON s.Path = m.Path
				WHERE m.SystemDBID = ? AND m.IsMissing = 1`,
				args: []any{systemDBID},
			},
		)
	}
	if !opts.IncompleteScan && !freshSystem {
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
		if _, err = execStep(capture.step, capture.query, capture.args...); err != nil {
			return stats, err
		}
	}

	// Media upsert: insert new rows, and update existing rows only when a
	// tracked field actually differs (title reassignment, parent dir move,
	// sort name change, or a missing row re-found on disk). Chunked by
	// ScanStage.Path — see sqlUpsertStagedMedia for why.
	upsertStart := time.Now()
	var upsertTiming chunkedStepTiming
	stats.MediaUpserted, upsertTiming, err = sqlUpsertStagedMedia(
		ctx, db, systemID, systemDBID, opts.Yield,
	)
	recordChunkedStep("upsert media", time.Since(upsertStart), upsertTiming)
	if err != nil {
		return stats, err
	}

	if _, err = execStep("upsert media properties", `
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text)
		SELECT m.DBID, t.DBID, sp.Text
		FROM ScanStageProperties sp
		JOIN Media m ON m.SystemDBID = ? AND m.Path = sp.Path
		JOIN TagTypes tt ON tt.Type = sp.PropertyType
		JOIN Tags t ON t.TypeDBID = tt.DBID AND t.Tag = sp.Property
		WHERE sp.Text <> ''
		ON CONFLICT(MediaDBID, TypeTagDBID) DO UPDATE SET
			Text = excluded.Text
		WHERE MediaProperties.Text IS NOT excluded.Text`, systemDBID); err != nil {
		return stats, err
	}

	// Anything on record for this system but absent from the scan is missing —
	// unless collection errored, in which case absence proves nothing. Chunk the
	// update so pathological path changes (hundreds of thousands of stale rows)
	// report progress and honour cancellation between batches instead of spending
	// minutes in one opaque SQLite statement.
	//
	// Also skipped on a fresh system. Again the reason is not an empty table —
	// the upsert has just filled Media — it is that every one of those rows was
	// written from ScanStage moments ago with IsMissing = 0, so the
	// "not present in ScanStage" predicate is false for all of them.
	if !opts.IncompleteScan && !freshSystem {
		flagMissingStart := time.Now()
		var flagMissingTiming chunkedStepTiming
		stats.MediaMissing, flagMissingTiming, err = sqlFlagMissingMedia(ctx, db, systemID, systemDBID, opts.Yield)
		recordChunkedStep("flag missing media", time.Since(flagMissingStart), flagMissingTiming)
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
	stats.TagsInserted, err = execStep("insert dynamic tags", fmt.Sprintf(`
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
	//
	// Skipped on a fresh system, and this one needs the most care of the set: its
	// SELECT is NOT empty there. MediaTags is still empty at this point (the link
	// insert is the next step), so the NOT EXISTS holds for every staged pair and
	// the SELECT returns a row per tagged media in a multi-file title. It affects
	// zero rows only because INSERT OR IGNORE finds every one of those TitleDBIDs
	// already in ScanTouchedTitles, put there by "capture new media titles" above:
	// on a fresh system that step captures exactly the titles with more than one
	// staged file, and post-upsert the per-title media count equals the per-slug
	// staged count, so multi_titles here is the same set.
	//
	// That makes the skip conditional on "capture new media titles" continuing to
	// run and to capture that set. If either changes, this skip has to be
	// re-derived — it is not safe by "the inputs are empty" reasoning.
	if !freshSystem {
		if _, err = execStep("capture tag additions", `
			WITH multi_titles AS (
				SELECT MediaTitleDBID FROM Media
				WHERE SystemDBID = ? AND IsMissing = 0
				GROUP BY MediaTitleDBID HAVING COUNT(*) > 1
			)
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT m.MediaTitleDBID
			FROM ScanStageTags st
			JOIN Media m ON m.SystemDBID = ? AND m.Path = st.Path
			JOIN multi_titles mtit ON mtit.MediaTitleDBID = m.MediaTitleDBID
			JOIN TagTypes tt ON tt.Type = st.TagType
			JOIN Tags t ON t.TypeDBID = tt.DBID AND t.Tag = st.Tag
			WHERE NOT EXISTS (
				SELECT 1 FROM MediaTags mt WHERE mt.MediaDBID = m.DBID AND mt.TagDBID = t.DBID
			)`, systemDBID, systemDBID); err != nil {
			return stats, err
		}
	}

	stats.TagLinksAdded, err = execStep("insert tag links", `
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
	//
	// Both are skipped on a fresh system. MediaTags is not empty by now — the
	// link insert above just filled it — but every row in it came from that
	// statement, reading the same (path, tag type, tag) tuples that
	// scanStaleLinkFilter's NOT EXISTS reconstructs, so no link can be stale.
	// A fresh system has no other source of MediaTags rows: the scraper's
	// writers cannot have touched media that did not exist, and reconcile holds
	// the only write transaction.
	if !freshSystem {
		staleTagTitleArgs := append([]any{systemDBID}, scanNonScannerTypeArgs(systemDBID)...)
		if _, err = execStep("capture stale tag titles",
			"WITH multi_titles AS ("+
				"SELECT MediaTitleDBID FROM Media WHERE SystemDBID = ? AND IsMissing = 0 "+
				"GROUP BY MediaTitleDBID HAVING COUNT(*) > 1) "+
				"INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID) SELECT m.MediaTitleDBID"+scanStaleLinkFilter+
				" AND EXISTS (SELECT 1 FROM multi_titles mtit WHERE mtit.MediaTitleDBID = m.MediaTitleDBID)",
			staleTagTitleArgs...); err != nil {
			return stats, err
		}
		stats.TagLinksDeleted, err = execStep("delete stale tag links",
			"DELETE FROM MediaTags WHERE (MediaDBID, TagDBID) IN (SELECT mt.MediaDBID, mt.TagDBID"+
				scanStaleLinkFilter+")",
			scanNonScannerTypeArgs(systemDBID)...)
		if err != nil {
			return stats, err
		}
	}

	countTouchedStart := time.Now()
	touchedCount, err := sqlCountScanTouchedTitles(ctx, db)
	countTouchedElapsed := time.Since(countTouchedStart)
	recordStep("count touched titles", countTouchedElapsed)
	if err != nil {
		return stats, err
	}
	stats.TouchedTitles = touchedCount
	log.Debug().
		Str("system", systemID).
		Int64("titleCount", touchedCount).
		Dur("elapsed", countTouchedElapsed).
		Msg("scan reconcile touched titles counted")
	if opts.Yield != nil {
		pacingStart := time.Now()
		yieldErr := opts.Yield()
		pacingTotal += time.Since(pacingStart)
		if yieldErr != nil {
			return stats, fmt.Errorf("scan reconcile pacing after touched-title count failed: %w", yieldErr)
		}
	}
	if touchedCount > 0 {
		if err = ctx.Err(); err != nil {
			return stats, fmt.Errorf("scan reconcile cancelled before disambiguation recompute: %w", err)
		}
		disambiguationStart := time.Now()
		recomputeScope := "titles"
		if touchedCount > scanSystemDisambiguationThreshold {
			recomputeScope = "system"
			err = sqlRecomputeDisambiguationForSystems(ctx, db, []int64{systemDBID})
		} else {
			// Only the title-scoped path needs the actual IDs; the system-wide
			// path above recomputes by systemDBID and never touches this slice.
			// With scanSystemDisambiguationThreshold this small, large scans
			// almost always take the system-wide branch, so deferring this
			// load until here avoids allocating every touched title ID for
			// scans that never use them.
			var touched []int64
			touched, err = sqlReadScanTouchedTitles(ctx, db)
			if err == nil {
				err = sqlRecomputeTitleDisambiguation(ctx, db, touched)
			}
		}
		if err != nil {
			return stats, fmt.Errorf("scan reconcile disambiguation recompute failed: %w", err)
		}
		disambiguationElapsed := time.Since(disambiguationStart)
		recordStep("disambiguation", disambiguationElapsed)
		logEvent := log.Debug()
		if disambiguationElapsed > 5*time.Second {
			logEvent = log.Warn()
		}
		logEvent.Str("system", systemID).
			Str("scope", recomputeScope).
			Int64("titleCount", touchedCount).
			Dur("elapsed", disambiguationElapsed).
			Msg("scan reconcile disambiguation recompute completed")
		if opts.Yield != nil {
			pacingStart := time.Now()
			yieldErr := opts.Yield()
			pacingTotal += time.Since(pacingStart)
			if yieldErr != nil {
				return stats, fmt.Errorf("scan reconcile pacing after disambiguation failed: %w", yieldErr)
			}
		}
	}

	clearStart := time.Now()
	clearErr := sqlClearScanStage(ctx, db)
	recordStep("clear scan stage", time.Since(clearStart))
	if clearErr != nil {
		return stats, clearErr
	}
	return stats, nil
}

// sqlCountScanTouchedTitles returns the number of titles touched by the
// current reconcile without loading their IDs, so the disambiguation-scope
// decision (title-scoped vs system-wide) doesn't require allocating a slice
// that the system-wide path never uses.
func sqlCountScanTouchedTitles(ctx context.Context, db sqlQueryable) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ScanTouchedTitles").Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count touched titles: %w", err)
	}
	return count, nil
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

// canonicalTagVocabHash returns a digest of the compiled-in canonical tag
// vocabulary: every type row and value row that sqlSeedCanonicalTags would
// insert, sorted for determinism (the rows are built from map iteration).
// Stored in DBConfig after a successful seed so later index runs can skip
// re-proving ~1,400 rows exist — a cost of a minute or more on slow storage.
func canonicalTagVocabHash(typeRows []canonicalTypeRow, tagRows []canonicalTagRow) string {
	lines := make([]string, 0, len(typeRows)+len(tagRows))
	for _, row := range typeRows {
		lines = append(lines, "t\x00"+row.name+"\x00"+strconv.FormatBool(row.isExclusive))
	}
	for _, row := range tagRows {
		lines = append(lines, "v\x00"+row.typeName+"\x00"+row.value)
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// invalidateCanonicalTagVocabStampIfDeleted clears the vocabulary seeding
// stamp after an orphan-tag cleanup that actually removed rows: canonical
// vocabulary rows may have been among them, so the next index run must seed
// again. res may be nil when the caller's driver returned none.
func invalidateCanonicalTagVocabStampIfDeleted(ctx context.Context, db sqlQueryable, res sql.Result) {
	if res == nil {
		return
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		log.Warn().Err(err).Msg("failed to read orphan tag cleanup row count; clearing vocabulary stamp")
	} else if deleted == 0 {
		return
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM DBConfig WHERE Name = ?", DBConfigCanonicalTagVocabHash,
	); err != nil {
		log.Warn().Err(err).Msg("failed to clear canonical tag vocabulary stamp after tag cleanup")
	}
}

// sqlSeedCanonicalTags ensures every canonical tag type and value exists,
// set-based: one anti-joined insert for types, then chunked anti-joined inserts
// for values. Replaces the per-row ScanState-driven seeding. A DBConfig stamp
// of the vocabulary hash short-circuits the whole pass when a previous run
// already seeded this exact vocabulary.
func sqlSeedCanonicalTags(ctx context.Context, db sqlQueryable) error {
	// Dedupe within the statement: the NOT EXISTS anti-join only sees rows
	// already in the table, not other rows of the same INSERT ... SELECT.
	seenTypes := map[string]struct{}{}
	typeRows := make([]canonicalTypeRow, 0, len(tags.CanonicalTagDefinitions)+2)
	addType := func(tagType tags.TagType) {
		name := string(tagType)
		if _, ok := seenTypes[name]; ok {
			return
		}
		seenTypes[name] = struct{}{}
		typeRows = append(typeRows, canonicalTypeRow{name, tags.IsExclusiveType(tagType)})
	}
	addType(tags.TagTypeUnknown)
	addType(tags.TagTypeExtension)
	for tagType := range tags.CanonicalTagDefinitions {
		addType(tagType)
	}

	seenTags := map[string]struct{}{}
	tagRows := make([]canonicalTagRow, 0, 1400)
	addTag := func(typeName, value string) {
		key := typeName + "\x00" + value
		if _, ok := seenTags[key]; ok {
			return
		}
		seenTags[key] = struct{}{}
		tagRows = append(tagRows, canonicalTagRow{typeName: typeName, value: value})
	}
	addTag(string(tags.TagTypeUnknown), "unknown")
	for tagType, values := range tags.CanonicalTagDefinitions {
		for _, value := range values {
			addTag(string(tagType), tags.PadTagValue(strings.ToLower(string(value))))
		}
	}

	// The vocabulary is compiled into the binary, so once a run has seeded it
	// the anti-joined inserts are guaranteed no-ops until a release changes the
	// tags package. Skip them entirely when the stored stamp matches; a stamp
	// read failure just means seeding runs as it always has.
	vocabHash := canonicalTagVocabHash(typeRows, tagRows)
	var storedHash string
	stampErr := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", DBConfigCanonicalTagVocabHash,
	).Scan(&storedHash)
	if stampErr == nil && storedHash == vocabHash {
		log.Debug().Msg("canonical tag vocabulary already seeded, skipping")
		return nil
	}
	if stampErr != nil && !errors.Is(stampErr, sql.ErrNoRows) {
		log.Warn().Err(stampErr).Msg("failed to read canonical tag vocabulary stamp, seeding anyway")
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
		INSERT INTO TagTypes (Type, IsExclusive) VALUES %s
		ON CONFLICT(Type) DO UPDATE SET IsExclusive = excluded.IsExclusive`, sb.String())
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to seed canonical tag types: %w", err)
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

	// Non-fatal: a missed stamp only means the next run seeds again.
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigCanonicalTagVocabHash, vocabHash,
	); err != nil {
		log.Warn().Err(err).Msg("failed to write canonical tag vocabulary stamp")
	}
	return nil
}
