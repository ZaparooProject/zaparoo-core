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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- mock scraper ---

type mockScraper struct {
	err     error
	id      string
	updates []scraper.ScrapeUpdate
}

func (m *mockScraper) ID() string { return m.id }

func (m *mockScraper) Scrape(_ context.Context, _ scraper.ScrapeOptions) (<-chan scraper.ScrapeUpdate, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan scraper.ScrapeUpdate, len(m.updates)+1)
	for _, u := range m.updates {
		ch <- u
	}
	ch <- scraper.ScrapeUpdate{Done: true}
	close(ch)
	return ch, nil
}

// --- helpers ---

// routeRequest wires r into a chi router at path using the given method and
// handler, then serves it. This lets URL params resolve properly.
func routeRequest(method, path, pattern string, handler http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Method(method, pattern, handler)
	router.ServeHTTP(rr, r)
	return rr
}

// --- ScraperRegistry tests ---

func TestScraperRegistry_ListScrapers_Idle(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()
	reg.Register(&mockScraper{id: "test.scraper"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scrapers", http.NoBody)
	rr := httptest.NewRecorder()
	reg.HandleListScrapers().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var out []scraperStatusResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out, 1)
	assert.Equal(t, "test.scraper", out[0].ID)
	assert.Equal(t, "idle", out[0].Status)
}

func TestScraperRegistry_ListScrapers_Running(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewScraperRegistry()
	// Use a scraper that blocks until context is cancelled.
	blockingScraper := &blockingMockScraper{id: "test.scraper"}
	reg.Register(blockingScraper)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run", http.NoBody)
	rr := routeRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(ctx),
		req)
	require.Equal(t, http.StatusAccepted, rr.Code)

	// Give the goroutine a moment to set running.
	require.Eventually(t, func() bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return reg.running == "test.scraper"
	}, time.Second, 10*time.Millisecond)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/scrapers", http.NoBody)
	listRR := httptest.NewRecorder()
	reg.HandleListScrapers().ServeHTTP(listRR, listReq)

	var out []scraperStatusResponse
	require.NoError(t, json.Unmarshal(listRR.Body.Bytes(), &out))
	require.Len(t, out, 1)
	assert.Equal(t, "running", out[0].Status)
}

func TestScraperRegistry_RunScraper_NotFound(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/unknown/run", http.NoBody)
	rr := routeRequest(http.MethodPost, "/api/v1/scrapers/unknown/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(context.Background()),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestScraperRegistry_RunScraper_Conflict(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewScraperRegistry()
	reg.Register(&blockingMockScraper{id: "a"})
	reg.Register(&blockingMockScraper{id: "b"})

	// Start the first scraper.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/a/run", http.NoBody)
	rr1 := routeRequest(http.MethodPost, "/api/v1/scrapers/a/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(ctx),
		req1)
	require.Equal(t, http.StatusAccepted, rr1.Code)

	// Wait until first one is running.
	require.Eventually(t, func() bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return reg.running != ""
	}, time.Second, 10*time.Millisecond)

	// Attempt to start a second scraper while first is running.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/b/run", http.NoBody)
	rr2 := routeRequest(http.MethodPost, "/api/v1/scrapers/b/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(ctx),
		req2)
	assert.Equal(t, http.StatusConflict, rr2.Code)
}

