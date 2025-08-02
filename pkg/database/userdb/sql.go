package userdb

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	goose "github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

// Queries go here to keep the interface clean

//go:embed migrations/*.sql
var migrationFiles embed.FS

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

//goland:noinspection SqlWithoutWhere
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
			Time, Type, TokenID, TokenValue, TokenData, Success
		) values (?, ?, ?, ?, ?, ?);
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(
		entry.Time.Unix(),
		entry.Type,
		entry.TokenID,
		entry.TokenValue,
		entry.TokenData,
		entry.Success,
	)
	return err
}

func sqlGetHistoryWithOffset(db *sql.DB, lastId int) ([]database.HistoryEntry, error) {
	var list []database.HistoryEntry
	// Instead of offset, use token-based
	if lastId == 0 {
		lastId = 2147483646
	}

	q, err := db.Prepare(`
		select 
		DBID, Time, Type, TokenID, TokenValue, TokenData, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)
	if err != nil {
		return list, err
	}
	defer func(q *sql.Stmt) {
		err := q.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(q)

	rows, err := q.Query(lastId)
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
		row := database.HistoryEntry{}
		var timeInt int64
		err := rows.Scan(
			&row.DBID,
			&timeInt,
			&row.Type,
			&row.TokenID,
			&row.TokenValue,
			&row.TokenData,
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
			Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?);
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
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
	)
	return err
}

func sqlGetMapping(db *sql.DB, id int64) (database.Mapping, error) {
	var row database.Mapping
	q, err := db.Prepare(`
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where DBID = ?;
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
	err = q.QueryRow(id).Scan(
		&row.DBID,
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

func sqlDeleteMapping(db *sql.DB, id int64) error {
	stmt, err := db.Prepare(`
		delete from Mappings where DBID = ?;
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(id)
	return err
}

func sqlUpdateMapping(db *sql.DB, id int64, m database.Mapping) error {
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
			DBID = ?;
	`)
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)
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
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)
	if err != nil {
		return list, err
	}
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
		row := database.Mapping{}
		err := rows.Scan(
			&row.DBID,
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
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Enabled = ?
	`)
	if err != nil {
		return list, err
	}
	defer func(q *sql.Stmt) {
		err := q.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(q)

	rows, err := q.Query(true)
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
		row := database.Mapping{}
		err := rows.Scan(
			&row.DBID,
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

func sqlUpdateZapLinkHost(db *sql.DB, host string, zapscript int) error {
	stmt, err := db.Prepare(`
		INSERT INTO ZapLinkHosts (Host, ZapScript, CheckedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(Host) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			CheckedAt = CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return err
	}
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)

	_, err = stmt.Exec(host, zapscript)
	return err
}

func sqlGetZapLinkHost(db *sql.DB, host string) (supported bool, ok bool, err error) {
	row := db.QueryRow(`
		SELECT ZapScript FROM ZapLinkHosts WHERE Host = ?;
	`, host)

	var zapscript int
	err = row.Scan(&zapscript)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}

	return zapscript != 0, true, nil
}

func sqlUpdateZapLinkCache(db *sql.DB, url string, zapscript string) error {
	stmt, err := db.Prepare(`
		INSERT INTO ZapLinkCache (URL, ZapScript, UpdatedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(URL) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			UpdatedAt = CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return err
	}
	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close sql statement")
		}
	}(stmt)

	_, err = stmt.Exec(url, zapscript)
	return err
}

func sqlGetZapLinkCache(db *sql.DB, url string) (string, error) {
	var zapscript string
	err := db.QueryRow(
		`SELECT ZapScript FROM ZapLinkCache WHERE URL = ?;`,
		url,
	).Scan(&zapscript)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return zapscript, nil
}
