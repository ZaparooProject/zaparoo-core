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

package httpretry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// outcome is one scripted answer: an error, or a response with code. hook
// runs before it is returned.
type outcome struct {
	err  error
	hook func()
	code int
}

// scripted answers each round trip with the next outcome, repeating the last,
// and records the bodies it was sent.
type scripted struct {
	outcomes []outcome
	bodies   []string
}

func (s *scripted) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		body = string(data)
	}
	s.bodies = append(s.bodies, body)
	next := s.outcomes[0]
	if len(s.outcomes) > 1 {
		s.outcomes = s.outcomes[1:]
	}
	if next.hook != nil {
		next.hook()
	}
	if next.err != nil {
		return nil, next.err
	}
	return &http.Response{StatusCode: next.code, Body: io.NopCloser(strings.NewReader("edge error"))}, nil
}

var ok200 = outcome{code: http.StatusOK}

var (
	errDial  = &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	errReset = &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
)

func newRequest(ctx context.Context, t *testing.T, method, body string) *http.Request {
	t.Helper()
	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://online.example/v1/device/x", reader)
	require.NoError(t, err)
	return req
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		first    outcome
		ctx      func(context.Context) context.Context
		name     string
		method   string
		calls    int
		wantCode int
		wantErr  bool
	}{
		{name: "GET after 502", method: http.MethodGet, first: outcome{code: 502}, calls: 2, wantCode: 200},
		{name: "GET after 503", method: http.MethodGet, first: outcome{code: 503}, calls: 2, wantCode: 200},
		{name: "GET after 504", method: http.MethodGet, first: outcome{code: 504}, calls: 2, wantCode: 200},
		{name: "PUT after 502", method: http.MethodPut, first: outcome{code: 502}, calls: 2, wantCode: 200},
		{name: "DELETE after reset", method: http.MethodDelete, first: outcome{err: errReset}, calls: 2, wantCode: 200},
		{name: "GET not after 500", method: http.MethodGet, first: outcome{code: 500}, calls: 1, wantCode: 500},
		{name: "GET not after 429", method: http.MethodGet, first: outcome{code: 429}, calls: 1, wantCode: 429},
		{name: "POST not after 502", method: http.MethodPost, first: outcome{code: 502}, calls: 1, wantCode: 502},
		{name: "POST not after reset", method: http.MethodPost, first: outcome{err: errReset}, calls: 1, wantErr: true},
		{name: "POST after dial error", method: http.MethodPost, first: outcome{err: errDial}, calls: 2, wantCode: 200},
		{
			name: "POST after DNS error", method: http.MethodPost, calls: 2, wantCode: 200,
			first: outcome{err: &net.DNSError{Err: "no such host", Name: "online.example"}},
		},
		{
			name: "marked POST after 502", method: http.MethodPost, ctx: Idempotent, calls: 2, wantCode: 200,
			first: outcome{code: 502},
		},
		{
			name: "marked POST after reset", method: http.MethodPost, ctx: Idempotent, calls: 2, wantCode: 200,
			first: outcome{err: errReset},
		},
		{
			name: "not after deadline", method: http.MethodGet, calls: 1, wantErr: true,
			first: outcome{err: context.DeadlineExceeded},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx(ctx)
			}
			body := ""
			if tt.method != http.MethodGet && tt.method != http.MethodDelete {
				body = `{"hashes":["a"]}`
			}
			base := &scripted{outcomes: []outcome{tt.first, ok200}}
			rt := &transport{base: base, delay: time.Millisecond}

			resp, err := rt.RoundTrip(newRequest(ctx, t, tt.method, body))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				assert.Equal(t, tt.wantCode, resp.StatusCode)
			}
			require.Len(t, base.bodies, tt.calls)
			for _, sent := range base.bodies {
				assert.Equal(t, body, sent, "every attempt sends the whole body")
			}
		})
	}
}

func TestRoundTripRetriesOnlyOnce(t *testing.T) {
	t.Parallel()
	base := &scripted{outcomes: []outcome{{code: http.StatusBadGateway}}}
	rt := &transport{base: base, delay: time.Millisecond}

	resp, err := rt.RoundTrip(newRequest(context.Background(), t, http.MethodGet, ""))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Len(t, base.bodies, 2)
}

func TestRoundTripNoRetryWithoutRewindableBody(t *testing.T) {
	t.Parallel()
	base := &scripted{outcomes: []outcome{{code: http.StatusBadGateway}, ok200}}
	rt := &transport{base: base, delay: time.Millisecond}
	req := newRequest(context.Background(), t, http.MethodPut, "payload")
	req.GetBody = nil

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Len(t, base.bodies, 1)
}

func TestRoundTripStopsWaitingWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := &scripted{outcomes: []outcome{{code: http.StatusBadGateway, hook: cancel}, ok200}}
	rt := &transport{base: base, delay: time.Hour}

	resp, err := rt.RoundTrip(newRequest(ctx, t, http.MethodGet, ""))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Len(t, base.bodies, 1)
}

func TestRoundTripCanceledDuringDelay(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	base := &scripted{outcomes: []outcome{{code: http.StatusBadGateway, hook: func() { close(waiting) }}, ok200}}
	rt := &transport{base: base, delay: time.Hour}

	go func() {
		<-waiting
		cancel()
	}()
	resp, err := rt.RoundTrip(newRequest(ctx, t, http.MethodGet, ""))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Len(t, base.bodies, 1)
}

func TestTransportThroughClient(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "payload" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(strings.Repeat("x", drainLimit*2)))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Transport: &transport{base: server.Client().Transport, delay: time.Millisecond}}
	req, err := http.NewRequestWithContext(
		Idempotent(context.Background()), http.MethodPost, server.URL, strings.NewReader("payload"),
	)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, int32(2), calls.Load())
}

func TestTransportDefaults(t *testing.T) {
	t.Parallel()
	rt, ok := Transport(nil).(*transport)
	require.True(t, ok)
	assert.Equal(t, DefaultDelay, rt.delay)
	assert.Equal(t, http.DefaultTransport, rt.baseTransport())
	rt.CloseIdleConnections()
}
