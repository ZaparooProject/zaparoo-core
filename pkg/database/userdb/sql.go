package userdb

import (
	"database/sql"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
)

// Queries go here to keep the interface clean
const UserDBVersion string = "1.0"

func sqlAllocate(db *sql.DB) error {
	// ROWID is an internal subject to change on vacuum
	// DBID INTEGER PRIMARY KEY aliases ROWID and makes it
	// persistent between vacuums
	sqlStmt := `	
	drop table if exists DBInfo;
	create table DBInfo (
		DBID INTEGER PRIMARY KEY,
		Version text
	);

	insert into
	DBInfo
	(DBID, Version)
	values (1, ?);

	drop table if exists History;
	create table History (
		DBID INTEGER PRIMARY KEY,
		Time integer not null,
		Type text not null,
		UID text not null,
		Text text not null,
		Data text not null,
		Success integer not null
	);

	drop table if exists Mappings;
	create table Mappings (
		DBID INTEGER PRIMARY KEY,
		Id text not null,
		Added integer not null,
		Label text not null,
		Enabled integer not null,
		Type text not null,
		Match text not null,
		Pattern text not null,
		Override text not null
	);
	`
	_, err := db.Exec(sqlStmt, UserDBVersion)
	return err
}

func sqlTruncate(db *sql.DB) error {
	sqlStmt := `
	delete from History;
	delete from Mappings;
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

func sqlAddHistory(db *sql.DB, entry database.HistoryEntry) error {
	stmt, err := db.Prepare(`
		insert into History(
			Time, Type, UID, Text, Data, Success
		) values (?, ?, ?, ?, ?, ?);
	`)
	defer stmt.Close()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(
		entry.Time.Unix(),
		entry.Type,
		entry.UID,
		entry.Text,
		entry.Data,
		entry.Success,
	)
	return err
}

func sqlGetHistoryWithOffset(db *sql.DB, lastId int) ([]database.HistoryEntry, error) {
	var list []database.HistoryEntry
	// Instead of offset use token based
	if lastId == 0 {
		lastId = 2147483646
	}
	q, err := db.Prepare(`
		select 
		DBID, Time, Type, UID, Text, Data, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)
	defer q.Close()
	rows, err := q.Query(lastId)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		row := database.HistoryEntry{}
		var timeInt int64
		err := rows.Scan(
			&row.DBID,
			&timeInt,
			&row.Type,
			&row.UID,
			&row.Text,
			&row.Data,
			&row.Success,
		)
		if err != nil {
			return list, err
		}
		row.Time = time.Unix(timeInt, 0)
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlAddMapping(db *sql.DB, m database.Mapping) error {
	stmt, err := db.Prepare(`
		insert into Mappings(
			Id, Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?, ?);
	`)
	defer stmt.Close()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(
		m.Id,
		m.Added,
		m.Label,
		m.Enabled,
		m.Type,
		m.Match,
		m.Pattern,
		m.Override,
	)
	return err
}

func sqlGetMapping(db *sql.DB, id string) (database.Mapping, error) {
	var row database.Mapping
	q, err := db.Prepare(`
		select
		DBID, Id, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Id = ?;
	`)
	defer q.Close()
	if err != nil {
		return row, err
	}
	err = q.QueryRow(id).Scan(
		&row.DBID,
		&row.Id,
		&row.Added,
		&row.Label,
		&row.Enabled,
		&row.Type,
		&row.Match,
		&row.Pattern,
		&row.Override,
	)
	return row, err
}

func sqlDeleteMapping(db *sql.DB, id string) error {
	stmt, err := db.Prepare(`
		delete from Mappings where Id = ?;
	`)
	defer stmt.Close()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(id)
	return err
}

func sqlUpdateMapping(db *sql.DB, id string, m database.Mapping) error {
	stmt, err := db.Prepare(`
		update Mappings set
			Added = ?,
			Label = ?,
			Enabled = ?,
			Type = ?,
			Match = ?,
			Pattern = ?,
			Override = ?
		where
			Id = ?;
	`)
	defer stmt.Close()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(
		m.Added,
		m.Label,
		m.Enabled,
		m.Type,
		m.Match,
		m.Pattern,
		m.Override,
		id,
	)
	return err
}

func sqlGetAllMappings(db *sql.DB) ([]database.Mapping, error) {
	var list []database.Mapping
	q, err := db.Prepare(`
		select
		DBID, Id, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)
	defer q.Close()
	rows, err := q.Query()
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		row := database.Mapping{}
		err := rows.Scan(
			&row.DBID,
			&row.Id,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if err != nil {
			return list, err
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlGetEnabledMappings(db *sql.DB) ([]database.Mapping, error) {
	var list []database.Mapping
	q, err := db.Prepare(`
		select
		DBID, Id, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Enabled = ?
	`)
	defer q.Close()
	rows, err := q.Query(true)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		row := database.Mapping{}
		err := rows.Scan(
			&row.DBID,
			&row.Id,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if err != nil {
			return list, err
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}
