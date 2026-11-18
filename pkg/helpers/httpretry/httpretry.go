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

// Package httpretry retries a request once after a transient failure between
// a CDN and the service behind it, such as a 502 for a single request when
// the CDN could not reach the origin.
package httpretry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// DefaultDelay is how long the transport waits before its one retry: long
// enough for a CDN to pick another origin connection, short enough that the
// caller does not notice.
const DefaultDelay = 1500 * time.Millisecond

// drainLimit bounds how much of a failed response is read so its connection
// can be reused.
const drainLimit = 64 << 10

type idempotentKey struct{}

// Idempotent marks a request as safe to send twice although its method is
// not idempotent, for a POST the service applies the same way however often
// it arrives.
func Idempotent(ctx context.Context) context.Context {
	return context.WithValue(ctx, idempotentKey{}, true)
}

// Transport wraps base so a request is retried once after DefaultDelay when
// it failed before reaching the service (any method), or when a request safe
// to send twice got a 502, 503 or 504 or lost its connection. A nil base
// means http.DefaultTransport.
func Transport(base http.RoundTripper) http.RoundTripper {
	return &transport{base: base, delay: DefaultDelay}
}

type transport struct {
	base  http.RoundTripper
	delay time.Duration
}

func (t *transport) baseTransport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.baseTransport().RoundTrip(req)
	if !shouldRetry(req, resp, err) {
		return resp, err //nolint:wrapcheck // Transparent wrapper.
	}
	body, ok := rewindBody(req)
	if !ok {
		return resp, err //nolint:wrapcheck // Transparent wrapper.
	}

	timer := time.NewTimer(t.delay)
	select {
	case <-req.Context().Done():
		timer.Stop()
		if body != nil {
			_ = body.Close()
		}
		return resp, err //nolint:wrapcheck // Transparent wrapper.
	case <-timer.C:
	}

	event := log.Info().Str("method", req.Method).Str("path", req.URL.Path)
	if resp != nil {
		event = event.Int("status", resp.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
		_ = resp.Body.Close()
	} else {
		event = event.Err(err)
	}
	event.Msg("retrying request after a transient failure")

	retry := req.Clone(req.Context())
	retry.Body = body
	return t.baseTransport().RoundTrip(retry) //nolint:wrapcheck // Transparent wrapper.
}

func (t *transport) CloseIdleConnections() {
	if c, ok := t.baseTransport().(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

func shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if req.Context().Err() != nil {
		return false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return notSent(err) || repeatable(req)
	}
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return repeatable(req)
	default:
		return false
	}
}

// notSent reports whether the request failed before any of it was sent, so
// sending it again cannot repeat its effect.
func notSent(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func repeatable(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	marked, ok := req.Context().Value(idempotentKey{}).(bool)
	return ok && marked
}

// rewindBody returns a fresh copy of the request body for the retry. A body
// that cannot be read again rules the retry out.
func rewindBody(req *http.Request) (io.ReadCloser, bool) {
	if req.Body == nil || req.Body == http.NoBody {
		return req.Body, true
	}
	if req.GetBody == nil {
		return nil, false
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, false
	}
	return body, true
}
