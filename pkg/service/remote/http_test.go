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

package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPError_Error(t *testing.T) {
	t.Parallel()

	withCode := &httpError{status: 404, code: "remote_slot_required"}
	assert.Contains(t, withCode.Error(), "404")
	assert.Contains(t, withCode.Error(), "remote_slot_required")

	withoutCode := &httpError{status: 500}
	assert.Contains(t, withoutCode.Error(), "500")
	assert.NotContains(t, withoutCode.Error(), "()")
}

// TestDecodeBoundedJSON pins the two guards that make this safe to call
// against an attacker-controlled or misbehaving RemoteControlBaseURL: a
// response is rejected once it exceeds the byte limit (bounding memory use)
// and rejected if anything follows the first JSON value (no smuggled data).
func TestDecodeBoundedJSON(t *testing.T) {
	t.Parallel()

	t.Run("decodes a value within the limit", func(t *testing.T) {
		t.Parallel()
		var out map[string]string
		err := decodeBoundedJSON(strings.NewReader(`{"a":"b"}`), 1024, &out)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "b"}, out)
	})

	t.Run("rejects a response exceeding the byte limit", func(t *testing.T) {
		t.Parallel()
		var out map[string]string
		body := `{"a":"` + strings.Repeat("x", 100) + `"}`
		err := decodeBoundedJSON(strings.NewReader(body), 10, &out)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "size limit")
	})

	t.Run("rejects trailing JSON after the value", func(t *testing.T) {
		t.Parallel()
		var out map[string]string
		err := decodeBoundedJSON(strings.NewReader(`{"a":"b"}{"c":"d"}`), 1024, &out)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing")
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		t.Parallel()
		var out map[string]string
		err := decodeBoundedJSON(strings.NewReader(`not json`), 1024, &out)
		require.Error(t, err)
	})
}

func TestBuildEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("joins base and request paths", func(t *testing.T) {
		t.Parallel()
		endpoint, err := buildEndpoint("https://api.example.com", "/v1/device/heartbeat")
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com/v1/device/heartbeat", endpoint)
	})

	t.Run("trims a trailing slash on the base URL", func(t *testing.T) {
		t.Parallel()
		endpoint, err := buildEndpoint("https://api.example.com/", "/v1/device/heartbeat")
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com/v1/device/heartbeat", endpoint)
	})

	t.Run("preserves the request path's query string", func(t *testing.T) {
		t.Parallel()
		endpoint, err := buildEndpoint("https://api.example.com", "/v1/device/remote-sessions/wait?timeout=25")
		require.NoError(t, err)
		assert.Equal(t, "https://api.example.com/v1/device/remote-sessions/wait?timeout=25", endpoint)
	})

	t.Run("rejects an invalid base URL", func(t *testing.T) {
		t.Parallel()
		_, err := buildEndpoint("://not-a-url", "/v1/device/heartbeat")
		require.Error(t, err)
	})
}

// TestDoJSON_NoStoredCredentialIsUnauthorized pins that doJSON never sends a
// request when there is no device credential to send: the manager under
// test has no config or auth entry configured at all, so a bug that skipped
// this check would surface as a nil-pointer panic reaching into deps.Config
// instead of the intended errUnauthorized.
func TestDoJSON_NoStoredCredentialIsUnauthorized(t *testing.T) {
	t.Parallel()
	m := &manager{deps: Deps{Config: &config.Instance{}}}
	err := m.doJSON(context.Background(), "GET", "/v1/device/heartbeat", nil, nil)
	require.ErrorIs(t, err, errUnauthorized)
}
