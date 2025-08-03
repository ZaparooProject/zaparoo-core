package mediadb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

// Queries go here to keep the interface clean

//go:embed migrations/*.sql
var migrationFiles embed.FS

const DBConfigLastGeneratedAt = "LastGeneratedAt"

func sqlMigrateUp(db *sql.DB) error {
	goose.SetBaseFS(migrationFiles)

	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("error setting goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("error running migrations up: %w", err)
	}

	return nil
}

func sqlAllocate(db *sql.DB) error {
	return sqlMigrateUp(db)
}

func sqlUpdateLastGenerated(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf(
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('%s', ?)",
			DBConfigLastGeneratedAt,
		),
		strconv.FormatInt(time.Now().Unix(), 10),
	)
	return err
}

func sqlGetLastGenerated(ctx context.Context, db *sql.DB) (time.Time, error) {
	var rawTimestamp string
	err := db.QueryRowContext(ctx,
		fmt.Sprintf(
			"SELECT Value FROM DBConfig WHERE Name = '%s'",
			DBConfigLastGeneratedAt,
		),
	).Scan(&rawTimestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, err
	}

	timestamp, err := strconv.Atoi(rawTimestamp)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(timestamp), 0), nil
}

func sqlIndexTables(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	create index if not exists mediatitles_slug_idx on MediaTitles (Slug);
	create index if not exists mediatitles_system_idx on MediaTitles (SystemDBID);
	create index if not exists media_mediatitle_idx on Media (MediaTitleDBID);
	create index if not exists tags_tag_idx on Tags (Tag);
	create index if not exists tags_tagtype_idx on Tags (TypeDBID);
	create index if not exists mediatags_media_idx on MediaTags (MediaDBID);
	create index if not exists mediatags_tag_idx on MediaTags (TagDBID);
	create index if not exists mediatitletags_mediatitle_idx on MediaTitleTags (MediaTitleDBID);
	create index if not exists mediatitletags_tag_idx on MediaTitleTags (TagDBID);
	create index if not exists supportingmedia_mediatitle_idx on SupportingMedia (MediaTitleDBID);
	create index if not exists supportingmedia_media_idx on SupportingMedia (MediaTitleDBID);
	create index if not exists supportingmedia_typetag_idx on SupportingMedia (TypeTagDBID);
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	return err
}

//goland:noinspection SqlWithoutWhere
func sqlTruncate(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	delete from Systems;
	delete from MediaTitles;
	delete from Media;
	delete from TagTypes;
	delete from Tags;
	delete from MediaTags;
	delete from MediaTitleTags;
	delete from SupportingMedia;
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	return err
}

func sqlVacuum(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	return err
}

func sqlBeginTransaction(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "BEGIN")
	return err
}

func sqlCommitTransaction(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "COMMIT")
	return err
}

func sqlFindSystem(ctx context.Context, db *sql.DB, system database.System) (database.System, error) {
	var row database.System
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Name
		from Systems
		where DBID = ?
		or SystemID = ?
		limit 1;
	`)
	if err != nil {
		return row, err
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		system.DBID,
		system.SystemID,
	).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	return row, err
}

func sqlInsertSystem(ctx context.Context, db *sql.DB, row database.System) (database.System, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		Systems
		(DBID, SystemID, Name)
		values (?, ?, ?)
	`)
	if err != nil {
		return row, err
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.SystemID,
		row.Name,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

func sqlFindMediaTitle(ctx context.Context, db *sql.DB, title database.MediaTitle) (database.MediaTitle, error) {
	var row database.MediaTitle
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemDBID, Slug, Name
		from MediaTitles
		where DBID = ?
		or Slug = ?
		LIMIT 1;
	`)
	if err != nil {
		return row, err
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		title.DBID,
		title.Slug,
	).Scan(
		&row.DBID,
		&row.SystemDBID,
		&row.Slug,
		&row.Name,
	)
	return row, err
}

func sqlInsertMediaTitle(ctx context.Context, db *sql.DB, row database.MediaTitle) (database.MediaTitle, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		MediaTitles
		(DBID, SystemDBID, Slug, Name)
		values (?, ?, ?, ?)
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.SystemDBID,
		row.Slug,
		row.Name,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

func sqlFindMedia(ctx context.Context, db *sql.DB, media database.Media) (database.Media, error) {
	var row database.Media
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaTitleDBID, Path
		from Media
		where DBID = ?
		or (
			MediaTitleDBID = ?
			and Path = ?
		)
		LIMIT 1;
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	err = stmt.QueryRowContext(ctx,
		media.DBID,
		media.MediaTitleDBID,
		media.Path,
	).Scan(
		&row.DBID,
		&row.MediaTitleDBID,
		&row.Path,
	)
	return row, err
}

func sqlInsertMedia(ctx context.Context, db *sql.DB, row database.Media) (database.Media, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		Media
		(DBID, MediaTitleDBID, Path)
		values (?, ?, ?)
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.MediaTitleDBID,
		row.Path,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

func sqlFindTagType(ctx context.Context, db *sql.DB, tagType database.TagType) (database.TagType, error) {
	var row database.TagType
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, Type
		from TagTypes
		where DBID = ?
		or Type = ?
		LIMIT 1;
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Type,
	).Scan(
		&row.DBID,
		&row.Type,
	)
	return row, err
}

func sqlInsertTagType(ctx context.Context, db *sql.DB, row database.TagType) (database.TagType, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		TagTypes
		(DBID, Type)
		values (?, ?)
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.Type,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

func sqlFindTag(ctx context.Context, db *sql.DB, tagType database.Tag) (database.Tag, error) {
	var row database.Tag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, TypeDBID, Tag
		from Tags
		where DBID = ?
		or Tag = ?
		LIMIT 1;
	`)
	// TODO: Add TagType dependency when unknown tags supported
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	err = stmt.QueryRowContext(ctx,
		tagType.DBID,
		tagType.Tag,
	).Scan(
		&row.DBID,
		&row.TypeDBID,
		&row.Tag,
	)
	return row, err
}

