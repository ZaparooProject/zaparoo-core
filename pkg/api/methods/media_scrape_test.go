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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptyClosedScraper returns a channel that is immediately closed (no updates).
func emptyClosedScraper() *closedChannelScraper {
	return &closedChannelScraper{id: "test-scraper", name: "Test Scraper"}
}

type closedChannelScraper struct {
	id   string
	name string
}

func (s *closedChannelScraper) ID() string               { return s.id }
func (s *closedChannelScraper) Name() string             { return s.name }
func (*closedChannelScraper) SupportedSystems() []string { return nil }
func (*closedChannelScraper) Scrape(_ context.Context, _ scraper.ScrapeOptions) (<-chan scraper.ScrapeUpdate, error) {
	ch := make(chan scraper.ScrapeUpdate)
	close(ch)
	return ch, nil
}

func makeScrapeEnv(
	t *testing.T,
	scrapers map[string]scraper.Scraper,
	mockMediaDB *testhelpers.MockMediaDBI,
	params any,
) requests.RequestEnv {
	t.Helper()

	pl := mocks.NewMockPlatform()
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
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
		Scrapers: scrapers,
		Params:   rawParams,
	}
}

// TestHandleMediaScrape_UnknownScraper verifies that supplying an unregistered
// scraperID returns a client error immediately without touching the DB.
func TestHandleMediaScrape_UnknownScraper(t *testing.T) {
	// Not parallel — calls ClearScrapingStatus which resets shared global state.
	ClearScrapingStatus()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeScrapeEnv(t,
		map[string]scraper.Scraper{},
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
		map[string]scraper.Scraper{"test-scraper": emptyClosedScraper()},
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
	scrapingStatusInstance.startIfNotRunning("test-scraper")
	defer scrapingStatusInstance.clear()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeScrapeEnv(t,
		map[string]scraper.Scraper{"test-scraper": emptyClosedScraper()},
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
	env := makeScrapeEnv(t, map[string]scraper.Scraper{}, mockDB, map[string]any{})

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
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockDB},
		Scrapers: map[string]scraper.Scraper{
			"test-scraper": emptyClosedScraper(),
		},
		Params: json.RawMessage(`{"scraperId":"test-scraper"}`),
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

func TestHandleMediaScrapeStatus_NoRun(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{Context: context.Background()})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Empty(t, status.ScraperID)
	assert.False(t, status.Scraping)
	assert.False(t, status.Done)
}

func TestHandleMediaScrapeStatus_TracksLatestProgress(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()

	publishScrapingStatus(make(chan models.Notification, 1), models.ScrapingStatusResponse{
		ScraperID: "test-scraper",
		SystemID:  "SNES",
		Processed: 42,
		Total:     100,
		Matched:   38,
		Skipped:   4,
		Scraping:  true,
	})

	result, err := HandleMediaScrapeStatus(requests.RequestEnv{Context: context.Background()})
	require.NoError(t, err)
	status, ok := result.(models.ScrapingStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.ScrapingStatusResponse{
		ScraperID: "test-scraper",
		SystemID:  "SNES",
		Processed: 42,
		Total:     100,
		Matched:   38,
		Skipped:   4,
		Scraping:  true,
	}, status)
}

// TestHandleMediaScrape_ScraperInitError verifies that when the scraper's
// Scrape method returns an error, HandleMediaScrape propagates the error and
// the global scraping status is cleared.
func TestHandleMediaScrape_ScraperInitError(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()

	scrapeErr := errors.New("connection refused")
	failingScraper := &errorScraper{id: "fail-scraper", err: scrapeErr}

	mockDB := testhelpers.NewMockMediaDBI()
	// TrackBackgroundOperation/BackgroundOperationDone must NOT be called because
	// the goroutine never starts when Scrape() returns an error.
	env := makeScrapeEnv(t,
		map[string]scraper.Scraper{"fail-scraper": failingScraper},
		mockDB,
		models.MediaScrapeParams{ScraperID: "fail-scraper"},
	)

	_, err := HandleMediaScrape(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start scraper")

	assert.False(t, IsScrapingRunning(), "scraping status must be cleared after error")
	mockDB.AssertExpectations(t)
}

// errorScraper is a test double whose Scrape always returns a non-nil error.
type errorScraper struct {
	err error
	id  string
}

func (s *errorScraper) ID() string               { return s.id }
func (*errorScraper) Name() string               { return "error scraper" }
func (*errorScraper) SupportedSystems() []string { return nil }
func (s *errorScraper) Scrape(_ context.Context, _ scraper.ScrapeOptions) (<-chan scraper.ScrapeUpdate, error) {
	return nil, s.err
}

// TestHandleMediaScrapeCancel_NoneRunning verifies the response when no
// scraping is active.
func TestHandleMediaScrapeCancel_NoneRunning(t *testing.T) {
	t.Parallel()
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
	scrapingStatusInstance.startIfNotRunning("test-scraper")
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
}
