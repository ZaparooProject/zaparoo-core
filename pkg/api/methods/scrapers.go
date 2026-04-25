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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// updateFanout broadcasts ScrapeUpdate events from a single producing goroutine
// to any number of SSE subscriber channels. Once the producing goroutine sends
// the final Done event all subscriber channels are closed.
type updateFanout struct {
	subs map[int]chan scraper.ScrapeUpdate
	next int
	mu   sync.Mutex
	done bool
}

func newUpdateFanout() *updateFanout {
	return &updateFanout{subs: make(map[int]chan scraper.ScrapeUpdate)}
}

// subscribe returns a buffered channel that receives updates.
// If the fanout is already done the returned channel is closed immediately.
func (f *updateFanout) subscribe(buf int) (chan scraper.ScrapeUpdate, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan scraper.ScrapeUpdate, buf)
	if f.done {
		close(ch)
		return ch, -1
	}
	id := f.next
	f.next++
	f.subs[id] = ch
	return ch, id
}

// unsubscribe removes and closes the subscriber channel identified by id.
// Safe to call after the fanout is done (no-op).
func (f *updateFanout) unsubscribe(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.subs[id]; ok {
		delete(f.subs, id)
		close(ch)
	}
}

// broadcast sends u to all live subscribers, dropping events on full channels.
// If u.Done is true all subscriber channels are closed afterwards.
func (f *updateFanout) broadcast(u scraper.ScrapeUpdate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs {
		select {
		case ch <- u:
		default:
			log.Warn().Msg("scraper SSE subscriber channel full, dropping update")
		}
	}
	if u.Done {
		f.done = true
		for id, ch := range f.subs {
			close(ch)
			delete(f.subs, id)
		}
	}
}

// run reads from src and broadcasts every event to all subscribers.
// Blocks until src is closed; always ensures a final Done event is sent.
func (f *updateFanout) run(src <-chan scraper.ScrapeUpdate) {
	for u := range src {
		f.broadcast(u)
	}
	// Guard against a channel close without a terminal Done event.
	f.mu.Lock()
	already := f.done
	f.mu.Unlock()
	if !already {
		f.broadcast(scraper.ScrapeUpdate{Done: true})
	}
}

// --- JSON shapes ---

// scraperStatusResponse is the JSON shape for GET /api/v1/scrapers list entries.
type scraperStatusResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "idle" or "running"
}

// runScraperRequest is the request body for POST /api/v1/scrapers/{id}/run.
type runScraperRequest struct {
	Systems []string `json:"systems"`
	Force   bool     `json:"force"`
}

// propertyResponse is the JSON shape for properties read endpoints.
type propertyResponse struct {
	TypeTag     string `json:"typeTag"`
	ContentType string `json:"contentType"`
	Text        string `json:"text"`
}

// --- ScraperRegistry ---

// ScraperRegistry holds registered scrapers and manages the single active run.
// Register scrapers before serving requests; the registry is safe for concurrent
// use after that.
type ScraperRegistry struct {
	scrapers map[string]scraper.Scraper
	cancel   context.CancelFunc
	fanout   *updateFanout
	running  string
	mu       sync.Mutex
}

// NewScraperRegistry creates an empty registry.
func NewScraperRegistry() *ScraperRegistry {
	return &ScraperRegistry{scrapers: make(map[string]scraper.Scraper)}
}

// Register adds a scraper to the registry. Call before serving requests.
func (reg *ScraperRegistry) Register(s scraper.Scraper) {
	reg.scrapers[s.ID()] = s
}

// HandleListScrapers returns a handler for GET /api/v1/scrapers.
func (reg *ScraperRegistry) HandleListScrapers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg.mu.Lock()
		running := reg.running
		reg.mu.Unlock()

		out := make([]scraperStatusResponse, 0, len(reg.scrapers))
		for id := range reg.scrapers {
			status := "idle"
			if id == running {
				status = "running"
			}
			out = append(out, scraperStatusResponse{ID: id, Status: status})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// HandleRunScraper returns a handler for POST /api/v1/scrapers/{id}/run.
// appCtx is the application-level context; the scraper gets a cancellable child.
func (reg *ScraperRegistry) HandleRunScraper(appCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		s, ok := reg.scrapers[id]
		if !ok {
			http.Error(w, fmt.Sprintf("scraper %q not found", id), http.StatusNotFound)
			return
		}

		var req runScraperRequest
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
		}

		reg.mu.Lock()
		if reg.running != "" {
			reg.mu.Unlock()
			http.Error(w, fmt.Sprintf("scraper %q is already running", reg.running), http.StatusConflict)
			return
		}

		runCtx, cancel := context.WithCancel(appCtx)
		fo := newUpdateFanout()
		reg.running = id
		reg.cancel = cancel
		reg.fanout = fo
		reg.mu.Unlock()

		opts := scraper.ScrapeOptions{Systems: req.Systems, Force: req.Force}
		ch, err := s.Scrape(runCtx, opts)
		if err != nil {
			cancel()
			reg.mu.Lock()
			reg.running = ""
			reg.cancel = nil
			reg.fanout = nil
			reg.mu.Unlock()
			http.Error(w, fmt.Sprintf("failed to start scraper: %v", err), http.StatusInternalServerError)
			return
		}

		// Fan out updates to all SSE subscribers and reset registry when done.
		go func() {
			fo.run(ch)
			cancel()
			reg.mu.Lock()
			if reg.running == id {
				reg.running = ""
				reg.cancel = nil
				reg.fanout = nil
			}
			reg.mu.Unlock()
			log.Info().Str("scraper", id).Msg("scraper run complete")
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}

// HandleScraperStatus returns a handler for GET /api/v1/scrapers/{id}/status.
// Streams ScrapeUpdate events as Server-Sent Events. If the named scraper is not
// currently running a single done event is sent and the stream closes.
func (reg *ScraperRegistry) HandleScraperStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if _, ok := reg.scrapers[id]; !ok {
			http.Error(w, fmt.Sprintf("scraper %q not found", id), http.StatusNotFound)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Subscribe while holding the lock so we don't miss the final event if
		// the run finishes concurrently.
		reg.mu.Lock()
		var fo *updateFanout
		if reg.running == id {
			fo = reg.fanout
		}
		var subCh chan scraper.ScrapeUpdate
		var subID int
		if fo != nil {
			subCh, subID = fo.subscribe(64)
		}
		reg.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher.Flush()

		// Not running: send an immediate done event and close.
		if subCh == nil {
			writeScrapeSSE(w, scraper.ScrapeUpdate{Done: true})
			flusher.Flush()
			return
		}
		defer fo.unsubscribe(subID)

		for {
			select {
			case <-r.Context().Done():
				return
			case u, more := <-subCh:
				if !more {
					return
				}
				writeScrapeSSE(w, u)
				flusher.Flush()
				if u.Done {
					return
				}
			}
		}
	}
}

