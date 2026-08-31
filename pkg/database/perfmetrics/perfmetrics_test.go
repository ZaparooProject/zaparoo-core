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

package perfmetrics

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panicDBProvider struct{}

func (panicDBProvider) GetDBPath() string {
	panic("unexpected GetDBPath call")
}

func (panicDBProvider) UnsafeGetSQLDb() *sql.DB {
	panic("unexpected UnsafeGetSQLDb call")
}

func TestNewRecorderForDB_RecoversFromProviderPanic(t *testing.T) {
	t.Parallel()

	recorder := NewRecorderForDB(panicDBProvider{})

	assert.Equal(t, Recorder{}, recorder)
}

func TestReadDBStats_CombinesFileAndSQLiteStats(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	fs := afero.NewOsFs()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "media.db")
	require.NoError(t, writeSizedFile(fs, dbPath, 11))
	require.NoError(t, writeSizedFile(fs, dbPath+"-wal", 17))
	require.NoError(t, writeSizedFile(fs, dbPath+"-shm", 23))

	mock.ExpectQuery(`PRAGMA page_count;`).
		WillReturnRows(sqlmock.NewRows([]string{"page_count"}).AddRow(101))
	mock.ExpectQuery(`PRAGMA freelist_count;`).
		WillReturnRows(sqlmock.NewRows([]string{"freelist_count"}).AddRow(7))
	mock.ExpectQuery(`PRAGMA page_size;`).
		WillReturnRows(sqlmock.NewRows([]string{"page_size"}).AddRow(4096))
	mock.ExpectQuery(`PRAGMA cache_size;`).
		WillReturnRows(sqlmock.NewRows([]string{"cache_size"}).AddRow(-32768))
	mock.ExpectQuery(`PRAGMA wal_checkpoint\(NOOP\);`).
		WillReturnRows(sqlmock.NewRows([]string{"busy", "log", "checkpointed"}).AddRow(0, 12, 9))

	stats, ok := readDBStats(context.Background(), dbPath, db)

	require.True(t, ok)
	assert.Equal(t, int64(11), stats.DBBytes)
	assert.Equal(t, int64(17), stats.WALBytes)
	assert.Equal(t, int64(23), stats.SHMBytes)
	assert.Equal(t, int64(101), stats.PageCount)
	assert.Equal(t, int64(7), stats.FreelistCount)
	assert.Equal(t, int64(4096), stats.PageSize)
	assert.Equal(t, int64(-32768), stats.CacheSize)
	assert.Equal(t, int64(0), stats.WALBusy)
	assert.Equal(t, int64(12), stats.WALFrames)
	assert.Equal(t, int64(9), stats.WALCheckpointedFrames)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAddDelta_LogsProcessRuntimeAndDBDeltas(t *testing.T) {
	t.Parallel()

	start := Snapshot{
		At: time.Unix(100, 0),
		IO: ProcessIO{
			RChar:               100,
			WChar:               200,
			Syscr:               3,
			Syscw:               4,
			ReadBytes:           1000,
			WriteBytes:          2000,
			CancelledWriteBytes: 5,
		},
		Runtime: RuntimeStats{
			TotalAllocBytes: 10,
			Mallocs:         20,
			Frees:           5,
			GCCycles:        1,
			GCPauseNs:       100,
			CgoCalls:        7,
		},
		DB: DBStats{
			DBBytes:  100,
			WALBytes: 10,
			SHMBytes: 20,
		},
		IOOK: true,
		DBOK: true,
	}
	end := Snapshot{
		At: time.Unix(100, int64(50*time.Millisecond)),
		IO: ProcessIO{
			RChar:               150,
			WChar:               275,
			Syscr:               8,
			Syscw:               10,
			ReadBytes:           1300,
			WriteBytes:          2600,
			CancelledWriteBytes: 9,
		},
		Runtime: RuntimeStats{
			TotalAllocBytes: 40,
			HeapAllocBytes:  400,
			HeapSysBytes:    800,
			HeapObjects:     12,
			Mallocs:         29,
			Frees:           11,
			GCCycles:        3,
			GCPauseNs:       175,
			Goroutines:      4,
			CgoCalls:        17,
		},
		DB: DBStats{
			DBBytes:               125,
			WALBytes:              30,
			SHMBytes:              20,
			PageCount:             50,
			FreelistCount:         2,
			PageSize:              4096,
			CacheSize:             -32768,
			WALBusy:               0,
			WALFrames:             6,
			WALCheckpointedFrames: 5,
		},
		IOOK: true,
		DBOK: true,
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	AddDelta(logger.Info(), &start, &end).Msg("metrics")

	var fields map[string]any
	decoder := json.NewDecoder(&buf)
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&fields))
	assertJSONNumber(t, fields, "procReadBytes", "300")
	assertJSONNumber(t, fields, "procWriteBytes", "600")
	assertJSONNumber(t, fields, "procReadSyscalls", "5")
	assertJSONNumber(t, fields, "procWriteSyscalls", "6")
	assertJSONNumber(t, fields, "procReadChars", "50")
	assertJSONNumber(t, fields, "procWriteChars", "75")
	assertJSONNumber(t, fields, "procCancelledWriteBytes", "4")
	assertJSONNumber(t, fields, "allocBytes", "30")
	assertJSONNumber(t, fields, "mallocs", "9")
	assertJSONNumber(t, fields, "frees", "6")
	assertJSONNumber(t, fields, "gcCycles", "2")
	assertJSONNumber(t, fields, "gcPauseNs", "75")
	assertJSONNumber(t, fields, "cgoCalls", "10")
	assertJSONNumber(t, fields, "heapAllocBytes", "400")
	assertJSONNumber(t, fields, "heapSysBytes", "800")
	assertJSONNumber(t, fields, "heapObjects", "12")
	assertJSONNumber(t, fields, "goroutines", "4")
	assertJSONNumber(t, fields, "dbBytes", "125")
	assertJSONNumber(t, fields, "dbWalBytes", "30")
	assertJSONNumber(t, fields, "dbShmBytes", "20")
	assertJSONNumber(t, fields, "dbBytesDelta", "25")
	assertJSONNumber(t, fields, "dbWalBytesDelta", "20")
	assertJSONNumber(t, fields, "dbShmBytesDelta", "0")
}

func assertJSONNumber(t *testing.T, fields map[string]any, name, expected string) {
	t.Helper()
	number, ok := fields[name].(json.Number)
	require.True(t, ok, "field %s should be a JSON number", name)
	assert.Equal(t, expected, number.String())
}

func writeSizedFile(fs afero.Fs, path string, size int) error {
	if err := afero.WriteFile(fs, path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		return fmt.Errorf("write sized file: %w", err)
	}
	return nil
}
