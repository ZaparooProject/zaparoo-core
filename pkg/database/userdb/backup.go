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

package userdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	backupDirName  = "backups"
	backupPrefix   = "backup-"
	backupExt      = ".db"
	autoBackupKeep = 3
)

func (db *UserDB) backupDir() string {
	return filepath.Join(filepath.Dir(db.GetDBPath()), backupDirName)
}

func backupName(manual bool, now time.Time) string {
	kind := "auto"
	if manual {
		kind = "manual"
	}
	return fmt.Sprintf("%s%s-%09d-%s%s",
		backupPrefix,
		now.UTC().Format("20060102-150405"),
		now.UTC().Nanosecond(),
		kind,
		backupExt,
	)
}

func isAutoBackupName(name string) bool {
	return strings.HasPrefix(name, backupPrefix) &&
		strings.HasSuffix(name, "-auto"+backupExt)
}

func isBackupName(name string) bool {
	return strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, backupExt)
}

func backupInfo(path string, quickCheck bool) (database.BackupInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return database.BackupInfo{}, fmt.Errorf("failed to stat user database backup: %w", err)
	}
	result := database.BackupInfo{
		Name:      filepath.Base(path),
		Path:      path,
		CreatedAt: info.ModTime(),
		Size:      info.Size(),
		Manual:    strings.HasSuffix(filepath.Base(path), "-manual"+backupExt),
	}
	if quickCheck {
		valid, check, checkErr := quickCheckDB(path)
		result.Valid = valid
		result.QuickCheck = check
		if checkErr != nil {
			result.QuickCheck = checkErr.Error()
		}
	} else {
		result.Valid = true
	}
	return result, nil
}

func quickCheckDB(path string) (valid bool, result string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checkDB, err := sql.Open("sqlite3", path+"?mode=ro&_query_only=ON&_mmap_size=0")
	if err != nil {
		return false, "", fmt.Errorf("failed to open backup for quick_check: %w", err)
	}
	defer func() { _ = checkDB.Close() }()

	if err = checkDB.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return false, "", fmt.Errorf("backup quick_check failed: %w", err)
	}
	return result == "ok", result, nil
}

func (db *UserDB) pruneAutoBackups() error {
	backups, err := db.ListBackups()
	if err != nil {
		return err
	}
	autoBackups := make([]database.BackupInfo, 0, len(backups))
	for _, backup := range backups {
		if isAutoBackupName(backup.Name) {
			autoBackups = append(autoBackups, backup)
		}
	}
	if len(autoBackups) <= autoBackupKeep {
		return nil
	}
	sort.Slice(autoBackups, func(i, j int) bool {
		return autoBackups[i].CreatedAt.After(autoBackups[j].CreatedAt)
	})
	for _, backup := range autoBackups[autoBackupKeep:] {
		if err := os.Remove(backup.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to prune old user database backup %s: %w", backup.Path, err)
		}
	}
	return nil
}

func (db *UserDB) Backup(reason string, manual bool) (database.BackupInfo, error) {
	if db.sql.Load() == nil {
		return database.BackupInfo{}, ErrNullSQL
	}
	if err := os.MkdirAll(db.backupDir(), 0o750); err != nil {
		return database.BackupInfo{}, fmt.Errorf("failed to create user database backup directory: %w", err)
	}

	backupPath := filepath.Join(db.backupDir(), backupName(manual, time.Now()))
	if _, err := db.sql.Load().ExecContext(db.ctx, "VACUUM INTO ?", backupPath); err != nil {
		db.NoteCorruption(err)
		return database.BackupInfo{}, fmt.Errorf("failed to back up user database: %w", err)
	}

	info, err := backupInfo(backupPath, true)
	if err != nil {
		// The VACUUM INTO file exists but could not be inspected; don't leave it behind.
		_ = os.Remove(backupPath)
		return database.BackupInfo{}, err
	}
	info.Reason = reason
	if !info.Valid {
		// A backup that fails quick_check is useless; remove it rather than leaving an
		// invalid file to be re-checked on every ListBackups.
		_ = os.Remove(backupPath)
		return info, fmt.Errorf("user database backup failed validation: %s", info.QuickCheck)
	}
	if !manual {
		if pruneErr := db.pruneAutoBackups(); pruneErr != nil {
			return info, pruneErr
		}
	}
	log.Info().
		Str("path", info.Path).
		Int64("size", info.Size).
		Bool("manual", manual).
		Str("reason", reason).
		Msg("created user database backup")
	return info, nil
}

