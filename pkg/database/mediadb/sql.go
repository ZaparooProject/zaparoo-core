package mediadb

import (
	"database/sql"
	"github.com/rs/zerolog/log"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

// Queries go here to keep the interface clean

const DBVersion string = "1.0"

func sqlAllocate(db *sql.DB) error {
	// ROWID is an internal subject to change on vacuum
	// DBID INTEGER PRIMARY KEY aliases ROWID and makes it
	// persistent between vacuums
	sqlStmt := `
	PRAGMA journal_mode = OFF;
	PRAGMA synchronous = OFF;

	drop table if exists DBInfo;
	create table DBInfo (
		DBID INTEGER PRIMARY KEY,
		Version text
	);

	insert into
	DBInfo
	(DBID, Version)
	values (1, ?);

	drop table if exists Systems;
	create table Systems (
		DBID INTEGER PRIMARY KEY,
		SystemID text unique not null,
		Name text not null
	);

	drop table if exists MediaTitles;
	create table MediaTitles (
		DBID INTEGER PRIMARY KEY,
		SystemDBID integer not null,
		Slug text not null,
		Name text not null
	);

	drop table if exists Media;
	create table Media (
		DBID INTEGER PRIMARY KEY,
		MediaTitleDBID integer not null,
		Path text not null
	);

	drop table if exists TagTypes;
	create table TagTypes (
		DBID INTEGER PRIMARY KEY,
		Type text unique not null
	);

	drop table if exists Tags;
	create table Tags (
		DBID INTEGER PRIMARY KEY,
		TypeDBID integer not null,
		Tag text not null
	);

	drop table if exists MediaTags;
	create table MediaTags (
		DBID INTEGER PRIMARY KEY,
		MediaDBID integer not null,
		TagDBID integer not null
	);

	drop table if exists MediaTitleTags;
	create table MediaTitleTags (
		DBID INTEGER PRIMARY KEY,
		TagDBID integer not null,
		MediaTitleDBID integer not null
	);

	drop table if exists SupportingMedia;
	create table SupportingMedia (
		DBID INTEGER PRIMARY KEY,
		MediaTitleDBID integer not null,
		TypeTagDBID integer not null,
		Path string not null,
		ContentType text not null,
		Binary blob
	);
	`
	_, err := db.Exec(sqlStmt, DBVersion)
	return err
}

func sqlIndexTables(db *sql.DB) error {
	sqlStmt := `
	create index mediatitles_slug_idx on MediaTitles (Slug);
	create index mediatitles_system_idx on MediaTitles (SystemDBID);
	create index media_mediatitle_idx on Media (MediaTitleDBID);
	create index tags_tag_idx on Tags (Tag);
	create index tags_tagtype_idx on Tags (TypeDBID);
	create index mediatags_media_idx on MediaTags (MediaDBID);
	create index mediatags_tag_idx on MediaTags (TagDBID);
	create index mediatitletags_mediatitle_idx on MediaTitleTags (MediaTitleDBID);
	create index mediatitletags_tag_idx on MediaTitleTags (TagDBID);
	create index supportingmedia_mediatitle_idx on SupportingMedia (MediaTitleDBID);
	create index supportingmedia_media_idx on SupportingMedia (MediaTitleDBID);
	create index supportingmedia_typetag_idx on SupportingMedia (TypeTagDBID);
	`
	_, err := db.Exec(sqlStmt)
	return err
}

//goland:noinspection SqlWithoutWhere
func sqlTruncate(db *sql.DB) error {
	// TODO: Consider deleting the sqlite db file and reallocating?
	sqlStmt := `
	delete from DBInfo;
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
	_, err := db.Exec(sqlStmt)
	return err
}

func sqlVacuum(db *sql.DB) error {
	sqlStmt := `
	vacuum;
	`
	_, err := db.Exec(sqlStmt)
	return err
}

func sqlBeginTransaction(db *sql.DB) error {
	_, err := db.Exec("BEGIN")
	return err
}

func sqlCommitTransaction(db *sql.DB) error {
	_, err := db.Exec("COMMIT")
	return err
}

func sqlFindSystem(db *sql.DB, system database.System) (database.System, error) {
	var row database.System
	stmt, err := db.Prepare(`
		select
		DBID, SystemID, Name
		from Systems
		where DBID = ?
		or SystemID = ?
		limit 1;
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
		system.DBID,
		system.SystemID,
	).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	return row, err
}

