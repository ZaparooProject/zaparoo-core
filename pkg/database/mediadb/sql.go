package mediadb

import (
	"database/sql"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
)

// Queries go here to keep the interface clean

func sqlAllocate(db *sql.DB) error {
	// ROWID is an internal subject to change on vacuum
	// DBID INTEGER PRIMARY KEY aliases ROWID and makes it
	// persistent between vacuums
	sqlStmt := `
	drop table if exists Systems;
	create table Systems (
		DBID INTEGER PRIMARY KEY,
		SystemId text unique not null,
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
	_, err := db.Exec(sqlStmt)
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
	create index supportingmedia_media_idx on SupportingMedia (MediaDBID);
	create index supportingmedia_typetag_idx on SupportingMedia (TypeTagDBID);
	vacuum;
	`
	_, err := db.Exec(sqlStmt)
	return err
}

func sqlTruncate(db *sql.DB) error {
	// Consider deleting sqlite db file and reallocating?
	sqlStmt := `
	delete from table Systems;
	delete from table MediaTitles;
	delete from table Media;
	delete from table TagTypes;
	delete from table Tags;
	delete from table MediaTags;
	delete from table MediaTitleTags;
	delete from table SupportingMedia;
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

func sqlBulkInsertSystems(db *sql.DB, ss *database.ScanState) error {
	rows := ss.Systems
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			Systems
			(DBID, SystemId, Name)
			values (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.SystemId,
			row.Name,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

func sqlBulkInsertTitles(db *sql.DB, ss *database.ScanState) error {
	rows := ss.Titles
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			MediaTitles
			(DBID, SystemDBID, Slug, Name)
			values (?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.SystemDBID,
			row.Slug,
			row.Name,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

func sqlBulkInsertMedia(db *sql.DB, ss *database.ScanState) error {
	rows := ss.Media
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			Media
			(DBID, MediaTitleDBID, Path)
			values (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.MediaTitleDBID,
			row.Path,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

func sqlBulkInsertTagTypes(db *sql.DB, ss *database.ScanState) error {
	rows := ss.TagTypes
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			TagTypes
			(DBID, Type)
			values (?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.Type,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

func sqlBulkInsertTags(db *sql.DB, ss *database.ScanState) error {
	rows := ss.Tags
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			Tags
			(DBID, TypeDBID, Tag)
			values (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.TypeDBID,
			row.Tag,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

func sqlBulkInsertMediaTags(db *sql.DB, ss *database.ScanState) error {
	rows := ss.MediaTags
	var err error
	_, err = db.Exec("BEGIN")
	if err != nil {
		return err
	}
	for i, row := range rows {
		if row.DBID == 0 {
			continue
		}
		if i%1000 == 0 {
			_, err = db.Exec("COMMIT")
			_, err = db.Exec("BEGIN")
		}
		stmt, err := db.Prepare(`
			insert into
			MediaTags
			(DBID, MediaDBID, TagDBID)
			values (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		_, err = stmt.Exec(
			row.DBID,
			row.MediaDBID,
			row.TagDBID,
		)
		if err != nil {
			return err
		}
	}
	_, err = db.Exec("COMMIT")
	if err != nil {
		return err
	}
	return nil
}

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
			Systems.SystemId,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemId IN (` +
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
	defer rows.Close()
	for rows.Next() {
		result := database.SearchResult{}
		err := rows.Scan(
			&result.SystemId,
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
			Systems.SystemId,
			Media.Path
		from Systems
		inner join MediaTitles
			on Systems.DBID = MediaTitles.SystemDBID
		inner join Media
			on MediaTitles.DBID = Media.MediaTitleDBID
		where Systems.SystemId IN (` +
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
	defer rows.Close()
	for rows.Next() {
		result := database.SearchResult{}
		err := rows.Scan(
			&result.SystemId,
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
		SystemId
		from Systems
		where SystemId = ?;
	`)
	defer q.Close()
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
		select SystemId from Systems;
	`)
	defer q.Close()
	rows, err := q.Query()
	if err != nil {
		return list, err
	}
	defer rows.Close()
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
		Systems.SystemId, Media.Path
		from Media
		INNER JOIN MediaTitles on MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems on Systems.DBID = MediaTitles.SystemDBID
		where Systems.SystemId = ?
		ORDER BY RANDOM() LIMIT 1;
	`)
	defer q.Close()
	if err != nil {
		return row, err
	}
	err = q.QueryRow(system.ID).Scan(
		&row.SystemId,
		&row.Path,
	)
	row.Name = utils.FilenameFromPath(row.Path)
	return row, err
}