func sqlInsertTag(ctx context.Context, db *sql.DB, row database.Tag) (database.Tag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		Tags
		(DBID, TypeDBID, Tag)
		values (?, ?, ?)
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.TypeDBID,
		row.Tag,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

func sqlFindMediaTag(ctx context.Context, db *sql.DB, mediaTag database.MediaTag) (database.MediaTag, error) {
	var row database.MediaTag
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, MediaDBID, TagDBID
		from MediaTags
		where DBID = ?
		or (
			MediaDBID = ?
			and TagDBID = ?
		)
		LIMIT 1;
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	err = stmt.QueryRowContext(ctx,
		mediaTag.DBID,
		mediaTag.MediaDBID,
		mediaTag.TagDBID,
	).Scan(
		&row.DBID,
		&row.MediaDBID,
		&row.TagDBID,
	)
	return row, err
}

func sqlInsertMediaTag(ctx context.Context, db *sql.DB, row database.MediaTag) (database.MediaTag, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}
	stmt, err := db.PrepareContext(ctx, `
		insert into
		MediaTags
		(DBID, MediaDBID, TagDBID)
		values (?, ?, ?)
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return row, err
	}
	res, err := stmt.ExecContext(ctx,
		dbID,
		row.MediaDBID,
		row.TagDBID,
	)
	if err != nil {
		return row, err
	}
	lastID, err := res.LastInsertId()
	row.DBID = lastID
	return row, err
}

// Not in use
/*
func sqlCleanInactiveMedia(db *sql.DB) error {
	_, err := db.Exec(`
		delete from MediaTitles
		where DBID in (
			select MediaTitleDBID
			from Media
			where IsActive = 0
			group by MediaTitleDBID
		);

		delete from MediaTags
		where MediaDBID in (
			select DBID
			from Media
			where IsActive = 0
		);

		delete from Media
		where IsActive = 0;
	`)
	return err
}
*/

// return ?, ?,... based on count
func prepareVariadic(p, s string, c int) string {
	if c < 1 {
		return ""
	}
	q := make([]string, c)
	for i := range q {
		q[i] = p
	}
	return strings.Join(q, s)
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
		return results, err
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
		return results, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if err := rows.Scan(
			&result.SystemID,
			&result.Path,
		); err != nil {
			return results, err
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
		prepareVariadic(" Media.Path like ? ", " and ", len(parts))+
		` LIMIT 250
	`)
	if err != nil {
		return results, err
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
		return results, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		result := database.SearchResult{}
		if err := rows.Scan(
			&result.SystemID,
			&result.Path,
		); err != nil {
			return results, err
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

func sqlSystemIndexed(ctx context.Context, db *sql.DB, system systemdefs.System) bool {
	systemID := ""
	q, err := db.PrepareContext(ctx, `
		select
		SystemID
		from Systems
		where SystemID = ?;
	`)
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, system.ID).Scan(&systemID)
	if err != nil {
		return false
	}
	return systemID == system.ID
}

func sqlIndexedSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	list := make([]string, 0)

	q, err := db.PrepareContext(ctx, `
		select SystemID from Systems;
	`)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := ""
		if err := rows.Scan(&row); err != nil {
			return list, err
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlRandomGame(ctx context.Context, db *sql.DB, system systemdefs.System) (database.SearchResult, error) {
	var row database.SearchResult
	q, err := db.PrepareContext(ctx, `
		select
		Systems.SystemID, Media.Path
		from Media
		INNER JOIN MediaTitles on MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems on Systems.DBID = MediaTitles.SystemDBID
		where Systems.SystemID = ?
		ORDER BY RANDOM() LIMIT 1;
	`)
	if err != nil {
		return row, err
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, system.ID).Scan(
		&row.SystemID,
		&row.Path,
	)
	row.Name = helpers.FilenameFromPath(row.Path)
	return row, err
}
