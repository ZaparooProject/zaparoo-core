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
package pinup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingServer answers every request with status and records the paths.
type recordingServer struct {
	srv    *httptest.Server
	paths  []string
	mu     syncutil.Mutex
	status int
}

func newRecordingServer(t *testing.T, status int) *recordingServer {
	t.Helper()
	rs := &recordingServer{status: status}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.paths = append(rs.paths, r.URL.Path)
		rs.mu.Unlock()
		w.WriteHeader(rs.status)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *recordingServer) requested() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.paths...)
}

func TestNewRemote(t *testing.T) {
	t.Parallel()

	remote, err := NewRemote("http://127.0.0.1:8095/", http.DefaultClient)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8095", remote.BaseURL(), "trailing slash is dropped")

	_, err = NewRemote("", http.DefaultClient)
	require.Error(t, err)
	_, err = NewRemote("ftp://127.0.0.1", http.DefaultClient)
	require.Error(t, err)
	_, err = NewRemote("http://", http.DefaultClient)
	require.Error(t, err)
	_, err = NewRemote("http://127.0.0.1", nil)
	require.Error(t, err)

	assert.Equal(t, "http://127.0.0.1:8095", ServerURL(CoreServerPort))
}

func TestRemoteRoutes(t *testing.T) {
	t.Parallel()

	rs := newRecordingServer(t, http.StatusOK)
	remote, err := NewRemote(rs.srv.URL, rs.srv.Client())
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, remote.Probe(ctx))
	require.NoError(t, remote.LaunchGame(ctx, 42))
	require.NoError(t, remote.SendEvent(ctx, EventEmuExit))
	assert.Equal(t, []string{
		"/function/getcuritem",
		"/function/launchgame/42",
		"/pupkey/15",
	}, rs.requested())

	require.Error(t, remote.LaunchGame(ctx, 0))
	require.Error(t, remote.SendEvent(ctx, -1))
	assert.Len(t, rs.requested(), 3, "invalid arguments never reach the server")
}

func TestRemoteErrors(t *testing.T) {
	t.Parallel()

	t.Run("non 200 status", func(t *testing.T) {
		t.Parallel()
		rs := newRecordingServer(t, http.StatusNotFound)
		remote, err := NewRemote(rs.srv.URL, rs.srv.Client())
		require.NoError(t, err)

		err = remote.LaunchGame(context.Background(), 42)
		require.ErrorContains(t, err, "404")
		require.ErrorContains(t, err, "/function/launchgame/42")
	})

	t.Run("server down", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		client := srv.Client()
		url := srv.URL
		srv.Close()
		remote, err := NewRemote(url, client)
		require.NoError(t, err)

		require.Error(t, remote.Probe(context.Background()))
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		rs := newRecordingServer(t, http.StatusOK)
		remote, err := NewRemote(rs.srv.URL, rs.srv.Client())
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		require.Error(t, remote.Probe(ctx))
	})
}