func sqlInsertSystem(db *sql.DB, row database.System) (database.System, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		Systems
		(DBID, SystemID, Name)
		values (?, ?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.SystemID,
		row.Name,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
	return row, err
}

func sqlFindMediaTitle(db *sql.DB, title database.MediaTitle) (database.MediaTitle, error) {
	var row database.MediaTitle
	stmt, err := db.Prepare(`
		select
		DBID, SystemDBID, Slug, Name
		from MediaTitles
		where DBID = ?
		or Slug = ?
		LIMIT 1;
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
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

func sqlInsertMediaTitle(db *sql.DB, row database.MediaTitle) (database.MediaTitle, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		MediaTitles
		(DBID, SystemDBID, Slug, Name)
		values (?, ?, ?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.SystemDBID,
		row.Slug,
		row.Name,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
	return row, err
}

func sqlFindMedia(db *sql.DB, media database.Media) (database.Media, error) {
	var row database.Media
	stmt, err := db.Prepare(`
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
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
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

func sqlInsertMedia(db *sql.DB, row database.Media) (database.Media, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		Media
		(DBID, MediaTitleDBID, Path)
		values (?, ?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.MediaTitleDBID,
		row.Path,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
	return row, err
}

func sqlFindTagType(db *sql.DB, tagType database.TagType) (database.TagType, error) {
	var row database.TagType
	stmt, err := db.Prepare(`
		select
		DBID, Type
		from TagTypes
		where DBID = ?
		or Type = ?
		LIMIT 1;
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
		tagType.DBID,
		tagType.Type,
	).Scan(
		&row.DBID,
		&row.Type,
	)
	return row, err
}

func sqlInsertTagType(db *sql.DB, row database.TagType) (database.TagType, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		TagTypes
		(DBID, Type)
		values (?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.Type,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
	return row, err
}

func sqlFindTag(db *sql.DB, tagType database.Tag) (database.Tag, error) {
	var row database.Tag
	stmt, err := db.Prepare(`
		select
		DBID, TypeDBID, Tag
		from Tags
		where DBID = ?
		or Tag = ?
		LIMIT 1;
	`)
	// TODO: Add TagType dependency when unknown tags supported
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
		tagType.DBID,
		tagType.Tag,
	).Scan(
		&row.DBID,
		&row.TypeDBID,
		&row.Tag,
	)
	return row, err
}

func sqlInsertTag(db *sql.DB, row database.Tag) (database.Tag, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		Tags
		(DBID, TypeDBID, Tag)
		values (?, ?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.TypeDBID,
		row.Tag,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
	return row, err
}

func sqlFindMediaTag(db *sql.DB, mediaTag database.MediaTag) (database.MediaTag, error) {
	var row database.MediaTag
	stmt, err := db.Prepare(`
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
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	err = stmt.QueryRow(
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

func sqlInsertMediaTag(db *sql.DB, row database.MediaTag) (database.MediaTag, error) {
	var DBID any = nil
	if row.DBID != 0 {
		DBID = row.DBID
	}
	stmt, err := db.Prepare(`
		insert into
		MediaTags
		(DBID, MediaDBID, TagDBID)
		values (?, ?, ?)
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return row, err
	}
	res, err := stmt.Exec(
		DBID,
		row.MediaDBID,
		row.TagDBID,
	)
	if err != nil {
		return row, err
	}
	lastId, err := res.LastInsertId()
	row.DBID = lastId
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
func prepareVariadic(p string, s string, c int) string {
	if c < 1 {
		return ""
	}
	q := make([]string, c)
	for i := range q {
		q[i] = p
	}
	return strings.Join(q, s)
}

func sqlSearchMediaPathExact(db *sql.DB, systems []systemdefs.System, path string) ([]database.SearchResult, error) {
	// query == path
	slug := utils.SlugifyPath(path)

	var results []database.SearchResult
	var args = make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	args = append(args, slug, path)
	stmt, err := db.Prepare(`
		select 
			Systems.SystemID,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		and MediaTitles.Slug = ?
		and Media.Path = ?
		LIMIT 1
	`)
	rows, err := stmt.Query(
		args...,
	)
	if err != nil {
		return results, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		result := database.SearchResult{}
		err := rows.Scan(
			&result.SystemID,
			&result.Path,
		)
		if err != nil {
			return results, err
		}
		result.Name = utils.FilenameFromPath(result.Path)
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSearchMediaPathParts(db *sql.DB, systems []systemdefs.System, parts []string) ([]database.SearchResult, error) {
	var results []database.SearchResult
	var args = make([]any, 0)
	for _, sys := range systems {
		args = append(args, sys.ID)
	}
	for _, p := range parts {
		args = append(args, "%"+p+"%")
	}
	stmt, err := db.Prepare(`
		select 
			Systems.SystemID,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemID IN (` +
		prepareVariadic("?", ",", len(systems)) +
		`)
		and ` +
		prepareVariadic(" Media.Path like ? ", " and ", len(parts)) +
		` LIMIT 250
	`)
	rows, err := stmt.Query(
		args...,
	)
	if err != nil {
		return results, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		result := database.SearchResult{}
		err := rows.Scan(
			&result.SystemID,
			&result.Path,
		)
		if err != nil {
			return results, err
		}
		result.Name = utils.FilenameFromPath(result.Path)
		results = append(results, result)
	}
	err = rows.Err()
	if err != nil {
		return results, err
	}
	return results, nil
}

func sqlSystemIndexed(db *sql.DB, system systemdefs.System) bool {
	systemId := ""
	q, err := db.Prepare(`
		select
		SystemID
		from Systems
		where SystemID = ?;
	`)
	defer func(q *sql.Stmt) {
		err := q.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(q)
	if err != nil {
		return false
	}
	err = q.QueryRow(system.ID).Scan(&systemId)
	if err != nil {
		return false
	}
	return systemId == system.ID
}

func sqlIndexedSystems(db *sql.DB) ([]string, error) {
	var list []string
	q, err := db.Prepare(`
		select SystemID from Systems;
	`)
	defer func(q *sql.Stmt) {
		err := q.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(q)
	rows, err := q.Query()
	if err != nil {
		return list, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		row := ""
		err := rows.Scan(&row)
		if err != nil {
			return list, err
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlRandomGame(db *sql.DB, system systemdefs.System) (database.SearchResult, error) {
	var row database.SearchResult
	q, err := db.Prepare(`
		select
		Systems.SystemID, Media.Path
		from Media
		INNER JOIN MediaTitles on MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems on Systems.DBID = MediaTitles.SystemDBID
		where Systems.SystemID = ?
		ORDER BY RANDOM() LIMIT 1;
	`)
	defer func(q *sql.Stmt) {
		err := q.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(q)
	if err != nil {
		return row, err
	}
	err = q.QueryRow(system.ID).Scan(
		&row.SystemID,
		&row.Path,
	)
	row.Name = utils.FilenameFromPath(row.Path)
	return row, err
}
