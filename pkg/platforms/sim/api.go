//go:build linux && !android

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

package sim

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/google/uuid"
)

// API is a small client for a Core instance's JSON-RPC API and event stream,
// shared by the sim CLI and the in-process test harness.
type API struct {
	HTTP    *http.Client
	BaseURL string
	// Key, if set, is sent as a bearer API key. Core's Unix listener requires
	// one; loopback TCP does not.
	Key string
}

// NewAPI returns a client for the API at baseURL, such as
// http://127.0.0.1:17497.
func NewAPI(baseURL string) *API {
	return &API{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Transport: useragent.Transport(nil)}}
}

// NewUnixAPI returns a client for an API served on a Unix socket,
// authenticating with key.
func NewUnixAPI(socket, key string) *API {
	return &API{
		Key:     key,
		BaseURL: "http://core.invalid",
		HTTP: &http.Client{Transport: useragent.Transport(&http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		})},
	}
}

func (a *API) authorize(r *http.Request) {
	if a.Key != "" {
		r.Header.Set("Authorization", "Bearer "+a.Key)
	}
}

// RPCError is an error a JSON-RPC method returned.
type RPCError struct {
	Data    any    `json:"data,omitempty"`
	Method  string `json:"-"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s: error %d: %s", e.Method, e.Code, e.Message)
}

// Call runs a JSON-RPC method. params may be nil, a json.RawMessage or any
// value that encodes to JSON.
func (a *API) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": uuid.NewString(), "method": method}
	switch p := params.(type) {
	case nil:
	case json.RawMessage:
		if len(p) > 0 {
			req["params"] = p
		}
	default:
		req["params"] = p
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/api/v0.1", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	a.authorize(hreq)
	resp, err := a.HTTP.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	var out struct {
		Error  *RPCError       `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out.Error != nil {
		out.Error.Method = method
		return nil, out.Error
	}
	return out.Result, nil
}

// CallInto runs a method and decodes its result into out.
func (a *API) CallInto(ctx context.Context, method string, params, out any) error {
	result, err := a.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(result) == 0 {
		return nil
	}
	if err := json.Unmarshal(result, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// Notification is one API notification from the event stream.
type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// WaitNotification reads the API event stream until match accepts a
// notification. subscribed, if set, runs once the stream is open, so a caller
// can trigger what it waits for without racing the subscription.
func (a *API) WaitNotification(
	ctx context.Context,
	match func(Notification) bool,
	subscribed func() error,
) (Notification, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/api/v0.1/events", http.NoBody)
	if err != nil {
		return Notification{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	a.authorize(req)
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return Notification{}, fmt.Errorf("open event stream: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Notification{}, fmt.Errorf("open event stream: HTTP %d", resp.StatusCode)
	}
	if subscribed != nil {
		if err := subscribed(); err != nil {
			return Notification{}, err
		}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		data, found := strings.CutPrefix(sc.Text(), "data: ")
		if !found {
			continue
		}
		var n Notification
		if json.Unmarshal([]byte(data), &n) != nil {
			continue
		}
		if match(n) {
			return n, nil
		}
	}
	if ctx.Err() != nil {
		return Notification{}, ctx.Err() //nolint:wrapcheck // Timeout or cancellation.
	}
	if err := sc.Err(); err != nil {
		return Notification{}, fmt.Errorf("read event stream: %w", err)
	}
	return Notification{}, errors.New("event stream closed")
}

// MediaStatus returns the media database status.
func (a *API) MediaStatus(ctx context.Context) (*models.IndexingStatusResponse, error) {
	var media models.MediaResponse
	if err := a.CallInto(ctx, models.MethodMedia, nil, &media); err != nil {
		return nil, err
	}
	return &media.Database, nil
}

// Index runs a real media.generate for systems (all when empty) and waits
// for it to finish.
func (a *API) Index(ctx context.Context, systems []string) (*models.IndexingStatusResponse, error) {
	var params any
	if len(systems) > 0 {
		params = map[string]any{"systems": systems}
	}
	before, err := a.MediaStatus(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := a.Call(ctx, models.MethodMediaGenerate, params); err != nil {
		return nil, err
	}
	return a.waitIndexed(ctx, before)
}

// WaitIdle waits until no indexing, optimization or scrape is running. An
// index run queues scrapes of the sources it found and runs them one after
// another. Between two queued scrapes, media.scrape.status reports the last
// one as finished while media.scrape still refuses to start ("scraping jobs
// are pending"), so idle must hold for a full second before it counts. Under
// load the gap can last longer; callers that start a scrape next should retry
// those refusals, as the scenario api command does.
func (a *API) WaitIdle(ctx context.Context) error {
	const settle = 10
	quiet := 0
	for quiet < settle {
		db, err := a.MediaStatus(ctx)
		if err != nil {
			return err
		}
		var scrape models.ScrapingStatusResponse
		if err := a.CallInto(ctx, models.MethodMediaScrapeStatus, nil, &scrape); err != nil {
			return err
		}
		if db.Indexing || db.Optimizing || scrape.Scraping {
			quiet = 0
		} else {
			quiet++
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for media work to finish: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

// waitIndexed polls media status until a run that started after before has
// finished. A poll rather than the media.indexing notification, so a run that
// ends before a subscription opens is not missed.
func (a *API) waitIndexed(ctx context.Context, before *models.IndexingStatusResponse) (
	*models.IndexingStatusResponse, error,
) {
	seenRunning := false
	started := time.Now()
	for {
		db, err := a.MediaStatus(ctx)
		if err != nil {
			return nil, err
		}
		switch {
		case db.Indexing || db.Optimizing:
			seenRunning = true
		case db.Exists && (seenRunning || changed(before, db) || time.Since(started) > 2*time.Second):
			return db, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for indexing: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func changed(before, after *models.IndexingStatusResponse) bool {
	if before == nil || !before.Exists {
		return after.Exists
	}
	b, a := before.TotalMedia, after.TotalMedia
	return (b == nil) != (a == nil) || (b != nil && a != nil && *b != *a)
}

// MatchParams reports whether every KEY=VALUE holds in params. Dotted keys
// reach nested fields. Values compare as their JSON text with strings
// unquoted, so active=true and name=Tetris both work.
func MatchParams(params json.RawMessage, matches []string) bool {
	if len(matches) == 0 {
		return true
	}
	var root any
	if json.Unmarshal(params, &root) != nil {
		return false
	}
	for _, m := range matches {
		key, want, _ := strings.Cut(m, "=")
		v := root
		for part := range strings.SplitSeq(key, ".") {
			obj, isObj := v.(map[string]any)
			if !isObj {
				return false
			}
			if v, isObj = obj[part]; !isObj {
				return false
			}
		}
		got := ""
		if s, isString := v.(string); isString {
			got = s
		} else {
			data, _ := json.Marshal(v)
			got = string(data)
		}
		if got != want {
			return false
		}
	}
	return true
}
