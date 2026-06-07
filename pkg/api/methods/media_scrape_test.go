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

package methods

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	assertmock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// emptyPlatformScraper returns a platforms.Scraper that immediately closes the
// update channel without emitting any updates.
func emptyPlatformScraper(id, name string) platforms.Scraper {
	return platforms.Scraper{
		ID:   id,
		Name: name,
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, _ scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
		) error {
			go func() { close(ch) }()
			return nil
		},
	}
}

func fatalUpdatePlatformScraper(id string) platforms.Scraper {
	return platforms.Scraper{
		ID:   id,
		Name: "fatal update scraper",
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, _ scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
		) error {
			go func() {
				ch <- scraper.ScrapeUpdate{FatalErr: errors.New("scrape failed")}
				close(ch)
			}()
			return nil
		},
	}
}

// errorPlatformScraper returns a platforms.Scraper whose Scrape always returns err.
func errorPlatformScraper(id string, err error) platforms.Scraper {
	return platforms.Scraper{
		ID:   id,
		Name: "error scraper",
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, _ scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, _ chan<- scraper.ScrapeUpdate,
		) error {
			return err
		},
	}
}

func makeScrapeEnv(
	t *testing.T,
	scrapers map[string]platforms.Scraper,
	mockMediaDB *testhelpers.MockMediaDBI,
	params any,
) requests.RequestEnv {
	t.Helper()

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(scrapers)
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		require.NoError(t, err)
		rawParams = b
	}

	return requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
		Params:   rawParams,
	}
}

func waitScrapeNotification(t *testing.T, ns <-chan models.Notification) models.Notification {
	t.Helper()
	select {
	case n := <-ns:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for media.scraping notification")
		return models.Notification{}
	}
}

