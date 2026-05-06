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

package database

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

var migrationMutex syncutil.Mutex

// gooseZerologAdapter implements goose.Logger interface to redirect
// goose output to zerolog instead of stdout
type gooseZerologAdapter struct{}

func (*gooseZerologAdapter) Printf(format string, v ...any) {
	log.Info().Msgf(format, v...)
}

func (*gooseZerologAdapter) Fatalf(format string, v ...any) {
	log.Fatal().Msgf(format, v...)
}

// MigrateUp provides thread-safe database migration using goose.
// It locks access to goose's global state to prevent race conditions
// between multiple databases setting their migration filesystems.
//
// dbPath and sidecarPath enable a fast path that skips goose's metadata
// queries (which on a cold SQLite file can cost hundreds of milliseconds)
// when a previously-written sidecar reports the live DB file is already at
// the latest embedded migration version. Pass both as empty strings to
// disable the sidecar fast path (e.g. in tests against in-memory DBs).
func MigrateUp(
	db *sql.DB,
	migrationFiles embed.FS,
	migrationDir, dbPath, sidecarPath string,
) error {
	if dbPath != "" && sidecarPath != "" {
		latestStart := time.Now()
		latest, latestErr := latestEmbeddedVersion(migrationFiles, migrationDir)
		log.Debug().Int64("duration_ms", time.Since(latestStart).Milliseconds()).
			Int64("latest", latest).
			Msg("latestEmbeddedVersion finished (fast path)")
		if latestErr == nil && latest > 0 {
			// Strict equality is load-bearing: an older binary running against
			// a DB last migrated by a newer binary sees v > latest, fails this
			// check, and falls through to CheckSchemaVersion which raises
			// ErrSchemaAhead. Do not relax to v >= latest without preserving
			// downgrade detection elsewhere.
			if v, ok := loadSchemaVersionSidecar(sidecarPath, dbPath); ok && v == latest {
				log.Debug().Int64("version", v).Str("path", sidecarPath).
					Msg("schema version sidecar match, skipping goose")
				return nil
			}
		}
	}

	log.Debug().Msg("waiting for migration mutex")
	migrationMutex.Lock()
	log.Debug().Msg("migration mutex acquired")
	defer func() {
		migrationMutex.Unlock()
		log.Debug().Msg("migration mutex released")
	}()

	log.Debug().Msg("setting up goose logger")
	// Set custom logger to redirect goose output to zerolog
	goose.SetLogger(&gooseZerologAdapter{})

	log.Debug().Msg("setting goose base filesystem")
	goose.SetBaseFS(migrationFiles)

	log.Debug().Msg("setting goose dialect to sqlite")
	dialectStart := time.Now()
	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("error setting goose dialect: %w", err)
	}
	log.Debug().Int64("duration_ms", time.Since(dialectStart).Milliseconds()).
		Msg("goose dialect set")

	// Check if the database schema is ahead of this binary. This happens
	// when switching from a newer binary (e.g. beta) to an older one.
	checkStart := time.Now()
	if err := CheckSchemaVersion(db, migrationFiles, migrationDir); err != nil {
		return err
	}
	log.Debug().Int64("duration_ms", time.Since(checkStart).Milliseconds()).
		Msg("schema version checked")

	log.Debug().Str("migration_dir", migrationDir).Msg("running goose up migrations")
	upStart := time.Now()
	if err := goose.Up(db, migrationDir); err != nil {
		return fmt.Errorf("error running migrations up: %w", err)
	}
	log.Debug().Int64("duration_ms", time.Since(upStart).Milliseconds()).
		Msg("goose up migrations finished")

	if dbPath != "" && sidecarPath != "" {
		latest, latestErr := latestEmbeddedVersion(migrationFiles, migrationDir)
		if latestErr == nil && latest > 0 {
			if writeErr := writeSchemaVersionSidecar(sidecarPath, dbPath, latest); writeErr != nil {
				log.Warn().Err(writeErr).Str("path", sidecarPath).
					Msg("failed to write schema version sidecar")
			}
		}
	}

	return nil
}

// schemaVersionSidecar is the on-disk record of the last successfully
// applied migration version. The DB file mtime+size act as a tamper check:
// any change to the live DB file (manual goose run, restored backup,
// swapped file) invalidates the sidecar and forces a full goose check.
type schemaVersionSidecar struct {
	SchemaVersion int64 `json:"schemaVersion"`
	DBMtimeUnixNs int64 `json:"dbMtimeUnixNs"`
	DBSizeBytes   int64 `json:"dbSizeBytes"`
}

