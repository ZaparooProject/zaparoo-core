package userdb

import (
	"database/sql"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
)

var ErrorNullSql = errors.New("UserDB is not connected")

type UserDB struct {
	sql *sql.DB
	pl  platforms.Platform
}

func OpenUserDB(pl platforms.Platform) (*UserDB, error) {
	db := &UserDB{sql: nil, pl: pl}
	err := db.Open()
	return db, err
}

func (db *UserDB) Open() error {
	exists := true
	dbPath := db.GetDBPath()
	_, err := os.Stat(dbPath)
	if err != nil {
		exists = false
		err := os.MkdirAll(filepath.Dir(dbPath), 0755)
		if err != nil {
			return err
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
	return filepath.Join(utils.DataDir(db.pl), config.UserDbFile)
}

func (db *UserDB) UnsafeGetSqlDb() *sql.DB {
	return db.sql
}

func (db *UserDB) Truncate() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlTruncate(db.sql)
}

func (db *UserDB) Allocate() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlAllocate(db.sql)
}

func (db *UserDB) MigrateUp() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlMigrateUp(db.sql)
}

func (db *UserDB) Vacuum() error {
	if db.sql == nil {
		return ErrorNullSql
	}
	return sqlVacuum(db.sql)
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
	return sqlAddHistory(db.sql, entry)
}

func (db *UserDB) GetHistory(lastId int) ([]database.HistoryEntry, error) {
	return sqlGetHistoryWithOffset(db.sql, lastId)
}

func (db *UserDB) UpdateZapLinkHost(host string, zapscript int) error {
	return sqlUpdateZapLinkHost(db.sql, host, zapscript)
}

func (db *UserDB) GetZapLinkHost(host string) (bool, bool, error) {
	return sqlGetZapLinkHost(db.sql, host)
}

func (db *UserDB) UpdateZapLinkCache(url string, zapscript string) error {
	return sqlUpdateZapLinkCache(db.sql, url, zapscript)
}

func (db *UserDB) GetZapLinkCache(url string) (string, error) {
	return sqlGetZapLinkCache(db.sql, url)
}