// TestHandleMediaScrape_UnknownScraper verifies that supplying an unregistered
// scraperID returns a client error immediately without touching the DB.
func TestHandleMediaScrape_UnknownScraper(t *testing.T) {
	// Not parallel — calls ClearScrapingStatus which resets shared global state.
	ClearScrapingStatus()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeScrapeEnv(t,
		map[string]platforms.Scraper{},
		mockDB,
		models.MediaScrapeParams{ScraperID: "unknown"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scraper")
}

// TestHandleMediaScrape_IndexingInProgress verifies that media.scrape is
// rejected while a media.generate (indexing) operation is running.
func TestHandleMediaScrape_IndexingInProgress(t *testing.T) {
	// Not parallel — manipulates shared statusInstance.
	ClearScrapingStatus()
	statusInstance.setRunning(true)
	defer statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeScrapeEnv(t,
		map[string]platforms.Scraper{"test-scraper": emptyPlatformScraper("test-scraper", "Test Scraper")},
		mockDB,
		models.MediaScrapeParams{ScraperID: "test-scraper"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media indexing is in progress")
}

// TestHandleMediaScrape_AlreadyRunning verifies that a second media.scrape
// call is rejected while one is already in progress.
func TestHandleMediaScrape_AlreadyRunning(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	scrapingStatusInstance.startIfNotRunning("test-scraper", false)
	defer scrapingStatusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeScrapeEnv(t,
		map[string]platforms.Scraper{"test-scraper": emptyPlatformScraper("test-scraper", "Test Scraper")},
		mockDB,
		models.MediaScrapeParams{ScraperID: "test-scraper"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scraping already in progress")
}

// TestHandleMediaScrape_InvalidParams verifies that missing required params
// return a client error.
func TestHandleMediaScrape_InvalidParams(t *testing.T) {
	// Not parallel — calls ClearScrapingStatus which resets shared global state.
	ClearScrapingStatus()

	mockDB := testhelpers.NewMockMediaDBI()
	// scraperId is required — omitting it should fail validation.
	env := makeScrapeEnv(t, map[string]platforms.Scraper{}, mockDB, map[string]any{})

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
}

// TestHandleMediaScrape_HappyPath verifies that a valid request starts the
// goroutine, returns nil result, and sends an initial "scraping: true"
// notification followed by "done: true" once the channel closes.
func TestHandleMediaScrape_HappyPath(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("SetScrapingOperation", database.ScrapingOperation{ScraperID: "test-scraper"}).Return(nil).Once()
	mockDB.On("SetScrapingStatus", mediadb.IndexingStatusRunning).Return(nil).Once()
	mockDB.On("SetScrapingStatus", mediadb.IndexingStatusCompleted).Return(nil).Once()
	mockDB.On("ClearScrapingOperation").Return(nil).Once()
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "test-scraper").Return(5, nil)

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(map[string]platforms.Scraper{
		"test-scraper": emptyPlatformScraper("test-scraper", "Test Scraper"),
	})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Params:   json.RawMessage(`{"scraperId":"test-scraper"}`),
	}

	result, err := HandleMediaScrape(env)
	require.NoError(t, err)
	assert.Nil(t, result)

	// Collect notifications: expect at minimum the initial "scraping:true" and
	// the terminal "done:true" notifications.
	var gotStart, gotDone bool
	timeout := time.After(2 * time.Second)
	for !gotDone {
		select {
		case n := <-ns:
			if n.Method != models.NotificationMediaScraping {
				continue
			}
			var payload models.ScrapingStatusResponse
			require.NoError(t, json.Unmarshal(n.Params, &payload))
			assert.Equal(t, "test-scraper", payload.ScraperID)
			assert.Equal(t, 5, payload.TotalScraped)
			if payload.Scraping && !payload.Done {
				gotStart = true
			}
			if payload.Done {
				gotDone = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for media.scraping notifications")
		}
	}

	assert.True(t, gotStart, "expected an initial scraping=true notification")
	assert.True(t, gotDone, "expected a done=true notification")

	// Wait for the goroutine to fully wind down before asserting status.
	require.Eventually(t, func() bool {
		return !IsScrapingRunning()
	}, 2*time.Second, 10*time.Millisecond, "scraping status should clear after goroutine completes")

	statusResult, err := HandleMediaScrapeStatus(requests.RequestEnv{Context: context.Background()})
	require.NoError(t, err)
	status, ok := statusResult.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, "test-scraper", status.ScraperID)
	assert.False(t, status.Scraping)
	assert.True(t, status.Done)

	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrape_FatalUpdateDoesNotSynthesizeDone(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("SetScrapingOperation", database.ScrapingOperation{ScraperID: "fail-scraper"}).Return(nil).Once()
	mockDB.On("SetScrapingStatus", mediadb.IndexingStatusRunning).Return(nil).Once()
	mockDB.On("SetScrapingStatus", mediadb.IndexingStatusFailed).Return(nil).Once()
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "fail-scraper").Return(0, nil)

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(map[string]platforms.Scraper{
		"fail-scraper": fatalUpdatePlatformScraper("fail-scraper"),
	})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Params:   json.RawMessage(`{"scraperId":"fail-scraper"}`),
	}

	result, err := HandleMediaScrape(env)
	require.NoError(t, err)
	assert.Nil(t, result)

	require.Eventually(t, func() bool {
		return !IsScrapingRunning()
	}, 2*time.Second, 10*time.Millisecond)

	var sawFailure bool
	for {
		select {
		case n := <-ns:
			if n.Method != models.NotificationMediaScraping {
				continue
			}
			var payload models.ScrapingStatusResponse
			require.NoError(t, json.Unmarshal(n.Params, &payload))
			assert.False(t, payload.Done, "fatal scrape must not synthesize a completed Done update")
			if payload.State == scrapeStateFailed {
				sawFailure = true
			}
		default:
			assert.True(t, sawFailure, "expected failed scraping notification")
			mockDB.AssertExpectations(t)
			return
		}
	}
}

func TestHandleMediaScrapeStatus_NoRun(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetTotalScrapedMediaCount", assertmock.Anything).Return(7, nil)

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Empty(t, status.ScraperID)
	assert.False(t, status.Scraping)
	assert.False(t, status.Done)
	assert.Equal(t, 7, status.TotalScraped)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_RefreshesExactCountDespiteCache(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	scrapingStatusInstance.updateCountCache("test-scraper", 5, time.Now())
	publishScrapingStatus(make(chan models.Notification, 1), &models.ScrapingStatusResponse{
		ScraperID:    "test-scraper",
		SystemID:     "SNES",
		Processed:    42,
		Total:        100,
		Matched:      38,
		Skipped:      4,
		TotalScraped: 5,
		Scraping:     true,
	})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "test-scraper").Return(12, nil).Once()

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, 12, status.TotalScraped)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_TracksLatestProgress(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	publishScrapingStatus(make(chan models.Notification, 1), &models.ScrapingStatusResponse{
		ScraperID: "test-scraper",
		SystemID:  "SNES",
		Processed: 42,
		Total:     100,
		Matched:   38,
		Skipped:   4,
		Scraping:  true,
	})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "test-scraper").Return(11, nil)

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.ScrapingStatusResponse{
		ScraperID:    "test-scraper",
		SystemID:     "SNES",
		State:        "running",
		Processed:    42,
		Total:        100,
		Matched:      38,
		Skipped:      4,
		TotalScraped: 11,
		Scraping:     true,
		Paused:       false,
	}, status)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_IgnoresScrapedCountError(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	publishScrapingStatus(make(chan models.Notification, 1), &models.ScrapingStatusResponse{
		ScraperID: "test-scraper",
		SystemID:  "SNES",
		Processed: 12,
		Total:     20,
		Matched:   10,
		Skipped:   2,
		Scraping:  true,
	})

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "test-scraper").Return(0, errors.New("count failed"))

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, "test-scraper", status.ScraperID)
	assert.Equal(t, "SNES", status.SystemID)
	assert.Equal(t, 12, status.Processed)
	assert.Equal(t, 20, status.Total)
	assert.Equal(t, 10, status.Matched)
	assert.Equal(t, 2, status.Skipped)
	assert.Zero(t, status.TotalScraped)
	assert.True(t, status.Scraping)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_BoundsSlowScrapedCount(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetTotalScrapedMediaCount", assertmock.Anything).
		Run(func(args assertmock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return(0, context.DeadlineExceeded).
		Once()

	started := time.Now()
	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})

	require.NoError(t, err)
	assert.Less(t, time.Since(started), 2*time.Second)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Zero(t, status.TotalScraped)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_UsesCachedCountWhenRefreshTimesOut(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	scrapingStatusInstance.updateCountCache("", 9, time.Now())

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetTotalScrapedMediaCount", assertmock.Anything).
		Return(0, context.DeadlineExceeded).
		Once()

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
	})

	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, 9, status.TotalScraped)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrapeStatus_UsesScrapePauser(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	publishScrapingStatus(make(chan models.Notification, 1), &models.ScrapingStatusResponse{
		ScraperID: "test-scraper",
		Scraping:  true,
	})
	pauser := syncutil.NewPauser()
	pauser.Pause()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "test-scraper").Return(0, nil)

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{
		Context:      context.Background(),
		Database:     &database.Database{MediaDB: mockDB},
		ScrapePauser: pauser,
	})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.True(t, status.Paused)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaScrape_ScraperInitError verifies that when the scraper's
