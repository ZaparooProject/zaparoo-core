package userdb

import (
	"context"
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
func sqlTruncate(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	delete from History;
	delete from Mappings;
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

func sqlAddHistory(ctx context.Context, db *sql.DB, entry database.HistoryEntry) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into History(
			Time, Type, TokenID, TokenValue, TokenData, Success
		) values (?, ?, ?, ?, ?, ?);
	`)
	defer func(stmt *sql.Stmt) {
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.ExecContext(ctx,
		entry.Time.Unix(),
		entry.Type,
		entry.TokenID,
		entry.TokenValue,
		entry.TokenData,
		entry.Success,
	)
	return err
}

func sqlGetHistoryWithOffset(ctx context.Context, db *sql.DB, lastID int) ([]database.HistoryEntry, error) {
	var list []database.HistoryEntry
	// Instead of offset, use token-based
	if lastID == 0 {
		lastID = 2147483646
	}

	q, err := db.PrepareContext(ctx, `
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
		closeErr := q.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(q)

	rows, err := q.QueryContext(ctx, lastID)
	if err != nil {
		return list, err
	}
	defer func(rows *sql.Rows) {
		closeErr := rows.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		row := database.HistoryEntry{}
		var timeInt int64
		scanErr := rows.Scan(
			&row.DBID,
			&timeInt,
			&row.Type,
			&row.TokenID,
			&row.TokenValue,
			&row.TokenData,
			&row.Success,
		)
		if scanErr != nil {
			return list, scanErr
		}
		row.Time = time.Unix(timeInt, 0)
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlAddMapping(ctx context.Context, db *sql.DB, m database.Mapping) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into Mappings(
			Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?);
	`)
	defer func(stmt *sql.Stmt) {
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.ExecContext(ctx,
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

func sqlGetMapping(ctx context.Context, db *sql.DB, id int64) (database.Mapping, error) {
	var row database.Mapping
	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where DBID = ?;
	`)
	defer func(q *sql.Stmt) {
		closeErr := q.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(q)
	if err != nil {
		return row, err
	}
	err = q.QueryRowContext(ctx, id).Scan(
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

func sqlDeleteMapping(ctx context.Context, db *sql.DB, id int64) error {
	stmt, err := db.PrepareContext(ctx, `
		delete from Mappings where DBID = ?;
	`)
	defer func(stmt *sql.Stmt) {
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.ExecContext(ctx, id)
	return err
}

func sqlUpdateMapping(ctx context.Context, db *sql.DB, id int64, m database.Mapping) error {
	stmt, err := db.PrepareContext(ctx, `
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
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)
	if err != nil {
		return err
	}
	_, err = stmt.ExecContext(ctx,
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

func sqlGetAllMappings(ctx context.Context, db *sql.DB) ([]database.Mapping, error) {
	var list []database.Mapping

	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)
	if err != nil {
		return list, err
	}
	defer func(q *sql.Stmt) {
		closeErr := q.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(q)

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, err
	}
	defer func(rows *sql.Rows) {
		closeErr := rows.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		row := database.Mapping{}
		scanErr := rows.Scan(
			&row.DBID,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if scanErr != nil {
			return list, scanErr
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlGetEnabledMappings(ctx context.Context, db *sql.DB) ([]database.Mapping, error) {
	var list []database.Mapping

	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Enabled = ?
	`)
	if err != nil {
		return list, err
	}
	defer func(q *sql.Stmt) {
		closeErr := q.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(q)

	rows, err := q.QueryContext(ctx, true)
	if err != nil {
		return list, err
	}
	defer func(rows *sql.Rows) {
		closeErr := rows.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}(rows)
	for rows.Next() {
		row := database.Mapping{}
		scanErr := rows.Scan(
			&row.DBID,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if scanErr != nil {
			return list, scanErr
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlUpdateZapLinkHost(ctx context.Context, db *sql.DB, host string, zapscript int) error {
	stmt, err := db.PrepareContext(ctx, `
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
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)

	_, err = stmt.ExecContext(ctx, host, zapscript)
	return err
}

func sqlGetZapLinkHost(ctx context.Context, db *sql.DB, host string) (supported, ok bool, err error) {
	row := db.QueryRowContext(ctx, `
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

func sqlUpdateZapLinkCache(ctx context.Context, db *sql.DB, url, zapscript string) error {
	stmt, err := db.PrepareContext(ctx, `
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
		closeErr := stmt.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}(stmt)

	_, err = stmt.ExecContext(ctx, url, zapscript)
	return err
}

func sqlGetZapLinkCache(ctx context.Context, db *sql.DB, url string) (string, error) {
	var zapscript string
	err := db.QueryRowContext(ctx,
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
