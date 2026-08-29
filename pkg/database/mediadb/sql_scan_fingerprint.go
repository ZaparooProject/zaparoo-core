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
	"hash"
	"strconv"

	"github.com/rs/zerolog/log"
)

// scanReconcileVersion identifies what sqlReconcileStagedSystem derives from a
// staged set and writes to the media tables. It is folded into every stored
// fingerprint, so bumping it invalidates all of them and the next index run
// reconciles every system once more. Bump it whenever a reconcile statement
// changes what it writes for the same staged input — a fingerprint match is
// only proof of "nothing to do" while the reconcile that produced the stored
// state is the one that would run now.
const scanReconcileVersion = "1"

// scanStagedSet is the staged input for one system as seen by reconcile: the
// digest of every ScanStage and ScanStageTags row it will consume, plus the
// row counts behind it. Properties are counted rather than hashed because the
// scanner only stages a property that is absent from the database
// (stagedPropertiesFromPath), so any staged property is new work by
// construction and the count alone decides whether reconcile can be skipped.
type scanStagedSet struct {
	fingerprint string
	files       int64
	tagRows     int64
	properties  int64
}

// scanStoredState digests the rows reconcile owns for one system — the ones
// its statements would read and compare against the staged set. Reconcile is
// only provably a no-op when both its inputs are unchanged: the staged set
// (scanStagedSet) and the stored state it would reconcile that set against.
// Hashing the stored state, rather than trusting that only reconcile writes
// it, is what lets an edit made by anything else — a scraper or API write of a
// scanner-owned tag, an orphan cleanup, a manual repair — fail open into a
// full reconcile instead of being silently preserved.
type scanStoredState struct {
	digest string
	titles int64
	media  int64
}

// scanFingerprintRow is the durable record of the last successful reconcile
// for one system: the staged set it consumed and the stored state it left
// behind. Counts are carried for the skip log line.
type scanFingerprintRow struct {
	fingerprint string
	stateDigest string
	mediaCount  int64
	titleCount  int64
	reconcileMs int64
}

// scanFingerprintGeneration returns the generation prefix folded into every
// staged-set digest: the reconcile behaviour version, the applied schema
// version, the compiled-in canonical tag vocabulary and the disambiguation
// algorithm version. A change to any of them moves every fingerprint, so a
// release that alters what reconcile would produce for the same files never
// matches a fingerprint written by the previous release.
func scanFingerprintGeneration(ctx context.Context, db sqlQueryable) (string, error) {
	var schemaVersion int64
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1",
	).Scan(&schemaVersion)
	if err != nil {
		return "", fmt.Errorf("failed to read schema version for scan fingerprint: %w", err)
	}
	typeRows, tagRows := canonicalTagVocabRows()
	return "reconcile\x00" + scanReconcileVersion +
		"\nschema\x00" + strconv.FormatInt(schemaVersion, 10) +
		"\nvocab\x00" + canonicalTagVocabHash(typeRows, tagRows) +
		"\ndisambiguation\x00" + disambiguationAlgoVersion + "\n", nil
}

// sqlScanStagedFingerprint digests the staged set for the current system. Rows
// are read in primary-key order from both WITHOUT ROWID tables, so the digest
// is independent of the order files were staged in, and every column reconcile
// consumes is included — a parser change that alters a slug, sort name or tag
// without touching the path moves the digest.
//
// The row scans deliberately run without the caller's cancellation: with a
// cancellable context the SQLite driver interrupts each rows.Next() through a
// goroutine, which on a 30k-row staged set costs more than the scan itself.
// Cancellation is checked between the scans instead.
func sqlScanStagedFingerprint(ctx context.Context, db sqlQueryable) (scanStagedSet, error) {
	var set scanStagedSet
	generation, err := scanFingerprintGeneration(ctx, db)
	if err != nil {
		return set, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("zaparoo-scan-fingerprint\n"))
	_, _ = h.Write([]byte(generation))

	readCtx := context.WithoutCancel(ctx)
	if err = ctx.Err(); err != nil {
		return set, fmt.Errorf("scan fingerprint cancelled: %w", err)
	}
	// One row and one column per staged file, with its tags folded in by SQL,
	// rather than a second scan of ScanStageTags: every row and every column
	// crossing the driver costs an allocation, and a system's tag rows
	// outnumber its files several times over. The tag list is ordered inside
	// the aggregate so the row is deterministic. Fields are joined with unit
	// and record separators, which no filename or tag value contains.
	set.files, err = hashRows(readCtx, db, h, "files", "staged files", `
		SELECT s.Path || char(31) || s.ParentDir || char(31) || s.Slug || char(31) || s.TitleName || char(31)
			|| s.SortName || char(31) || s.SlugLength || char(31) || s.SlugWordCount || char(31)
			|| COALESCE(s.SecondarySlug, '') || char(31)
			|| COALESCE((
				SELECT group_concat(st.TagType || char(31) || st.Tag, char(30) ORDER BY st.TagType, st.Tag)
				FROM ScanStageTags st WHERE st.Path = s.Path
			), '')
		FROM ScanStage s ORDER BY s.Path`)
	if err != nil {
		return set, err
	}
	if err = db.QueryRowContext(readCtx, "SELECT COUNT(*) FROM ScanStageTags").Scan(&set.tagRows); err != nil {
		return set, fmt.Errorf("failed to count staged tags for scan fingerprint: %w", err)
	}
	if err = db.QueryRowContext(readCtx, "SELECT COUNT(*) FROM ScanStageProperties").Scan(&set.properties); err != nil {
		return set, fmt.Errorf("failed to count staged properties for scan fingerprint: %w", err)
	}
	set.fingerprint = hex.EncodeToString(h.Sum(nil))
	return set, nil
}

