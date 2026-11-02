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

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openContendedUserDB opens a UserDB whose busy timeout is far shorter than
// production's, so a lock held by holder outlasts it immediately, and a second
// connection to the same file that is already holding the write lock.
func openContendedUserDB(t *testing.T) (db *userdb.UserDB, holder *sql.Conn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "user.db")
	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=20&_txlock=immediate")
	require.NoError(t, err)
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform")
	db = &userdb.UserDB{}
	require.NoError(t, db.SetSQLForTesting(context.Background(), sqlDB, pl))
	t.Cleanup(func() { _ = db.Close() })

	holderDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = holderDB.Close() })
	holder, err = holderDB.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })
	_, err = holder.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	return db, holder
}

func tokenHistoryEntry() *database.HistoryEntry {
	return &database.HistoryEntry{
		Time: time.Now(), Type: "nfc", TokenID: "id", TokenValue: "value", Success: true,
	}
}

// A maintenance write that holds the user database's write lock past the busy
// timeout must not lose the token history entry queued behind it.
func TestWriteTokenHistoryRetriesPastLockContention(t *testing.T) {
	t.Parallel()
	db, holder := openContendedUserDB(t)

	var sleeps atomic.Int32
	err := writeTokenHistory(context.Background(), db, tokenHistoryEntry(),
		[]time.Duration{time.Millisecond, time.Millisecond},
		func(context.Context, time.Duration) error {
			// The first attempt has failed on the held lock; let the other
			// writer finish before the retry.
			if sleeps.Add(1) == 1 {
				_, commitErr := holder.ExecContext(context.Background(), "COMMIT")
				require.NoError(t, commitErr)
			}
			return nil
		})
	require.NoError(t, err)
	assert.Equal(t, int32(1), sleeps.Load())

	history, err := db.GetHistory(0)
	require.NoError(t, err)
	assert.Len(t, history, 1)
}

// When the lock never frees, the error says how long the write waited and
// which SQLite code it ended on, so a report can be told apart from a fault.
func TestWriteTokenHistoryReportsLockContention(t *testing.T) {
	t.Parallel()
	db, _ := openContendedUserDB(t)

	err := writeTokenHistory(context.Background(), db, tokenHistoryEntry(),
		[]time.Duration{time.Millisecond, time.Millisecond}, sleepContext)
	require.Error(t, err)
	_, contended := database.LockContention(err)
	assert.True(t, contended)
	assert.Contains(t, err.Error(), "sqlite_code=5")
	assert.Contains(t, err.Error(), "attempts=3")
	assert.Contains(t, err.Error(), "elapsed_ms=")
}

// Shutdown stops the retries but never the first attempt.
func TestWriteTokenHistoryStopsRetryingAtShutdown(t *testing.T) {
	t.Parallel()
	db, _ := openContendedUserDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := writeTokenHistory(ctx, db, tokenHistoryEntry(), []time.Duration{time.Hour}, sleepContext)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attempts=1")
}

// The user database maintenance steps write to one SQLite file and must not
// overlap: each starts only after the one before it returns.
func TestRunInSequence(t *testing.T) {
	t.Parallel()

	var running atomic.Int32
	var order []int
	step := func(n int) func(context.Context) {
		return func(context.Context) {
			require.Equal(t, int32(1), running.Add(1), "steps overlapped")
			order = append(order, n)
			running.Add(-1)
		}
	}
	runInSequence(context.Background(), step(1), step(2), step(3))
	assert.Equal(t, []int{1, 2, 3}, order)

	ctx, cancel := context.WithCancel(context.Background())
	order = nil
	runInSequence(ctx, step(1), func(context.Context) { cancel() }, step(3))
	assert.Equal(t, []int{1}, order, "steps after cancellation must not run")
}

func TestOptimizationFailureLevel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, zerolog.DebugLevel, optimizationFailureLevel(fmt.Errorf("step: %w", context.Canceled)))
	assert.Equal(t, zerolog.WarnLevel, optimizationFailureLevel(sqlite3.Error{Code: sqlite3.ErrIoErr}))
	assert.Equal(t, zerolog.WarnLevel, optimizationFailureLevel(sqlite3.Error{Code: sqlite3.ErrCorrupt}))
	assert.Equal(t, zerolog.ErrorLevel, optimizationFailureLevel(errors.New("constraint failed")))
}

// A resume whose job was cancelled or replaced since it was stored has nothing
// left to do. The error comes from the real resume path so the check covers
// how it is wrapped there.
func TestScrapeResumeFailureLevel(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	t.Cleanup(methods.ClearScrapingStatus)
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	mdb := testhelpers.NewMockMediaDBI()
	mdb.On("GetScrapingOperation").Return(database.ScrapingOperation{}, false, nil).Once()
	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", cfg).Return(map[string]platforms.Scraper{"local": {
		ID: "local",
		Scrape: func(context.Context, *config.Instance, platforms.Platform, afero.Fs,
			*database.Database, scraper.ScrapeOptions, platforms.ScraperCustomOptions,
			chan<- scraper.ScrapeUpdate,
		) error {
			return nil
		},
	}})
	st, _ := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	retiredErr := methods.ResumeMediaScrape(&requests.RequestEnv{
		Context: st.GetContext(), Platform: pl, Config: cfg, State: st,
		Database: &database.Database{MediaDB: mdb},
	}, database.ScrapingOperation{ScraperID: "local"})
	require.True(t, methods.IsScrapeJobRetired(retiredErr), "got %v", retiredErr)

	assert.Equal(t, zerolog.InfoLevel, scrapeResumeFailureLevel(retiredErr))
	assert.Equal(t, zerolog.InfoLevel, scrapeResumeFailureLevel(fmt.Errorf("resume: %w", context.Canceled)))
	assert.Equal(t, zerolog.WarnLevel, scrapeResumeFailureLevel(sqlite3.Error{Code: sqlite3.ErrIoErr}))
	assert.Equal(t, zerolog.ErrorLevel, scrapeResumeFailureLevel(errors.New("scraper exploded")))
	mdb.AssertExpectations(t)
}
