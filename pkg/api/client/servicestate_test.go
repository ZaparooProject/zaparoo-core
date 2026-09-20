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

package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Everything that reads this — the daemon's spawn check and the TUI's status
// block — changes what it does for each answer, so each one has to come back
// as itself rather than collapsing to "running" or "not running".
func TestServiceState_ReadsEachStateFromTheHealthRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantState string
	}{
		{name: "ready", body: `{"status":"ok","state":"ready"}`, wantState: ServiceStateReady},
		{name: "starting", body: `{"status":"starting","state":"starting"}`, wantState: ServiceStateStarting},
		{name: "failed", body: `{"status":"error","state":"failed"}`, wantState: ServiceStateFailed},
		{
			// Builds before the JSON payload answered with a bare OK, which
			// only ever came from a fully started service.
			name: "a build older than the state field", body: "OK", wantState: ServiceStateReady,
		},
		{
			// Same reasoning for a payload that parses but says nothing.
			name: "JSON without a state field", body: `{"status":"ok"}`, wantState: ServiceStateReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var asked string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked = r.URL.Path
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			state, ok := ServiceState(testConfigWithPort(t, parseServerPort(t, server)))
			require.True(t, ok, "something answered, so the answer counts")
			assert.Equal(t, "/health", asked, "the state has to come from the unauthenticated route")
			assert.Equal(t, tt.wantState, state)
		})
	}
}

// Nothing listening is the third case, and it has to be distinguishable from a
// service that answered: SpawnDaemon starts a daemon for one and attaches for
// the other.
func TestServiceState_ReportsNothingAnsweredWhenThePortIsClosed(t *testing.T) {
	t.Parallel()

	state, ok := ServiceState(testConfigWithPort(t, unusedPort(t)))
	assert.False(t, ok)
	assert.Empty(t, state)
}
