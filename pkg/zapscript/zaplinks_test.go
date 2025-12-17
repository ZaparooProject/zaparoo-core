// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package zapscript

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetZapLinkHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform string
	}{
		{
			name:     "mister platform",
			platform: "mister",
		},
		{
			name:     "batocera platform",
			platform: "batocera",
		},
		{
			name:     "linux platform",
			platform: "linux",
		},
		{
			name:     "empty platform",
			platform: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(
				context.Background(), http.MethodGet, "https://example.com", http.NoBody,
			)
			require.NoError(t, err)

			setZapLinkHeaders(req, tt.platform)

			assert.Equal(t, runtime.GOOS, req.Header.Get(HeaderZaparooOS))
			assert.Equal(t, runtime.GOARCH, req.Header.Get(HeaderZaparooArch))
			assert.Equal(t, tt.platform, req.Header.Get(HeaderZaparooPlatform))
		})
	}
}

func TestSetZapLinkHeaders_DoesNotOverwriteOtherHeaders(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "https://example.com", http.NoBody,
	)
	require.NoError(t, err)

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "test-agent")

	setZapLinkHeaders(req, "mister")

	assert.Equal(t, runtime.GOOS, req.Header.Get(HeaderZaparooOS))
	assert.Equal(t, runtime.GOARCH, req.Header.Get(HeaderZaparooArch))
	assert.Equal(t, "mister", req.Header.Get(HeaderZaparooPlatform))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	assert.Equal(t, "test-agent", req.Header.Get("User-Agent"))
}

func TestHeaderConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Zaparoo-OS", HeaderZaparooOS)
	assert.Equal(t, "Zaparoo-Arch", HeaderZaparooArch)
	assert.Equal(t, "Zaparoo-Platform", HeaderZaparooPlatform)
}

func TestWellKnownPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/.well-known/zaparoo", WellKnownPath)
}

func TestAcceptedMimeTypes(t *testing.T) {
	t.Parallel()

	assert.Contains(t, AcceptedMimeTypes, MIMEZaparooZapScript)
	assert.Equal(t, "application/vnd.zaparoo.zapscript", MIMEZaparooZapScript)
}

func TestIsOfflineError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "no such host",
			err:      errors.New("no such host"),
			expected: true,
		},
		{
			name:     "network is unreachable",
			err:      errors.New("network is unreachable"),
			expected: true,
		},
		{
			name:     "host is down",
			err:      errors.New("host is down"),
			expected: true,
		},
		{
			name:     "i/o timeout",
			err:      errors.New("i/o timeout"),
			expected: true,
		},
		{
			name:     "tls handshake timeout",
			err:      errors.New("tls handshake timeout"),
			expected: true,
		},
		{
			name:     "case insensitive - NO SUCH HOST",
			err:      errors.New("NO SUCH HOST"),
			expected: true,
		},
		{
			name:     "wrapped connection refused",
			err:      errors.New("dial tcp: connection refused"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isOfflineError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
