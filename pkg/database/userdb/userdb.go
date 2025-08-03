package userdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
)

var ErrorNullSQL = errors.New("UserDB is not connected")

type UserDB struct {
	sql *sql.DB
	pl  platforms.Platform
	ctx context.Context
}

func OpenUserDB(ctx context.Context, pl platforms.Platform) (*UserDB, error) {
	db := &UserDB{sql: nil, pl: pl, ctx: ctx}
	err := db.Open()
	return db, err
}

func (db *UserDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		mkdirErr := os.MkdirAll(filepath.Dir(dbPath), 0o755)
		if mkdirErr != nil {
			return mkdirErr
		}
	}
	sqlInstance, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	db.sql = sqlInstance
	if !exists {
		return db.Allocate()
	}
	return nil
}

func (db *UserDB) GetDBPath() string {
	return filepath.Join(helpers.DataDir(db.pl), config.UserDbFile)
}

func (db *UserDB) UnsafeGetSQLDb() *sql.DB {
	return db.sql
}

func (db *UserDB) Truncate() error {
	if db.sql == nil {
		return ErrorNullSQL
	}
	return sqlTruncate(db.ctx, db.sql)
}

func (db *UserDB) Allocate() error {
	if db.sql == nil {
		return ErrorNullSQL
	}
	return sqlAllocate(db.sql)
}

func (db *UserDB) MigrateUp() error {
	if db.sql == nil {
		return ErrorNullSQL
	}
	return sqlMigrateUp(db.sql)
}

func (db *UserDB) Vacuum() error {
	if db.sql == nil {
		return ErrorNullSQL
	}
	return sqlVacuum(db.ctx, db.sql)
}

func (db *UserDB) Close() error {
	if db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// TODO: reader source (physical reader vs web)
// TODO: metadata

func (db *UserDB) AddHistory(entry database.HistoryEntry) error {
	return sqlAddHistory(db.ctx, db.sql, entry)
}

func (db *UserDB) GetHistory(lastID int) ([]database.HistoryEntry, error) {
	return sqlGetHistoryWithOffset(db.ctx, db.sql, lastID)
}

func (db *UserDB) UpdateZapLinkHost(host string, zapscript int) error {
	return sqlUpdateZapLinkHost(db.ctx, db.sql, host, zapscript)
}

func (db *UserDB) GetZapLinkHost(host string) (exists, allowed bool, err error) {
	return sqlGetZapLinkHost(db.ctx, db.sql, host)
}

func (db *UserDB) UpdateZapLinkCache(url, zapscript string) error {
	return sqlUpdateZapLinkCache(db.ctx, db.sql, url, zapscript)
}

func (db *UserDB) GetZapLinkCache(url string) (string, error) {
	return sqlGetZapLinkCache(db.ctx, db.sql, url)
}