func TestScraperRegistry_CancelScraper_NotRunning(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()
	reg.Register(&mockScraper{id: "test.scraper"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/cancel", http.NoBody)
	rr := routeRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/cancel",
		"/api/v1/scrapers/{id}/cancel",
		reg.HandleCancelScraper(),
		req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestScraperRegistry_CancelScraper_NotFound(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/unknown/cancel", http.NoBody)
	rr := routeRequest(http.MethodPost, "/api/v1/scrapers/unknown/cancel",
		"/api/v1/scrapers/{id}/cancel",
		reg.HandleCancelScraper(),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestScraperRegistry_CancelScraper_WhileRunning(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewScraperRegistry()
	reg.Register(&blockingMockScraper{id: "test.scraper"})

	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run", http.NoBody)
	rr := routeRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(ctx),
		runReq)
	require.Equal(t, http.StatusAccepted, rr.Code)

	require.Eventually(t, func() bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return reg.running == "test.scraper"
	}, time.Second, 10*time.Millisecond)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/cancel", http.NoBody)
	cancelRR := routeRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/cancel",
		"/api/v1/scrapers/{id}/cancel",
		reg.HandleCancelScraper(),
		cancelReq)
	assert.Equal(t, http.StatusNoContent, cancelRR.Code)
}

func TestScraperRegistry_Status_Idle(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()
	reg.Register(&mockScraper{id: "test.scraper"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scrapers/test.scraper/status", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/scrapers/test.scraper/status",
		"/api/v1/scrapers/{id}/status",
		reg.HandleScraperStatus(),
		req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/event-stream")

	// Should contain exactly one done event.
	body := rr.Body.String()
	assert.Contains(t, body, `"done":true`)
}

func TestScraperRegistry_Status_NotFound(t *testing.T) {
	t.Parallel()

	reg := NewScraperRegistry()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scrapers/unknown/status", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/scrapers/unknown/status",
		"/api/v1/scrapers/{id}/status",
		reg.HandleScraperStatus(),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestScraperRegistry_Status_StreamsUpdates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a controlled scraper: it blocks on a channel until we release it.
	release := make(chan struct{})
	ctrl := &controlledMockScraper{id: "test.scraper", release: release}

	reg := NewScraperRegistry()
	reg.Register(ctrl)

	// Subscribe to status BEFORE starting the run (in a goroutine, since it blocks).
	type sseResult struct {
		body string
		code int
	}
	sseResultCh := make(chan sseResult, 1)
	go func() {
		// Small delay to let the scraper start first.
		time.Sleep(20 * time.Millisecond)
		statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/scrapers/test.scraper/status", http.NoBody)
		statusRR := routeRequest(http.MethodGet, "/api/v1/scrapers/test.scraper/status",
			"/api/v1/scrapers/{id}/status",
			reg.HandleScraperStatus(),
			statusReq)
		sseResultCh <- sseResult{body: statusRR.Body.String(), code: statusRR.Code}
	}()

	// Start the scraper.
	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run", http.NoBody)
	runRR := routeRequest(http.MethodPost, "/api/v1/scrapers/test.scraper/run",
		"/api/v1/scrapers/{id}/run",
		reg.HandleRunScraper(ctx),
		runReq)
	require.Equal(t, http.StatusAccepted, runRR.Code)

	// Let the controlled scraper finish after a short pause.
	time.Sleep(50 * time.Millisecond)
	close(release)

	// Collect SSE result.
	var res sseResult
	select {
	case res = <-sseResultCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SSE response")
	}

	require.Equal(t, http.StatusOK, res.code)

	// The response must contain at least one SSE event with done=true.
	assert.Contains(t, res.body, `"done":true`)
}

// --- properties endpoint tests ---

func TestHandleGetMediaTitleProperties_TitleNotFound(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(999)).Return((*database.MediaTitle)(nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/999/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/999/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_EmptyProperties(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1, Name: "Test Game"}
	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Empty(t, out)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_WithProperties(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1, Name: "Test Game"}
	props := []database.MediaProperty{
		{TypeTag: "property:description", Text: "A great game", ContentType: "text/plain"},
		{TypeTag: "property:image.boxart", Text: "/roms/snes/images/mario.png", ContentType: "image/png"},
	}

	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return(props, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out, 2)
	assert.Equal(t, "property:description", out[0].TypeTag)
	assert.Equal(t, "A great game", out[0].Text)
	assert.Equal(t, "text/plain", out[0].ContentType)
	assert.Equal(t, "property:image.boxart", out[1].TypeTag)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_DBError(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1}
	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return(
		[]database.MediaProperty(nil), errors.New("db error"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_InvalidID(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/abc/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/abc/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetMediaProperties_MediaNotFound(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	// GetMediaByDBID returns zero-value result (MediaID == 0) when not found.
	db.On("GetMediaByDBID", mock.Anything, int64(999)).Return(
		database.SearchResultWithCursor{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/999/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/999/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaProperties_EmptyProperties(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	db.On("GetMediaByDBID", mock.Anything, int64(1)).Return(
		database.SearchResultWithCursor{MediaID: 1, Name: "Test ROM"}, nil)
	db.On("GetMediaProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/1/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Empty(t, out)
	db.AssertExpectations(t)
}

func TestHandleGetMediaProperties_WithProperties(t *testing.T) {
	t.Parallel()

	props := []database.MediaProperty{
		{TypeTag: "property:video", Text: "/roms/snes/videos/mario.mp4", ContentType: "video/mp4"},
	}

	db := helpers.NewMockMediaDBI()
	db.On("GetMediaByDBID", mock.Anything, int64(5)).Return(
		database.SearchResultWithCursor{MediaID: 5, Name: "Test ROM"}, nil)
	db.On("GetMediaProperties", mock.Anything, int64(5)).Return(props, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/5/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/5/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out, 1)
	assert.Equal(t, "property:video", out[0].TypeTag)
	assert.Equal(t, "/roms/snes/videos/mario.mp4", out[0].Text)
	db.AssertExpectations(t)
}

// --- blockingMockScraper ---

// blockingMockScraper blocks until its context is cancelled.
type blockingMockScraper struct {
	id string
}

func (b *blockingMockScraper) ID() string { return b.id }

func (b *blockingMockScraper) Scrape(ctx context.Context, _ scraper.ScrapeOptions) (<-chan scraper.ScrapeUpdate, error) {
	ch := make(chan scraper.ScrapeUpdate, 1)
	go func() {
		defer close(ch)
		<-ctx.Done()
		ch <- scraper.ScrapeUpdate{Done: true}
	}()
	return ch, nil
}

// controlledMockScraper sends one update then blocks until release is closed.
type controlledMockScraper struct {
	release chan struct{}
	id      string
}

func (c *controlledMockScraper) ID() string { return c.id }

func (c *controlledMockScraper) Scrape(ctx context.Context, _ scraper.ScrapeOptions) (<-chan scraper.ScrapeUpdate, error) {
	ch := make(chan scraper.ScrapeUpdate, 4)
	go func() {
		defer close(ch)
		ch <- scraper.ScrapeUpdate{SystemID: "NES", Total: 1}
		select {
		case <-c.release:
		case <-ctx.Done():
		}
		ch <- scraper.ScrapeUpdate{SystemID: "NES", Processed: 1, Matched: 1, Done: true}
	}()
	return ch, nil
}

// ensure unused import of bytes is used
var _ = bytes.NewBuffer