func (db *UserDB) EnsureRecentBackup(maxAge time.Duration) (database.BackupInfo, bool, error) {
	backups, err := db.ListBackups()
	if err != nil {
		return database.BackupInfo{}, false, err
	}
	for _, backup := range backups {
		if !backup.Valid {
			continue
		}
		if time.Since(backup.CreatedAt) <= maxAge {
			return backup, false, nil
		}
		break
	}
	backup, err := db.Backup("scheduled", false)
	return backup, err == nil, err
}

func (db *UserDB) ListBackups() ([]database.BackupInfo, error) {
	entries, err := os.ReadDir(db.backupDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list user database backups: %w", err)
	}
	backups := make([]database.BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isBackupName(entry.Name()) {
			continue
		}
		info, infoErr := backupInfo(filepath.Join(db.backupDir(), entry.Name()), true)
		if infoErr != nil {
			log.Warn().Err(infoErr).Str("name", entry.Name()).Msg("failed to inspect user database backup")
			continue
		}
		backups = append(backups, info)
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (db *UserDB) resolveBackupPath(name string) (string, error) {
	base := filepath.Base(name)
	if base != name || !isBackupName(base) {
		return "", fmt.Errorf("invalid user database backup name: %s", name)
	}
	path := filepath.Join(db.backupDir(), base)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("failed to find user database backup: %w", err)
	}
	return path, nil
}

func copyFileSync(src, dst string, mode os.FileMode) error {
	//nolint:gosec // src is selected by backup validation/recovery code, not raw user input.
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	//nolint:gosec // dst is the known user database path controlled by this package.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to open destination file: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("failed to copy file: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("failed to sync copied file: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close copied file: %w", closeErr)
	}
	return nil
}

func replaceDatabaseFromBackup(fs afero.Fs, backupPath, dbPath string) (err error) {
	tmp, err := afero.TempFile(fs, filepath.Dir(dbPath), ".userdb-restore-*")
	if err != nil {
		return fmt.Errorf("failed to create staged restore file: %w", err)
	}
	tmpPath := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		_ = fs.Remove(tmpPath)
		return fmt.Errorf("failed to close staged restore file: %w", closeErr)
	}
	defer func() { _ = fs.Remove(tmpPath) }()

	if err = copyFileSync(backupPath, tmpPath, 0o600); err != nil {
		return fmt.Errorf("failed to stage user database backup: %w", err)
	}

	rollbackPath := dbPath + ".restore-rollback"
	_ = fs.Remove(rollbackPath)
	if err = fs.Rename(dbPath, rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to preserve user database before restore: %w", err)
	}
	originalPreserved := err == nil

	database.RemoveSidecars(dbPath)
	if err = fs.Rename(tmpPath, dbPath); err != nil {
		if originalPreserved {
			if rollbackErr := fs.Rename(rollbackPath, dbPath); rollbackErr != nil {
				return errors.Join(
					fmt.Errorf("failed to install staged user database backup: %w", err),
					fmt.Errorf("failed to restore original user database: %w", rollbackErr),
				)
			}
		}
		return fmt.Errorf("failed to install staged user database backup: %w", err)
	}
	_ = fs.Remove(rollbackPath)
	return nil
}

func (db *UserDB) RestoreBackup(name string) (database.RestoreInfo, error) {
	backupPath, err := db.resolveBackupPath(name)
	if err != nil {
		return database.RestoreInfo{}, err
	}
	backup, err := backupInfo(backupPath, true)
	if err != nil {
		return database.RestoreInfo{}, err
	}
	if !backup.Valid {
		return database.RestoreInfo{}, fmt.Errorf("refusing to restore invalid backup: %s", backup.QuickCheck)
	}

	var preRestore *database.BackupInfo
	if db.sql.Load() != nil {
		// Created as an auto backup so the standard retention policy prunes it;
		// otherwise every restore would leave a permanent backup behind. It is the
		// newest backup when written, so a failed restore still recovers from it.
		pre, backupErr := db.Backup("pre-restore", false)
		if backupErr != nil {
			log.Warn().Err(backupErr).Msg("failed to create pre-restore user database backup")
		} else {
			preRestore = &pre
		}
	}

	if closeErr := db.Close(); closeErr != nil {
		return database.RestoreInfo{}, closeErr
	}
	if err = replaceDatabaseFromBackup(afero.NewOsFs(), backupPath, db.GetDBPath()); err != nil {
		return db.restoreFailed(fmt.Errorf("failed to restore user database backup: %w", err))
	}
	if err = db.Open(); err != nil {
		return db.restoreFailed(fmt.Errorf("failed to reopen restored user database: %w", err))
	}
	if err = db.MigrateUp(); err != nil {
		return db.restoreFailed(fmt.Errorf("failed to migrate restored user database: %w", err))
	}
	result := database.RestoreInfo{RestoredFrom: backup, PreRestoreBackup: preRestore}
	if err = db.ClearCorruptMarker(); err != nil {
		return result, fmt.Errorf("failed to clear user database corrupt marker after restore: %w", err)
	}

	return result, nil
}

