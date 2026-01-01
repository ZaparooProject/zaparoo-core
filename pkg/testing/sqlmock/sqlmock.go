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

// Package sqlmock provides SQL mocking utilities for testing.
// This package is separate from helpers to avoid import cycles with database packages.
package sqlmock

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
)

// NewSQLMock creates a new SQL mock for testing database operations.
// Returns a mock database connection and a sqlmock.Sqlmock for setting expectations.
func NewSQLMock() (*sql.DB, sqlmock.Sqlmock, error) {
	return SetupSQLMock()
}

// SetupSQLMock creates a sqlmock with regex query matching enabled.
func SetupSQLMock() (*sql.DB, sqlmock.Sqlmock, error) {
	db, mockDB, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sqlmock: %w", err)
	}
	return db, mockDB, nil
}

// SetupSQLMockWithExpectations creates a sqlmock with common expectations.
func SetupSQLMockWithExpectations() (*sql.DB, sqlmock.Sqlmock, error) {
	db, mockDB, err := SetupSQLMock()
	if err != nil {
		return nil, nil, err
	}

	// Common expectations that most tests might need
	mockDB.ExpectPing()

	return db, mockDB, nil
}