// Scrape method returns an error, HandleMediaScrape propagates the error and
// the global scraping status is cleared.
func TestHandleMediaScrape_PersistOperationErrorClearsRunning(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("SetScrapingOperation", database.ScrapingOperation{ScraperID: "test-scraper"}).
		Return(errors.New("persist failed")).Once()
	env := makeScrapeEnv(t,
		map[string]platforms.Scraper{"test-scraper": emptyPlatformScraper("test-scraper", "Test Scraper")},
		mockDB,
		models.MediaScrapeParams{ScraperID: "test-scraper"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to persist scraping operation")
	assert.False(t, IsScrapingRunning())
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrape_ScraperInitError(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	scrapeErr := errors.New("connection refused")
	failingScraper := errorPlatformScraper("fail-scraper", scrapeErr)

	mockDB := testhelpers.NewMockMediaDBI()
	// TrackBackgroundOperation/BackgroundOperationDone must NOT be called because
	// the goroutine never starts when Scrape() returns an error.
	env := makeScrapeEnv(t,
		map[string]platforms.Scraper{"fail-scraper": failingScraper},
		mockDB,
		models.MediaScrapeParams{ScraperID: "fail-scraper"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start scraper")

	assert.False(t, IsScrapingRunning(), "scraping status must be cleared after error")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaScrapeCancel_NoneRunning verifies the response when no
// scraping is active.
func TestHandleMediaScrapeCancel_NoneRunning(t *testing.T) {
	ClearScrapingStatus()

	env := requests.RequestEnv{Context: context.Background()}
	result, err := HandleMediaScrapeCancel(env)
	require.NoError(t, err)
	resp, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "no scraping operation is currently running", resp["message"])
}

// TestHandleMediaScrapeCancel_CancelsActive verifies that an active scrape is
// cancelled and the response message reflects that.
func TestHandleMediaScrapeCancel_CancelsActive(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	ctx, cancel := context.WithCancel(context.Background())
	scrapingStatusInstance.startIfNotRunning("test-scraper", false)
	scrapingStatusInstance.setCancelFunc(cancel)
	defer func() {
		cancel()
		scrapingStatusInstance.clear()
	}()

	env := requests.RequestEnv{Context: ctx}
	result, err := HandleMediaScrapeCancel(env)
	require.NoError(t, err)

	resp, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scraping cancelled", resp["message"])

	statusResult, err := HandleMediaScrapeStatus(requests.RequestEnv{Context: context.Background()})
	require.NoError(t, err)
	status, ok := statusResult.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.False(t, status.Scraping)
	assert.True(t, status.Done)
	assert.Equal(t, "cancelled", status.State)
}

func TestHandleMediaScrapeResume_ResumesPausedScrape(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	scrapingStatusInstance.startIfNotRunning("test-scraper", false)
	defer scrapingStatusInstance.clear()
	pauser := syncutil.NewPauser()
	pauser.Pause()

	result, err := HandleMediaScrapeResume(requests.RequestEnv{
		Context:      context.Background(),
		State:        st,
		ScrapePauser: pauser,
	})
	require.NoError(t, err)
	resp, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Media scraping resumed", resp["message"])
	assert.False(t, pauser.IsPaused())

	notif := waitScrapeNotification(t, ns)
	assert.Equal(t, models.NotificationMediaScraping, notif.Method)
	var payload models.ScrapingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &payload))
	assert.True(t, payload.Scraping)
	assert.False(t, payload.Paused)
	assert.Equal(t, "running", payload.State)
}

func TestHandleMediaScrapeResume_NilPauser(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	result, err := HandleMediaScrapeResume(requests.RequestEnv{Context: context.Background()})
	require.NoError(t, err)
	resp, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Media scraping is not paused", resp["message"])
}

func TestHandleMediaScrapeResume_NotPaused(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	pauser := syncutil.NewPauser()
	result, err := HandleMediaScrapeResume(requests.RequestEnv{
		Context:      context.Background(),
		ScrapePauser: pauser,
	})
	require.NoError(t, err)
	resp, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Media scraping is not paused", resp["message"])
}

func TestScrapingStatus_ClearIfOwner_MatchingID_Clears(t *testing.T) {
	t.Parallel()
	s := &scrapingStatus{}
	s.startIfNotRunning("my-scraper", true)
	s.clearIfOwner("my-scraper")
	assert.False(t, s.isRunning())
	assert.False(t, s.force)
}

func TestScrapingStatus_ClearIfOwner_MismatchedID_Preserves(t *testing.T) {
	t.Parallel()
	s := &scrapingStatus{}
	s.startIfNotRunning("owner-a", true)
	s.clearIfOwner("owner-b")
	assert.True(t, s.isRunning(), "non-owner clearIfOwner must not clear state")
	assert.True(t, s.force, "non-owner clearIfOwner must preserve force state")
	s.clear()
}

func TestHandleMediaScrape_CachesProgressScrapedCountAndRefreshesDone(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "cached-progress").Return(5, nil).Once()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "cached-progress").Return(9, nil).Once()

	progressScraper := platforms.Scraper{
		ID:   "cached-progress",
		Name: "Cached Progress",
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, _ scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
		) error {
			go func() {
				defer close(ch)
				ch <- scraper.ScrapeUpdate{SystemID: "SNES", Total: 30, Processed: 10, Matched: 8, Skipped: 2}
				ch <- scraper.ScrapeUpdate{SystemID: "SNES", Total: 30, Processed: 20, Matched: 16, Skipped: 4}
				ch <- scraper.ScrapeUpdate{
					SystemID: "SNES", Total: 30, Processed: 30, Matched: 24, Skipped: 6, Done: true,
				}
			}()
			return nil
		},
	}

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(map[string]platforms.Scraper{
		"cached-progress": progressScraper,
	})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Params:   json.RawMessage(`{"scraperId":"cached-progress"}`),
	}

	result, err := HandleMediaScrape(env)
	require.NoError(t, err)
	assert.Nil(t, result)

	var progressCounts []int
	var doneCount int
	var gotDone bool
	timeout := time.After(2 * time.Second)
	for !gotDone {
		select {
		case n := <-ns:
			if n.Method != models.NotificationMediaScraping {
				continue
			}
			var p models.ScrapingStatusResponse
			require.NoError(t, json.Unmarshal(n.Params, &p))
			if p.Done {
				doneCount = p.TotalScraped
				gotDone = true
			} else if p.Processed > 0 {
				progressCounts = append(progressCounts, p.TotalScraped)
			}
		case <-timeout:
			t.Fatal("timed out waiting for media.scraping done notification")
		}
	}

	assert.Equal(t, []int{5, 5}, progressCounts)
	assert.Equal(t, 9, doneCount)
	require.Eventually(t, func() bool {
		return !IsScrapingRunning()
	}, 2*time.Second, 10*time.Millisecond)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrape_EmitsFatalStatus(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "fatal-scraper").Return(0, nil)

	scrapeErr := errors.New("parse failed")
	fatalScraper := platforms.Scraper{
		ID:   "fatal-scraper",
		Name: "Fatal Scraper",
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, _ scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
		) error {
			go func() {
				defer close(ch)
				ch <- scraper.ScrapeUpdate{
					SystemID: "SNES", FatalErr: scrapeErr, Done: true, TotalSteps: 2, CurrentStep: 1,
				}
			}()
			return nil
		},
	}

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(map[string]platforms.Scraper{"fatal-scraper": fatalScraper})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	_, err := HandleMediaScrape(requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Params:   json.RawMessage(`{"scraperId":"fatal-scraper"}`),
	})
	require.NoError(t, err)

	var gotFatal bool
	timeout := time.After(2 * time.Second)
	for !gotFatal {
		select {
		case n := <-ns:
			if n.Method != models.NotificationMediaScraping {
				continue
			}
			var p models.ScrapingStatusResponse
			require.NoError(t, json.Unmarshal(n.Params, &p))
			if p.Done {
				assert.Equal(t, "failed", p.State)
				assert.Equal(t, "parse failed", p.Error)
				gotFatal = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for fatal media.scraping notification")
		}
	}
	require.Eventually(t, func() bool {
		return !IsScrapingRunning()
	}, 2*time.Second, 10*time.Millisecond)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaScrape_EmitsProgressUpdates(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()
	mockDB.On("GetScrapedMediaCount", assertmock.Anything, "progress-scraper").Return(3, nil)

	var gotForceOption bool
	progressScraper := platforms.Scraper{
		ID:   "progress-scraper",
		Name: "Progress Scraper",
		Scrape: func(
			_ context.Context, _ *config.Instance, _ platforms.Platform,
			_ afero.Fs, _ *database.Database, opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
		) error {
			gotForceOption = opts.Force
			go func() {
				defer close(ch)
				ch <- scraper.ScrapeUpdate{
					SystemID: "SNES", Total: 10, Processed: 5, Matched: 4, Skipped: 1,
					TotalSteps: 2, CurrentStep: 1,
				}
				ch <- scraper.ScrapeUpdate{
					SystemID: "SNES", Total: 10, Processed: 10, Matched: 8, Skipped: 2,
					TotalSteps: 2, CurrentStep: 1, Done: true,
				}
			}()
			return nil
		},
	}

	pl := mocks.NewMockPlatform()
	pl.On("Scrapers", assertmock.Anything).Return(map[string]platforms.Scraper{
		"progress-scraper": progressScraper,
	})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Params:   json.RawMessage(`{"scraperId":"progress-scraper","force":true}`),
	}

	result, err := HandleMediaScrape(env)
	require.NoError(t, err)
	assert.Nil(t, result)

	var gotDone bool
	var maxProcessed int
	var sawCurrentSystem bool
	var sawForce bool
	timeout := time.After(2 * time.Second)
	for !gotDone {
		select {
		case n := <-ns:
			if n.Method != models.NotificationMediaScraping {
				continue
			}
			var p models.ScrapingStatusResponse
			require.NoError(t, json.Unmarshal(n.Params, &p))
			if p.Force {
				sawForce = true
			}
			if p.Processed > maxProcessed {
				maxProcessed = p.Processed
			}
			if p.CurrentSystem != nil && !p.Done {
				sawCurrentSystem = true
				assert.Equal(t, "SNES", p.CurrentSystem.SystemID)
				assert.Equal(t, p.Processed, p.CurrentSystem.Processed)
				assert.Equal(t, p.Total, p.CurrentSystem.Total)
				require.NotNil(t, p.TotalSteps)
				require.NotNil(t, p.CurrentStep)
				assert.Equal(t, 2, *p.TotalSteps)
				assert.Equal(t, 1, *p.CurrentStep)
				assert.Equal(t, "running", p.State)
			}
			if p.Done {
				gotDone = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for media.scraping done notification")
		}
	}

	assert.True(t, gotForceOption, "force option should be passed to scraper")
	assert.True(t, sawForce, "scrape status should expose active force scrape")
	assert.Equal(t, 10, maxProcessed, "progress updates should reflect actual processed count")
	assert.True(t, sawCurrentSystem, "progress updates should expose currentSystem and step fields")
	require.Eventually(t, func() bool {
		return !IsScrapingRunning()
	}, 2*time.Second, 10*time.Millisecond)
	mockDB.AssertExpectations(t)
}
