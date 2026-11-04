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

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestRemoteRejectionLogThrottlesPerIPAndReason(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	rejections := newRemoteRejectionLog(clock)

	assert.True(t, rejections.allow("192.0.2.1", rejectReasonUnencrypted))
	assert.False(t, rejections.allow("192.0.2.1", rejectReasonUnencrypted),
		"a reconnecting client is logged once per interval")
	assert.True(t, rejections.allow("192.0.2.1", rejectReasonAPIKey),
		"a different reason from the same client is logged")
	assert.True(t, rejections.allow("192.0.2.2", rejectReasonUnencrypted),
		"another client is logged")

	clock.Advance(remoteRejectionLogInterval - 1)
	assert.False(t, rejections.allow("192.0.2.1", rejectReasonUnencrypted))
	clock.Advance(1)
	assert.True(t, rejections.allow("192.0.2.1", rejectReasonUnencrypted),
		"the client is logged again once the interval has passed")
}

func TestRemoteRejectionLogIsBounded(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	rejections := newRemoteRejectionLog(clock)

	for i := range remoteRejectionLogMaxEntries {
		assert.True(t, rejections.allow(fmt.Sprintf("ip-%d", i), rejectReasonUnencrypted))
	}
	assert.False(t, rejections.allow("one-too-many", rejectReasonUnencrypted),
		"a full table drops new clients rather than growing")
	assert.Len(t, rejections.last, remoteRejectionLogMaxEntries)

	clock.Advance(remoteRejectionLogInterval)
	assert.True(t, rejections.allow("one-too-many", rejectReasonUnencrypted),
		"expired entries are pruned to make room")
	assert.Len(t, rejections.last, 1)
}

func TestLegacyAdmissionMiddlewareLogsRemoteRejection(t *testing.T) {
	logs := captureLogs(t, zerolog.InfoLevel)
	rejections := newRemoteRejectionLog(clockwork.NewFakeClock())
	handler := legacyAdmissionMiddleware(platformids.Windows, rejections)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", http.NoBody)
	request.RemoteAddr = "198.51.100.7:5555"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	output := logs.String()
	assert.Contains(t, output, `"remote_ip":"198.51.100.7"`)
	assert.Contains(t, output, `"path":"/api/events"`)
	assert.Contains(t, output, `"reason":"`+rejectReasonUnencrypted+`"`)
	assert.Contains(t, output, `"hint":"`+rejectHint+`"`)
	assert.Contains(t, output, "rejected remote API connection")
}

func TestRemoteRejectionLogKeysScopedIPv6OnHost(t *testing.T) {
	t.Parallel()
	rejections := newRemoteRejectionLog(clockwork.NewFakeClock())
	for _, addr := range []string{"[fe80::1%eth0]:50123", "[fe80::1%eth0]:50124"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
		request.RemoteAddr = addr
		rejections.record(request, rejectReasonUnencrypted)
	}
	assert.Len(t, rejections.last, 1, "a new source port is the same client")
	assert.False(t, rejections.allow("fe80::1%eth0", rejectReasonUnencrypted))
}
