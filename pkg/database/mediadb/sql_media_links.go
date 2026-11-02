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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// linkedMediaCopiesProperty says, for every canonical property, whether a
// linked row takes its target's value. A linked row is the same file as its
// target, so it takes everything a scraper wrote. It keeps the two properties
// that are not scraper data: the indexer's disc identity, and the user's
// launcher override. A new property must be classified here; a test enforces
// it.
var linkedMediaCopiesProperty = map[tags.TagValue]bool{
	tags.TagPropertyDescription:          true,
	tags.TagPropertyImageImage:           true,
	tags.TagPropertyImageBoxart:          true,
	tags.TagPropertyImageBoxart3D:        true,
	tags.TagPropertyImageBoxartSide:      true,
	tags.TagPropertyImageBoxartBack:      true,
	tags.TagPropertyImageScreenshot:      true,
	tags.TagPropertyImageThumbnail:       true,
	tags.TagPropertyImageMarquee:         true,
	tags.TagPropertyImageWheel:           true,
	tags.TagPropertyImageFanart:          true,
	tags.TagPropertyImageTitleshot:       true,
	tags.TagPropertyImageMap:             true,
	tags.TagPropertyVideo:                true,
	tags.TagPropertyManual:               true,
	tags.TagPropertyXMLGameID:            true,
	tags.TagPropertyGameDatabaseID:       true,
	tags.TagPropertyGameDatabaseContains: true,
	tags.TagPropertyMAMESetName:          true,
	tags.TagPropertyGameID:               false,
	tags.TagPropertyLauncherOverride:     false,
}

// linkedOwnPropertyFilter is a predicate on a TypeTagDBID column that is true
// for properties a linked row keeps as its own.
func linkedOwnPropertyFilter(column string) (clause string, args []any) {
	for value, copies := range linkedMediaCopiesProperty {
		if !copies {
			args = append(args, tags.PadTagValue(string(value)))
		}
	}
	clause = column + ` IN (
		SELECT t.DBID FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type = ? AND t.Tag IN (` + prepareVariadic("?", ",", len(args)) + `))`
	return clause, append([]any{string(tags.TagTypeProperty)}, args...)
}

// linkedOwnedTagFilter is a predicate over a MediaTags row (mt) and its tag
// type (tt) that is true for the links a linked row takes from its target:
// everything a scraper wrote, including scraper sentinels, but never user tags
// or per-run markers. Genre, rating and scraper types are matched by type too,
// because links written before MediaTags.Scraped existed carry Scraped = 0.
func linkedOwnedTagFilter(mt, tt string) (clause string, args []any) {
	clause = fmt.Sprintf(`(%[1]s.Scraped = 1 OR %[2]s.Type IN (?, ?) OR %[2]s.Type LIKE ?)
		AND %[2]s.Type <> ? AND %[2]s.Type NOT LIKE ?`, mt, tt)
	args = []any{
		string(tags.TagTypeGenre),
		string(tags.TagTypeRating),
		string(tags.ScraperType("")) + "%",
		string(tags.TagTypeUser),
		string(tags.ScraperRunType("")) + "%",
	}
	return clause, args
}

// linkSyncResult reports what a link sync changed.
type linkSyncResult struct {
	// propertySystems are the systems whose linked rows had properties change.
	propertySystems []int64
	// tagSystems are the systems whose linked rows had tags change.
	tagSystems []int64
	// tagTitles are the titles of linked rows whose tags changed.
	tagTitles []int64
}

func (r *linkSyncResult) changed() bool {
	return len(r.propertySystems) > 0 || len(r.tagSystems) > 0
}