// restoreFailed leaves the user database connection usable after a restore step
// fails once the live connection has already been closed. It runs the
// corruption-recovery flow, which preserves the bad file and reopens from a valid
// backup — including the pre-restore backup taken at the start of RestoreBackup —
// so subsequent UserDB operations don't all fail with ErrNullSQL. The original
// restore error is returned regardless of recovery's outcome.
func (db *UserDB) restoreFailed(cause error) (database.RestoreInfo, error) {
	if _, recoverErr := db.RecoverFromCorruption(); recoverErr != nil {
		log.Error().Err(recoverErr).Msg("failed to recover user database after restore failure")
	}
	return database.RestoreInfo{}, cause
}

func preserveCorruptFile(path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to stat corrupt user database file")
		return
	}
	backup := path + database.CorruptMarkerSuffix + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to preserve corrupt user database file")
	}
}

func (db *UserDB) RecoverFromCorruption() (database.RestoreInfo, error) {
	if err := db.Close(); err != nil {
		log.Warn().Err(err).Msg("error closing corrupt user database before recovery")
	}
	for _, path := range []string{db.GetDBPath(), db.GetDBPath() + "-wal", db.GetDBPath() + "-shm"} {
		preserveCorruptFile(path)
	}

	backups, err := db.ListBackups()
	if err != nil {
		return database.RestoreInfo{}, err
	}
	for _, backup := range backups {
		if !backup.Valid {
			continue
		}
		if err = copyFileSync(backup.Path, db.GetDBPath(), 0o600); err != nil {
			log.Warn().Err(err).Str("path", backup.Path).Msg("failed to restore user database backup")
			continue
		}
		if err = db.Open(); err != nil {
			return database.RestoreInfo{}, fmt.Errorf("failed to reopen restored user database: %w", err)
		}
		if err = db.MigrateUp(); err != nil {
			return database.RestoreInfo{}, fmt.Errorf("failed to migrate restored user database: %w", err)
		}
		if err = db.ClearCorruptMarker(); err != nil {
			return database.RestoreInfo{}, fmt.Errorf(
				"failed to clear user database corrupt marker after recovery: %w", err,
			)
		}
		log.Warn().Str("backup", backup.Path).Msg("restored user database from backup after corruption")
		return database.RestoreInfo{RestoredFrom: backup}, nil
	}

	database.RemoveSidecars(db.GetDBPath())
	if err = db.Open(); err != nil {
		return database.RestoreInfo{}, fmt.Errorf(
			"failed to create fresh user database after corruption: %w", err,
		)
	}
	if err = db.MigrateUp(); err != nil {
		return database.RestoreInfo{}, fmt.Errorf(
			"failed to migrate fresh user database after corruption: %w", err,
		)
	}
	if err = db.ClearCorruptMarker(); err != nil {
		return database.RestoreInfo{}, fmt.Errorf(
			"failed to clear user database corrupt marker after fresh recovery: %w", err,
		)
	}
	log.Warn().Msg("created fresh user database after corruption; no valid backup was available")
	return database.RestoreInfo{}, nil
}

func (db *UserDB) MarkCorrupt(reason string) {
	database.MarkCorrupt(db.GetDBPath(), reason, time.Now())
}

func (db *UserDB) IsMarkedCorrupt() bool {
	return database.IsMarkedCorrupt(db.GetDBPath())
}

func (db *UserDB) ClearCorruptMarker() error {
	if err := database.ClearCorruptMarker(db.GetDBPath()); err != nil {
		return fmt.Errorf("user database: %w", err)
	}
	return nil
}

func (db *UserDB) NoteCorruption(err error) bool {
	return database.NoteCorruption(db.GetDBPath(), err, time.Now())
}

func (db *UserDB) IntegrityReport() []string {
	if db.sql.Load() == nil {
		return []string{"integrity check unavailable: user database not connected"}
	}
	return database.IntegrityReport(db.ctx, db.sql.Load(), database.DefaultIntegrityReportRows)
}