// HandleCancelScraper returns a handler for POST /api/v1/scrapers/{id}/cancel.
func (reg *ScraperRegistry) HandleCancelScraper() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if _, ok := reg.scrapers[id]; !ok {
			http.Error(w, fmt.Sprintf("scraper %q not found", id), http.StatusNotFound)
			return
		}

		reg.mu.Lock()
		if reg.running != id {
			reg.mu.Unlock()
			http.Error(w, fmt.Sprintf("scraper %q is not running", id), http.StatusConflict)
			return
		}
		cancel := reg.cancel
		reg.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Properties endpoints ---

// HandleGetMediaTitleProperties returns a handler for
// GET /api/v1/titles/{titleDBID}/properties.
func HandleGetMediaTitleProperties(db database.MediaDBI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbid, err := parseDBID(chi.URLParam(r, "titleDBID"))
		if err != nil {
			http.Error(w, "invalid title ID", http.StatusBadRequest)
			return
		}

		// 404 if title does not exist.
		title, err := db.FindMediaTitleByDBID(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("titleDBID", dbid).Msg("scrapers: FindMediaTitleByDBID error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if title == nil {
			http.Error(w, "title not found", http.StatusNotFound)
			return
		}

		props, err := db.GetMediaTitleProperties(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("titleDBID", dbid).Msg("scrapers: GetMediaTitleProperties error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, toPropertyResponses(props))
	}
}

// HandleGetMediaProperties returns a handler for
// GET /api/v1/media/{mediaDBID}/properties.
func HandleGetMediaProperties(db database.MediaDBI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbid, err := parseDBID(chi.URLParam(r, "mediaDBID"))
		if err != nil {
			http.Error(w, "invalid media ID", http.StatusBadRequest)
			return
		}

		// 404 if media does not exist.
		result, err := db.GetMediaByDBID(r.Context(), dbid)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && result.MediaID == 0) {
			http.Error(w, "media not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Error().Err(err).Int64("mediaDBID", dbid).Msg("scrapers: GetMediaByDBID error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		props, err := db.GetMediaProperties(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("mediaDBID", dbid).Msg("scrapers: GetMediaProperties error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, toPropertyResponses(props))
	}
}

// --- helpers ---

func parseDBID(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid ID: %q", s)
	}
	return v, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("scrapers: failed to marshal response")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		log.Debug().Err(err).Msg("scrapers: response write error")
	}
}

func writeScrapeSSE(w http.ResponseWriter, u scraper.ScrapeUpdate) {
	type ssePayload struct {
		SystemID  string `json:"systemId"`
		Err       string `json:"err,omitempty"`
		FatalErr  string `json:"fatalErr,omitempty"`
		Processed int    `json:"processed"`
		Total     int    `json:"total"`
		Matched   int    `json:"matched"`
		Skipped   int    `json:"skipped"`
		Done      bool   `json:"done"`
	}
	p := ssePayload{
		SystemID:  u.SystemID,
		Processed: u.Processed,
		Total:     u.Total,
		Matched:   u.Matched,
		Skipped:   u.Skipped,
		Done:      u.Done,
	}
	if u.Err != nil {
		p.Err = u.Err.Error()
	}
	if u.FatalErr != nil {
		p.FatalErr = u.FatalErr.Error()
	}
	data, err := json.Marshal(p)
	if err != nil {
		log.Error().Err(err).Msg("scrapers: failed to marshal SSE event")
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		log.Debug().Err(err).Msg("scrapers: SSE write failed")
	}
}

func toPropertyResponses(props []database.MediaProperty) []propertyResponse {
	out := make([]propertyResponse, 0, len(props))
	for _, p := range props {
		out = append(out, propertyResponse{
			TypeTag:     p.TypeTag,
			ContentType: p.ContentType,
			Text:        p.Text,
		})
	}
	return out
}
