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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/jonboulle/clockwork"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemsQueriesMarkCorruption(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"indexed", "counts", "tagged"} {
		for _, cause := range []error{
			sqlite3.Error{Code: sqlite3.ErrCorrupt},
			sqlite3.Error{Code: sqlite3.ErrNotADB},
			sqlite3.Error{Code: sqlite3.ErrIoErr},
			context.Canceled,
		} {
			t.Run(method+"/"+cause.Error(), func(t *testing.T) {
				t.Parallel()
				conn, mock, err := testsqlmock.NewSQLMock()
				require.NoError(t, err)
				t.Cleanup(func() { _ = conn.Close() })
				db := &MediaDB{
					ctx: t.Context(), dbPath: filepath.Join(t.TempDir(), "media.db"),
					clock: clockwork.NewFakeClock(),
				}
				db.sql.Store(conn)
				if method != "tagged" {
					mock.ExpectQuery("SELECT Value FROM DBConfig").WillReturnError(sql.ErrNoRows)
				}
				if method == "indexed" {
					mock.ExpectPrepare("SELECT s.SystemID").WillReturnError(cause)
					_, err = db.IndexedSystems()
				} else {
					mock.ExpectQuery("SELECT Systems.SystemID").WillReturnError(cause)
					var tags []zapscript.TagFilter
					if method == "tagged" {
						tags = []zapscript.TagFilter{{Type: "region", Value: "usa"}}
					}
					_, err = db.SystemMediaCounts(t.Context(), tags, false)
				}
				require.ErrorIs(t, err, cause)
				assert.Equal(t, database.IsCorruptionError(cause), database.IsMarkedCorrupt(db.dbPath))
				assert.Nil(t, db.systemMediaCountsCache.Load())
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestIsCorruptionError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "corrupt sqlite code",
			err:  sqlite3.Error{Code: sqlite3.ErrCorrupt},
			want: true,
		},
		{
			name: "not a database sqlite code",
			err:  sqlite3.Error{Code: sqlite3.ErrNotADB},
			want: true,
		},
		{
			name: "wrapped corrupt sqlite code",
			err:  fmt.Errorf("during maintenance: %w", sqlite3.Error{Code: sqlite3.ErrCorrupt}),
			want: true,
		},
		{
			name: "malformed disk image message fallback",
			err:  errors.New("database disk image is malformed"),
			want: true,
		},
		{
			name: "not a database message fallback",
			err:  errors.New("file is not a database"),
			want: true,
		},
		{
			name: "other sqlite code",
			err:  sqlite3.Error{Code: sqlite3.ErrBusy},
			want: false,
		},
		{
			name: "ordinary error",
			err:  errors.New("temporary query failure"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, database.IsCorruptionError(tt.err))
		})
	}
}
