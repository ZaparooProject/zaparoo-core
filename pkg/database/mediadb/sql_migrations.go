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

package mediadb

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func sqlMigrateUp(db *sql.DB, dbPath string) error {
	sidecarPath := schemaVersionSidecarPath(dbPath)
	if err := database.MigrateUp(db, migrationFiles, "migrations", dbPath, sidecarPath); err != nil {
		return fmt.Errorf("failed to run media database migrations: %w", err)
	}
	return nil
}

func sqlAllocate(db *sql.DB, dbPath string) error {
	return sqlMigrateUp(db, dbPath)
}

// schemaVersionSidecarPath returns the JSON sidecar path used by MigrateUp
// to record the last applied migration version. Returns "" when dbPath is
// empty so the fast path is disabled (e.g. in-memory test databases).
func schemaVersionSidecarPath(dbPath string) string {
	if dbPath == "" {
		return ""
	}
	return filepath.Join(
		filepath.Dir(dbPath),
		config.CacheDir,
		filepath.Base(dbPath)+".schema_version.json",
	)
}
