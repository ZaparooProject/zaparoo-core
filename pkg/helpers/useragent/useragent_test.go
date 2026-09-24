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

package useragent

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTestPlatform(t *testing.T, id string) {
	t.Helper()
	old := platformID.Load()
	t.Cleanup(func() { platformID.Store(old) })
	SetPlatform(id)
}

func TestString(t *testing.T) {
	target := runtime.GOOS + "/" + runtime.GOARCH

	setTestPlatform(t, "")
	assert.Equal(t, "Zaparoo-Core/"+config.AppVersion+" ("+target+")", String())

	SetPlatform("mister")
	assert.Equal(t, "Zaparoo-Core/"+config.AppVersion+" (mister; "+target+")", String())
}

func TestSet(t *testing.T) {
	setTestPlatform(t, "mister")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", http.NoBody)
	Set(req)
	assert.Equal(t, String(), req.Header.Get("User-Agent"))

	req.Header.Set("User-Agent", "custom/1.0")
	Set(req)
	assert.Equal(t, "custom/1.0", req.Header.Get("User-Agent"))
}

func TestHeader(t *testing.T) {
	setTestPlatform(t, "mister")
	assert.Equal(t, http.Header{"User-Agent": {String()}}, Header())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTransport(t *testing.T) {
	setTestPlatform(t, "mister")

	var got string
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("User-Agent")
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: req}, nil
	})
	rt := Transport(base)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", http.NoBody)
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, String(), got)
	assert.Empty(t, req.Header.Get("User-Agent"), "caller's request must not be modified")

	req.Header.Set("User-Agent", "custom/1.0")
	resp, err = rt.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "custom/1.0", got)
}

func TestTransportNilBaseFollowsDefaultTransport(t *testing.T) {
	setTestPlatform(t, "mister")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-User-Agent", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Transport: Transport(nil)}

	var swapped bool
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		swapped = true
		return old.RoundTrip(req)
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.True(t, swapped, "nil base must resolve http.DefaultTransport at request time")
	assert.Equal(t, String(), resp.Header.Get("X-Seen-User-Agent"))
}

type idleCloser struct {
	roundTripFunc
	closed bool
}

func (c *idleCloser) CloseIdleConnections() { c.closed = true }

func TestTransportCloseIdleConnections(t *testing.T) {
	base := &idleCloser{}
	client := &http.Client{Transport: Transport(base)}
	client.CloseIdleConnections()
	assert.True(t, base.closed)
}