// sqlScanStoredStateDigest digests the rows reconcile would compare the staged
// set against, for one system: one row per media, in path order (which the
// (SystemDBID, Path) index provides without a sort), carrying the columns
// reconcile reads or rewrites for existing rows. Those are the media row's
// parent directory, sort name and missing flag (the upsert and missing-flag
// steps), its title's slug, name and secondary slug (title reassignment and
// the rename step) and its tag links (the link insert and stale-link delete),
// folded into the row as an ordered list of tag DBIDs. A title is hashed
// through its media rather than on its own because reconcile only ever reaches
// a title through a staged file. Links are hashed for every tag type, not just
// the scanner-owned ones — that costs one reconcile of a system after it is
// scraped, and avoids joining Tags and TagTypes to filter by type.
//
// Runs without cancellation for the same reason as sqlScanStagedFingerprint.
func sqlScanStoredStateDigest(ctx context.Context, db sqlQueryable, systemDBID int64) (scanStoredState, error) {
	var state scanStoredState
	h := sha256.New()
	_, _ = h.Write([]byte("zaparoo-scan-stored-state\n"))

	var err error
	if err = ctx.Err(); err != nil {
		return state, fmt.Errorf("scan stored state digest cancelled: %w", err)
	}
	// One column per row for the same reason as the staged-set query. Every
	// text operand is COALESCEd: a NULL anywhere would null the whole row and
	// hash as empty, silently hiding that row's other fields.
	state.media, err = hashRows(context.WithoutCancel(ctx), db, h, "media", "system media", `
		SELECT m.Path || char(31) || COALESCE(m.ParentDir, '') || char(31) || COALESCE(m.SortName, '') || char(31)
			|| m.IsMissing || char(31) || t.Slug || char(31) || COALESCE(t.Name, '') || char(31)
			|| COALESCE(t.SecondarySlug, '') || char(31)
			|| COALESCE((
				SELECT group_concat(mt.TagDBID, ',' ORDER BY mt.TagDBID)
				FROM MediaTags mt WHERE mt.MediaDBID = m.DBID
			), '')
		FROM Media m
		JOIN MediaTitles t ON t.DBID = m.MediaTitleDBID
		WHERE m.SystemDBID = ? ORDER BY m.Path`, systemDBID)
	if err != nil {
		return state, err
	}
	if err = db.QueryRowContext(context.WithoutCancel(ctx),
		"SELECT COUNT(*) FROM MediaTitles WHERE SystemDBID = ?", systemDBID,
	).Scan(&state.titles); err != nil {
		return state, fmt.Errorf("failed to count system titles for scan fingerprint: %w", err)
	}
	state.digest = hex.EncodeToString(h.Sum(nil))
	return state, nil
}

// hashRows runs query and folds every row into h as one NUL-separated,
// newline-terminated record after a section header, returning the row count.
// Values are read as their textual form (integers print in decimal), which is
// stable across driver versions. NUL cannot appear in a SQLite TEXT value read
// through the driver, so the framing is unambiguous and adjacent rows cannot
// alias each other.
func hashRows(
	ctx context.Context, db sqlQueryable, h hash.Hash, section, what, query string, args ...any,
) (int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s for scan fingerprint: %w", what, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("rows", what).Msg("failed to close scan fingerprint rows")
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("failed to read %s columns for scan fingerprint: %w", what, err)
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	_, _ = h.Write([]byte(section + "\n"))
	var count int64
	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			return 0, fmt.Errorf("failed to scan %s for scan fingerprint: %w", what, err)
		}
		count++
		for i, value := range values {
			if i > 0 {
				_, _ = h.Write([]byte{0})
			}
			_, _ = h.Write(value)
		}
		_, _ = h.Write([]byte{'\n'})
	}
	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("failed reading %s for scan fingerprint: %w", what, err)
	}
	return count, nil
}

func sqlLoadScanFingerprint(ctx context.Context, db sqlQueryable, systemDBID int64) (scanFingerprintRow, bool, error) {
	var row scanFingerprintRow
	err := db.QueryRowContext(ctx, `
		SELECT Fingerprint, StateDigest, MediaCount, TitleCount, ReconcileMs
		FROM ScanSystemFingerprints WHERE SystemDBID = ?`, systemDBID,
	).Scan(&row.fingerprint, &row.stateDigest, &row.mediaCount, &row.titleCount, &row.reconcileMs)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("failed to load scan fingerprint: %w", err)
	}
	return row, true, nil
}

func sqlStoreScanFingerprint(ctx context.Context, db sqlQueryable, systemDBID int64, row scanFingerprintRow) error {
	if _, err := db.ExecContext(ctx, `
		INSERT OR REPLACE INTO ScanSystemFingerprints
			(SystemDBID, Fingerprint, StateDigest, MediaCount, TitleCount, ReconcileMs)
		VALUES (?, ?, ?, ?, ?, ?)`,
		systemDBID, row.fingerprint, row.stateDigest, row.mediaCount, row.titleCount, row.reconcileMs,
	); err != nil {
		return fmt.Errorf("failed to store scan fingerprint: %w", err)
	}
	return nil
}

func sqlDeleteScanFingerprint(ctx context.Context, db sqlQueryable, systemDBID int64) error {
	if _, err := db.ExecContext(ctx,
		"DELETE FROM ScanSystemFingerprints WHERE SystemDBID = ?", systemDBID,
	); err != nil {
		return fmt.Errorf("failed to clear scan fingerprint: %w", err)
	}
	return nil
}
