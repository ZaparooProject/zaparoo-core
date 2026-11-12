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

package hoststatus

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func statusServer(t *testing.T, status int, hits *atomic.Int32) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if status == http.StatusFound {
			w.Header().Set("Location", "http://portal.invalid/login")
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte("<html>sign in</html>"))
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// probe uses its own transport. httptest's Server.Close closes the idle
// connections of http.DefaultTransport, which fails a request another parallel
// subtest has just dialled on it.
func probe(urls ...string) InternetState {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	return NewProber(urls, transport).Probe(context.Background())
}

func TestProber_Probe(t *testing.T) {
	t.Parallel()

	t.Run("a 204 is the internet", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, InternetFull, probe(statusServer(t, http.StatusNoContent, nil)))
	})

	t.Run("a page in its place is a portal", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, InternetPortal, probe(statusServer(t, http.StatusOK, nil)))
	})

	t.Run("a redirect is a portal and is not followed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, InternetPortal, probe(statusServer(t, http.StatusFound, nil)))
	})

	t.Run("nothing answering is no internet", func(t *testing.T) {
		t.Parallel()
		// A dial that fails, not the port of a closed server: a parallel
		// subtest's server can be handed that port and answer on it.
		transport := &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("unreachable")
			},
		}
		prober := NewProber([]string{"http://unreachable.invalid"}, transport)
		assert.Equal(t, InternetNone, prober.Probe(context.Background()))
	})

	t.Run("a server error is no internet", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, InternetNone, probe(statusServer(t, http.StatusBadGateway, nil)))
	})

	t.Run("one blocked endpoint does not hide the internet", func(t *testing.T) {
		t.Parallel()
		blocked := statusServer(t, http.StatusForbidden, nil)
		assert.Equal(t, InternetFull, probe(blocked, statusServer(t, http.StatusNoContent, nil)))
	})

	t.Run("probing stops at the first proof", func(t *testing.T) {
		t.Parallel()
		var later atomic.Int32
		first := statusServer(t, http.StatusNoContent, nil)
		second := statusServer(t, http.StatusNoContent, &later)
		assert.Equal(t, InternetFull, probe(first, second))
		assert.Zero(t, later.Load())
	})

	t.Run("no endpoints is no internet", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, InternetNone, probe())
	})

	t.Run("a cancelled probe has no answer", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		prober := NewProber([]string{statusServer(t, http.StatusNoContent, nil)}, nil)
		assert.Empty(t, prober.Probe(ctx))
	})
}