// loadSchemaVersionSidecar returns the recorded schema version when the
// sidecar exists and the live DB file's mtime and size match the values
// captured at write time. Any deviation is treated as "no sidecar" so the
// caller falls back to the full goose path.
// schemaVersionSidecarMaxBytes caps the sidecar read so a malformed or
// malicious file in the data directory cannot OOM the daemon at boot. The
// real payload is ~80 bytes; 64 KiB leaves ample headroom while staying far
// below any plausible memory pressure.
const schemaVersionSidecarMaxBytes = 64 << 10

func loadSchemaVersionSidecar(sidecarPath, dbPath string) (int64, bool) {
	f, err := os.Open(sidecarPath) //nolint:gosec // path is derived from the DB path
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, schemaVersionSidecarMaxBytes))
	if err != nil {
		return 0, false
	}
	var s schemaVersionSidecar
	if jsonErr := json.Unmarshal(data, &s); jsonErr != nil {
		log.Warn().Err(jsonErr).Str("path", sidecarPath).
			Msg("schema version sidecar malformed, ignoring")
		return 0, false
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		return 0, false
	}
	if info.ModTime().UnixNano() != s.DBMtimeUnixNs || info.Size() != s.DBSizeBytes {
		return 0, false
	}
	return s.SchemaVersion, true
}

// writeSchemaVersionSidecar persists the sidecar atomically (temp file +
// rename) after a successful goose.Up. The DB file's mtime and size are
// captured *after* goose.Up runs, so a no-op migration still produces a
// matching sidecar on the next boot.
func writeSchemaVersionSidecar(sidecarPath, dbPath string, version int64) error {
	info, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("stat db file: %w", err)
	}
	payload := schemaVersionSidecar{
		SchemaVersion: version,
		DBMtimeUnixNs: info.ModTime().UnixNano(),
		DBSizeBytes:   info.Size(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sidecar: %w", err)
	}
	dir := filepath.Dir(sidecarPath)
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return fmt.Errorf("create sidecar dir: %w", mkErr)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(sidecarPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create sidecar temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write sidecar temp file: %w", writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync sidecar temp file: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("close sidecar temp file: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, sidecarPath); renameErr != nil {
		cleanup()
		return fmt.Errorf("rename sidecar: %w", renameErr)
	}
	return nil
}

// ErrSchemaAhead is returned when the database schema version is newer than
// the binary supports. This happens when switching from a newer binary
// (e.g. beta) to an older one (e.g. stable).
var ErrSchemaAhead = errors.New("database schema is newer than this binary supports")

// CheckSchemaVersion compares the database's migration version against the
// latest migration embedded in the current binary. Returns ErrSchemaAhead
// if the database is ahead, preventing the older binary from running against
// an incompatible schema.
func CheckSchemaVersion(db *sql.DB, migrationFiles embed.FS, migrationDir string) error {
	getVersionStart := time.Now()
	dbVersion, err := goose.GetDBVersion(db)
	if err != nil {
		log.Warn().Err(err).
			Int64("duration_ms", time.Since(getVersionStart).Milliseconds()).
			Msg("goose.GetDBVersion failed")
		return fmt.Errorf("checking database schema version: %w", err)
	}
	log.Debug().Int64("duration_ms", time.Since(getVersionStart).Milliseconds()).
		Int64("db_version", dbVersion).
		Msg("goose.GetDBVersion finished")
	if dbVersion == 0 {
		return nil
	}

	latestStart := time.Now()
	latest, err := latestEmbeddedVersion(migrationFiles, migrationDir)
	if err != nil {
		log.Warn().Err(err).
			Int64("duration_ms", time.Since(latestStart).Milliseconds()).
			Msg("latestEmbeddedVersion failed")
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	log.Debug().Int64("duration_ms", time.Since(latestStart).Milliseconds()).
		Int64("latest", latest).
		Msg("latestEmbeddedVersion finished")

	if dbVersion > latest {
		return fmt.Errorf(
			"%w: database is at version %d but this binary only supports up to %d, "+
				"update to a newer version or reinstall the previous version",
			ErrSchemaAhead, dbVersion, latest,
		)
	}

	return nil
}

// latestEmbeddedVersion returns the highest goose version number from the
// embedded migration filenames (e.g. 20250605021915_init.sql).
func latestEmbeddedVersion(migrationFiles embed.FS, migrationDir string) (int64, error) {
	entries, err := fs.ReadDir(migrationFiles, migrationDir)
	if err != nil {
		return 0, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var maxVersion int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) == 0 {
			continue
		}

		version, parseErr := strconv.ParseInt(parts[0], 10, 64)
		if parseErr != nil {
			continue
		}

		if version > maxVersion {
			maxVersion = version
		}
	}

	if maxVersion == 0 {
		return 0, errors.New("no valid migration versions found in embedded files")
	}

	return maxVersion, nil
}