// sqlSystemHasMediaLinks reports whether any linked media belongs to one of
// the given systems, or to any system when none are given.
func sqlSystemHasMediaLinks(ctx context.Context, db sqlQueryable, systemDBIDs []int64) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM MediaLinks)`
	args := make([]any, 0, len(systemDBIDs))
	if len(systemDBIDs) > 0 {
		query = `SELECT EXISTS (
			SELECT 1 FROM MediaLinks l JOIN Media a ON a.DBID = l.MediaDBID
			WHERE a.SystemDBID IN (` + prepareVariadic("?", ",", len(systemDBIDs)) + `))`
		for _, id := range systemDBIDs {
			args = append(args, id)
		}
	}
	var exists bool
	if err := db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("check media links: %w", err)
	}
	return exists, nil
}

// sqlSyncMediaLinks makes every present linked row in the given systems (all
// systems when none are given) carry its target's scraped data, written into
// the linked row's own media scope. Titles are never written: a linked row's
// title may be shared with rows that are not links. When the linked row
// already shares its target's title, the title's data applies to it as it is,
// and only the target's media-scope data is copied. Rows already matching
// their target are not written. When captureTouched is set, titles whose
// linked rows had tags change are added to ScanTouchedTitles for the
// reconcile's disambiguation recompute.
//
//nolint:gocognit,funlen // linear statement sequence
func sqlSyncMediaLinks(
	ctx context.Context, db sqlQueryable, systemDBIDs []int64, captureTouched bool,
) (linkSyncResult, error) {
	var result linkSyncResult
	setup := []string{
		`CREATE TEMP TABLE IF NOT EXISTS _mls_links (
			MediaDBID INTEGER PRIMARY KEY, TargetMediaDBID INTEGER NOT NULL,
			TitleDBID INTEGER NOT NULL, TargetTitleDBID INTEGER NOT NULL,
			SystemDBID INTEGER NOT NULL, SharesTitle INTEGER NOT NULL)`,
		`CREATE TEMP TABLE IF NOT EXISTS _mls_props (
			MediaDBID INTEGER NOT NULL, TypeTagDBID INTEGER NOT NULL, Text TEXT NOT NULL, BlobDBID INTEGER,
			PRIMARY KEY (MediaDBID, TypeTagDBID)) WITHOUT ROWID`,
		`CREATE TEMP TABLE IF NOT EXISTS _mls_tags (
			MediaDBID INTEGER NOT NULL, TagDBID INTEGER NOT NULL, TypeDBID INTEGER NOT NULL,
			PRIMARY KEY (MediaDBID, TagDBID)) WITHOUT ROWID`,
		`CREATE TEMP TABLE IF NOT EXISTS _mls_prop_changed (MediaDBID INTEGER PRIMARY KEY)`,
		`CREATE TEMP TABLE IF NOT EXISTS _mls_tag_changed (MediaDBID INTEGER PRIMARY KEY)`,
	}
	tempTables := []string{"_mls_links", "_mls_props", "_mls_tags", "_mls_prop_changed", "_mls_tag_changed"}
	for _, stmt := range setup {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return result, fmt.Errorf("create link sync table: %w", err)
		}
	}
	defer func() {
		for _, table := range tempTables {
			_, _ = db.ExecContext(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+table)
		}
	}()
	for _, table := range tempTables {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return result, fmt.Errorf("clear link sync table: %w", err)
		}
	}

	exec := func(step, query string, args ...any) error {
		if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
			return fmt.Errorf("link sync %s: %w", step, execErr)
		}
		return nil
	}

	linkArgs := make([]any, 0, len(systemDBIDs))
	systemFilter := ""
	if len(systemDBIDs) > 0 {
		systemFilter = " AND a.SystemDBID IN (" + prepareVariadic("?", ",", len(systemDBIDs)) + ")"
		for _, id := range systemDBIDs {
			linkArgs = append(linkArgs, id)
		}
	}
	if err := exec("collect links", `
		INSERT INTO _mls_links (MediaDBID, TargetMediaDBID, TitleDBID, TargetTitleDBID, SystemDBID, SharesTitle)
		SELECT l.MediaDBID, l.TargetMediaDBID, a.MediaTitleDBID, t.MediaTitleDBID, a.SystemDBID,
			a.MediaTitleDBID = t.MediaTitleDBID
		FROM MediaLinks l
		JOIN Media a ON a.DBID = l.MediaDBID
		JOIN Media t ON t.DBID = l.TargetMediaDBID
		WHERE a.IsMissing = 0`+systemFilter, linkArgs...); err != nil {
		return result, err
	}

	ownProp, ownPropArgs := linkedOwnPropertyFilter("TypeTagDBID")

	// Desired properties: the target's media scope first, then its title scope
	// fills the types the media scope left empty, the precedence media.image
	// reads them in.
	//nolint:gosec // ownProp holds only placeholders and fixed column names.
	if err := exec("collect media properties", `
		INSERT INTO _mls_props (MediaDBID, TypeTagDBID, Text, BlobDBID)
		SELECT k.MediaDBID, mp.TypeTagDBID, mp.Text, mp.BlobDBID
		FROM _mls_links k JOIN MediaProperties mp ON mp.MediaDBID = k.TargetMediaDBID
		WHERE NOT mp.`+ownProp, ownPropArgs...); err != nil {
		return result, err
	}
	//nolint:gosec // ownProp holds only placeholders and fixed column names.
	if err := exec("collect title properties", `
		INSERT OR IGNORE INTO _mls_props (MediaDBID, TypeTagDBID, Text, BlobDBID)
		SELECT k.MediaDBID, tp.TypeTagDBID, tp.Text, tp.BlobDBID
		FROM _mls_links k JOIN MediaTitleProperties tp ON tp.MediaTitleDBID = k.TargetTitleDBID
		WHERE k.SharesTitle = 0 AND NOT tp.`+ownProp, ownPropArgs...); err != nil {
		return result, err
	}

	if err := exec("capture property changes", `
		INSERT OR IGNORE INTO _mls_prop_changed (MediaDBID)
		SELECT d.MediaDBID FROM _mls_props d
		LEFT JOIN MediaProperties mp ON mp.MediaDBID = d.MediaDBID AND mp.TypeTagDBID = d.TypeTagDBID
		WHERE mp.DBID IS NULL OR mp.Text IS NOT d.Text OR mp.BlobDBID IS NOT d.BlobDBID`); err != nil {
		return result, err
	}
	//nolint:gosec // ownProp holds only placeholders and fixed column names.
	if err := exec("capture property removals", `
		INSERT OR IGNORE INTO _mls_prop_changed (MediaDBID)
		SELECT mp.MediaDBID FROM _mls_links k JOIN MediaProperties mp ON mp.MediaDBID = k.MediaDBID
		WHERE NOT mp.`+ownProp+`
		  AND NOT EXISTS (
			SELECT 1 FROM _mls_props d WHERE d.MediaDBID = mp.MediaDBID AND d.TypeTagDBID = mp.TypeTagDBID
		  )`, ownPropArgs...); err != nil {
		return result, err
	}
	//nolint:gosec // ownProp holds only placeholders and fixed column names.
	if err := exec("delete stale properties", `
		DELETE FROM MediaProperties
		WHERE MediaDBID IN (SELECT MediaDBID FROM _mls_prop_changed)
		  AND NOT `+ownProp+`
		  AND NOT EXISTS (
			SELECT 1 FROM _mls_props d
			WHERE d.MediaDBID = MediaProperties.MediaDBID AND d.TypeTagDBID = MediaProperties.TypeTagDBID
		  )`, ownPropArgs...); err != nil {
		return result, err
	}
	if err := exec("upsert properties", `
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text, BlobDBID)
		SELECT MediaDBID, TypeTagDBID, Text, BlobDBID FROM _mls_props
		WHERE MediaDBID IN (SELECT MediaDBID FROM _mls_prop_changed)
		ON CONFLICT(MediaDBID, TypeTagDBID) DO UPDATE SET Text = excluded.Text, BlobDBID = excluded.BlobDBID
		WHERE MediaProperties.Text IS NOT excluded.Text
		   OR MediaProperties.BlobDBID IS NOT excluded.BlobDBID`); err != nil {
		return result, err
	}
	// A cached cover colour and type describe the artwork the row had before.
	if err := exec("delete stale cover thumbs", `
		DELETE FROM MediaCoverThumbs WHERE MediaDBID IN (SELECT MediaDBID FROM _mls_prop_changed)`); err != nil {
		return result, err
	}

	owned, ownedArgs := linkedOwnedTagFilter("mt", "tt")
	//nolint:gosec // owned holds only placeholders and fixed column names.
	if err := exec("collect media tags", `
		INSERT OR IGNORE INTO _mls_tags (MediaDBID, TagDBID, TypeDBID)
		SELECT k.MediaDBID, mt.TagDBID, t.TypeDBID
		FROM _mls_links k
		JOIN MediaTags mt ON mt.MediaDBID = k.TargetMediaDBID
		JOIN Tags t ON t.DBID = mt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE `+owned, ownedArgs...); err != nil {
		return result, err
	}
	// Title tags are all scraper data. A single-value type the target's media
	// scope already supplied keeps that value.
	if err := exec("collect title tags", `
		INSERT OR IGNORE INTO _mls_tags (MediaDBID, TagDBID, TypeDBID)
		SELECT k.MediaDBID, mtt.TagDBID, t.TypeDBID
		FROM _mls_links k
		JOIN MediaTitleTags mtt ON mtt.MediaTitleDBID = k.TargetTitleDBID
		JOIN Tags t ON t.DBID = mtt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE k.SharesTitle = 0
		  AND tt.Type <> ? AND tt.Type NOT LIKE ?
		  AND NOT (tt.IsExclusive = 1 AND EXISTS (
			SELECT 1 FROM _mls_tags d WHERE d.MediaDBID = k.MediaDBID AND d.TypeDBID = t.TypeDBID
		  ))`, string(tags.TagTypeUser), string(tags.ScraperRunType(""))+"%"); err != nil {
		return result, err
	}
	// The indexer's own value for a single-value type stays the row's value.
	//nolint:gosec // owned holds only placeholders and fixed column names.
	if err := exec("drop indexer conflicts", `
		DELETE FROM _mls_tags
		WHERE TypeDBID IN (SELECT DBID FROM TagTypes WHERE IsExclusive = 1)
		  AND EXISTS (
			SELECT 1 FROM MediaTags mt
			JOIN Tags t ON t.DBID = mt.TagDBID
			JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE mt.MediaDBID = _mls_tags.MediaDBID AND t.TypeDBID = _mls_tags.TypeDBID
			  AND NOT (`+owned+`)
		  )`, ownedArgs...); err != nil {
		return result, err
	}

	if err := exec("capture tag additions", `
		INSERT OR IGNORE INTO _mls_tag_changed (MediaDBID)
		SELECT d.MediaDBID FROM _mls_tags d
		WHERE NOT EXISTS (
			SELECT 1 FROM MediaTags mt WHERE mt.MediaDBID = d.MediaDBID AND mt.TagDBID = d.TagDBID
		)`); err != nil {
		return result, err
	}
	//nolint:gosec // owned holds only placeholders and fixed column names.
	if err := exec("capture tag removals", `
		INSERT OR IGNORE INTO _mls_tag_changed (MediaDBID)
		SELECT mt.MediaDBID
		FROM _mls_links k
		JOIN MediaTags mt ON mt.MediaDBID = k.MediaDBID
		JOIN Tags t ON t.DBID = mt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE `+owned+`
		  AND NOT EXISTS (
			SELECT 1 FROM _mls_tags d WHERE d.MediaDBID = mt.MediaDBID AND d.TagDBID = mt.TagDBID
		  )`, ownedArgs...); err != nil {
		return result, err
	}
	//nolint:gosec // owned holds only placeholders and fixed column names.
	if err := exec("delete stale tags", `
		DELETE FROM MediaTags WHERE (MediaDBID, TagDBID) IN (
			SELECT mt.MediaDBID, mt.TagDBID
			FROM _mls_tag_changed c
			JOIN MediaTags mt ON mt.MediaDBID = c.MediaDBID
			JOIN Tags t ON t.DBID = mt.TagDBID
			JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE `+owned+`
			  AND NOT EXISTS (
				SELECT 1 FROM _mls_tags d WHERE d.MediaDBID = mt.MediaDBID AND d.TagDBID = mt.TagDBID
			  )
		)`, ownedArgs...); err != nil {
		return result, err
	}
	// An indexer link of the same tag already present stays Scraped = 0, so
	// the scanner still owns it.
	if err := exec("insert tags", `
		INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID, Scraped)
		SELECT MediaDBID, TagDBID, 1 FROM _mls_tags
		WHERE MediaDBID IN (SELECT MediaDBID FROM _mls_tag_changed)`); err != nil {
		return result, err
	}

	if captureTouched {
		if err := exec("capture touched titles", `
			INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
			SELECT DISTINCT k.TitleDBID FROM _mls_links k
			WHERE k.MediaDBID IN (SELECT MediaDBID FROM _mls_tag_changed)`); err != nil {
			return result, err
		}
	}

	var err error
	if result.propertySystems, err = queryInt64s(ctx, db, `
		SELECT DISTINCT k.SystemDBID FROM _mls_links k
		WHERE k.MediaDBID IN (SELECT MediaDBID FROM _mls_prop_changed)`); err != nil {
		return result, err
	}
	if result.tagSystems, err = queryInt64s(ctx, db, `
		SELECT DISTINCT k.SystemDBID FROM _mls_links k
		WHERE k.MediaDBID IN (SELECT MediaDBID FROM _mls_tag_changed)`); err != nil {
		return result, err
	}
	if result.tagTitles, err = queryInt64s(ctx, db, `
		SELECT DISTINCT k.TitleDBID FROM _mls_links k
		WHERE k.MediaDBID IN (SELECT MediaDBID FROM _mls_tag_changed)`); err != nil {
		return result, err
	}
	return result, nil
}

// sqlPurgeLinkCopies deletes the scraped data a sync copied onto rows that
// are no longer links, so a file that stops being an alias does not keep its
// former target's art. Nothing else writes scraped data to a linked row:
// scrapers never select one. rowsQuery selects the affected Media DBIDs.
func sqlPurgeLinkCopies(ctx context.Context, db sqlQueryable, rowsQuery string, rowsArgs ...any) error {
	ownProp, ownPropArgs := linkedOwnPropertyFilter("TypeTagDBID")
	//nolint:gosec // ownProp and rowsQuery hold only placeholders and fixed SQL.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM MediaProperties
		WHERE MediaDBID IN (`+rowsQuery+`) AND NOT `+ownProp,
		append(append([]any{}, rowsArgs...), ownPropArgs...)...); err != nil {
		return fmt.Errorf("purge linked properties: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		DELETE FROM MediaCoverThumbs WHERE MediaDBID IN (`+rowsQuery+`)`, rowsArgs...); err != nil {
		return fmt.Errorf("purge linked cover thumbs: %w", err)
	}
	owned, ownedArgs := linkedOwnedTagFilter("mt", "tt")
	//nolint:gosec // owned and rowsQuery hold only placeholders and fixed SQL.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM MediaTags WHERE (MediaDBID, TagDBID) IN (
			SELECT mt.MediaDBID, mt.TagDBID FROM MediaTags mt
			JOIN Tags t ON t.DBID = mt.TagDBID
			JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE mt.MediaDBID IN (`+rowsQuery+`) AND `+owned+`
		)`, append(append([]any{}, rowsArgs...), ownedArgs...)...); err != nil {
		return fmt.Errorf("purge linked tags: %w", err)
	}
	return nil
}

func queryInt64s(ctx context.Context, db sqlQueryable, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ids: %w", err)
	}
	return ids, nil
}

// SyncLinkedMedia copies each linked row's target data onto it for the given
// systems, or every system when none are given. Scrape runs call it once they
// finish, because scrapers write only to the target and never select a link.
func (db *MediaDB) SyncLinkedMedia(ctx context.Context, systemIDs []string) (retErr error) {
	conn := db.sql.Load()
	if conn == nil {
		return ErrNullSQL
	}
	defer func() { db.NoteCorruption(retErr) }()

	var systemDBIDs []int64
	if len(systemIDs) > 0 {
		args := make([]any, 0, len(systemIDs))
		for _, id := range systemIDs {
			args = append(args, id)
		}
		var err error
		systemDBIDs, err = queryInt64s(ctx, conn, `SELECT DBID FROM Systems WHERE SystemID IN (`+
			prepareVariadic("?", ",", len(systemIDs))+`)`, args...)
		if err != nil {
			return fmt.Errorf("resolve linked media systems: %w", err)
		}
		if len(systemDBIDs) == 0 {
			return nil
		}
	}
	hasLinks, err := sqlSystemHasMediaLinks(ctx, conn, systemDBIDs)
	if err != nil || !hasLinks {
		return err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sync linked media: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := sqlSyncMediaLinks(ctx, tx, systemDBIDs, false)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("sync linked media: commit: %w", err)
	}
	committed = true
	if !result.changed() {
		return nil
	}

	if len(result.propertySystems) > 0 {
		clearCoverAvailabilityCacheFor(conn)
		systems, sysErr := db.systemIDsForDBIDs(ctx, result.propertySystems)
		if sysErr != nil {
			db.requireFullScrapeImageInvalidation(
				sysErr, "failed to resolve systems with changed linked images; full invalidation required")
		}
		for _, systemID := range systems {
			db.recordScrapeImageSystem(systemID)
		}
	}
	if len(result.tagSystems) > 0 {
		systems, sysErr := db.systemIDsForDBIDs(ctx, result.tagSystems)
		if sysErr != nil {
			log.Warn().Err(sysErr).Msg("failed to resolve systems with changed linked tags")
			db.MarkScrapeTagCacheStale(nil)
		} else {
			db.addScrapeTagSystems(systems)
		}
	}
	if len(result.tagTitles) > 0 {
		if disErr := db.RecomputeTitleDisambiguation(ctx, result.tagTitles); disErr != nil {
			log.Warn().Err(disErr).Msg("failed to recompute title disambiguation after linked media sync")
		}
	}
	log.Debug().
		Int("propertySystems", len(result.propertySystems)).
		Int("tagSystems", len(result.tagSystems)).
		Int("tagTitles", len(result.tagTitles)).
		Msg("synced linked media")
	return nil
}

func (db *MediaDB) systemIDsForDBIDs(ctx context.Context, systemDBIDs []int64) ([]string, error) {
	args := make([]any, 0, len(systemDBIDs))
	for _, id := range systemDBIDs {
		args = append(args, id)
	}
	rows, err := db.sql.Load().QueryContext(ctx, `SELECT SystemID FROM Systems WHERE DBID IN (`+
		prepareVariadic("?", ",", len(systemDBIDs))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query systems: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var systems []string
	for rows.Next() {
		var systemID string
		if err := rows.Scan(&systemID); err != nil {
			return nil, fmt.Errorf("scan system: %w", err)
		}
		systems = append(systems, systemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate systems: %w", err)
	}
	return systems, nil
}

// GetMediaLinkTarget returns the media a linked row is an alias of.
func (db *MediaDB) GetMediaLinkTarget(
	ctx context.Context, mediaDBID int64,
) (targetDBID int64, linked bool, err error) {
	conn := db.sql.Load()
	if conn == nil {
		return 0, false, ErrNullSQL
	}
	err = conn.QueryRowContext(ctx,
		`SELECT TargetMediaDBID FROM MediaLinks WHERE MediaDBID = ?`, mediaDBID).Scan(&targetDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get media link target: %w", err)
	}
	return targetDBID, true, nil
}

// mediaLinkFilter is a predicate excluding linked rows, for the media streams
// scrapers select from. Empty when no link exists, so every other install runs
// its queries unchanged.
func mediaLinkFilter(ctx context.Context, db sqlQueryable, mediaAlias string) (string, error) {
	hasLinks, err := sqlSystemHasMediaLinks(ctx, db, nil)
	if err != nil || !hasLinks {
		return "", err
	}
	return " AND NOT EXISTS (SELECT 1 FROM MediaLinks ml WHERE ml.MediaDBID = " + mediaAlias + ".DBID)", nil
}

// titleLinkFilter is a predicate keeping only titles with a present row that
// is not a link, for the title streams scrapers select from. Empty when no
// link exists.
func titleLinkFilter(ctx context.Context, db sqlQueryable, titleAlias string) (string, error) {
	hasLinks, err := sqlSystemHasMediaLinks(ctx, db, nil)
	if err != nil || !hasLinks {
		return "", err
	}
	return strings.Join([]string{
		" AND EXISTS (SELECT 1 FROM Media lm WHERE lm.MediaTitleDBID = ", titleAlias, ".DBID",
		" AND lm.IsMissing = 0 AND NOT EXISTS (SELECT 1 FROM MediaLinks ml WHERE ml.MediaDBID = lm.DBID))",
	}, ""), nil
}
