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

// Package useragent identifies Core on every outbound HTTP request.
package useragent

import (
	"net/http"
	"runtime"
	"sync/atomic"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

const headerName = "User-Agent"

var platformID atomic.Pointer[string]

// SetPlatform records the running platform's ID. It is only known once a
// platform is constructed, so requests made before this omit it.
func SetPlatform(id string) {
	platformID.Store(&id)
}

// String returns the User-Agent value, for example
// "Zaparoo-Core/2.18.0 (mister; linux/arm)".
func String() string {
	target := runtime.GOOS + "/" + runtime.GOARCH
	if id := platformID.Load(); id != nil && *id != "" {
		target = *id + "; " + target
	}
	return "Zaparoo-Core/" + config.AppVersion + " (" + target + ")"
}

// Set adds the User-Agent to req unless the caller already set one.
func Set(req *http.Request) {
	if req.Header.Get(headerName) == "" {
		req.Header.Set(headerName, String())
	}
}

// Header returns a header set holding only the User-Agent, for dialers that
// take headers rather than a request, such as WebSocket handshakes.
func Header() http.Header {
	h := make(http.Header, 1)
	h.Set(headerName, String())
	return h
}

// Transport wraps base so every request carries the User-Agent. A nil base
// uses http.DefaultTransport as it is at request time, so a later swap of the
// default transport (custom TLS roots) still applies.
func Transport(base http.RoundTripper) http.RoundTripper {
	return &transport{base: base}
}

type transport struct {
	base http.RoundTripper
}

func (t *transport) baseTransport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(headerName) == "" {
		// A RoundTripper must not modify the caller's request.
		req = req.Clone(req.Context())
		req.Header.Set(headerName, String())
	}
	return t.baseTransport().RoundTrip(req) //nolint:wrapcheck // Transparent wrapper.
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the base.
func (t *transport) CloseIdleConnections() {
	if c, ok := t.baseTransport().(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
