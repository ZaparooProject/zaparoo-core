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

package backup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureOnlineTestAuth(t *testing.T, baseURL string) {
	t.Helper()
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(baseURL): {Bearer: "online-token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)
}

func TestOnlineClientRequiresCredential(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	env := newBackupTestEnv(t, "mister")
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)

	_, err := env.Manager.NewOnlineClient("https://online.example.com")
	require.Error(t, err)
	assert.True(t, IsRemoteUnlinkedError(err))
}

func TestOnlineClientRequests(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	env := newBackupTestEnv(t, "mister")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer online-token", r.Header.Get("Authorization"))
		assert.Equal(t, useragent.String(), r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/v1/device/json":
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "7", r.URL.Query().Get("since"), "a query after the path reaches the server")
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"hello":"world"}`, string(body))
			writeJSON(t, w, map[string]string{"answer": "ok"})
		case "/v1/device/bytes":
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
			assert.Equal(t, "7", r.Header.Get("X-Test-Count"))
			assert.Equal(t, []string{"Bearer online-token"}, r.Header.Values("Authorization"),
				"extra headers cannot replace the credential")
			assert.Equal(t, []string{"application/octet-stream"}, r.Header.Values("Content-Type"),
				"extra headers cannot replace the content type")
			assert.Equal(t, []string{"mister"}, r.Header.Values(zapscript.HeaderZaparooPlatform),
				"extra headers cannot replace the device headers")
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, []byte{1, 2, 3}, body)
			w.WriteHeader(http.StatusNoContent)
		case "/v1/device/missing":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"nothing here"}}`))
		case "/v1/device/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer server.Close()
	configureOnlineTestAuth(t, server.URL)

	client, err := env.Manager.NewOnlineClient(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, server.URL, client.BaseURL())
	assert.Len(t, client.CredentialTag(), 16)
	assert.NotContains(t, client.CredentialTag(), "online-token")
	ctx := context.Background()

	var out struct {
		Answer string `json:"answer"`
	}
	payload := map[string]string{"hello": "world"}
	require.NoError(t, client.DoJSON(ctx, http.MethodPost, "/v1/device/json?since=7", payload, &out))
	assert.Equal(t, "ok", out.Answer)

	headers := http.Header{}
	headers.Set("X-Test-Count", "7")
	headers.Set("Authorization", "Bearer spoofed")
	headers.Set("Content-Type", "text/plain")
	headers.Add(zapscript.HeaderZaparooPlatform, "spoofed")
	require.NoError(t, client.DoBytes(ctx, http.MethodPut, "/v1/device/bytes", []byte{1, 2, 3}, headers, nil))

	err = client.DoJSON(ctx, http.MethodGet, "/v1/device/missing", nil, nil)
	apiErr, ok := AsAPIError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
	assert.Equal(t, "not_found", apiErr.Code)
	assert.Equal(t, "nothing here", apiErr.Message)
	assert.Contains(t, err.Error(), "nothing here")

	err = client.DoJSON(ctx, http.MethodGet, "/v1/device/unauthorized", nil, nil)
	assert.True(t, IsRemoteUnlinkedError(err))
}

func TestOnlineClientRetryRateLimited(t *testing.T) {
	// No t.Parallel(): the auth config is global.
	env := newBackupTestEnv(t, "mister")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configureOnlineTestAuth(t, server.URL)

	client, err := env.Manager.WithRateLimitWaits(time.Millisecond, time.Millisecond, 5*time.Millisecond).
		NewOnlineClient(server.URL)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, client.RetryRateLimited(ctx, func() error {
		return client.DoJSON(ctx, http.MethodGet, "/v1/device/limited", nil, nil)
	}))
	assert.Equal(t, int32(2), calls.Load())

	calls.Store(0)
	err = client.DoJSON(ctx, http.MethodGet, "/v1/device/limited", nil, nil)
	assert.True(t, IsRateLimitedError(err), "without the retry wrapper a 429 is reported as rate limited")
	_, isAPIErr := AsAPIError(err)
	assert.False(t, isAPIErr)
}

func TestRemoteStatusErrorKeepsSentinels(t *testing.T) {
	t.Parallel()
	for code, sentinel := range map[string]error{
		"not_available":      errRemoteNotAvailable,
		"quota_exceeded":     errRemoteQuotaExceeded,
		"payload_too_large":  errRemotePayloadTooLarge,
		"missing_objects":    errRemoteMissingObjects,
		"backup_too_large":   errRemoteBackupTooLarge,
		"integrity_mismatch": errRemoteIntegrityRetry,
	} {
		recorder := httptest.NewRecorder()
		recorder.WriteHeader(http.StatusBadRequest)
		_, _ = recorder.WriteString(`{"error":{"code":"` + code + `","message":"detail"}}`)
		err := remoteStatusError(recorder.Result())
		require.ErrorIs(t, err, sentinel, code)
		assert.Equal(t, sentinel.Error(), err.Error(), code)
		apiErr, ok := AsAPIError(err)
		require.True(t, ok, code)
		assert.Equal(t, code, apiErr.Code)
	}

	recorder := httptest.NewRecorder()
	recorder.WriteHeader(http.StatusBadGateway)
	_, _ = recorder.WriteString("upstream down")
	err := remoteStatusError(recorder.Result())
	assert.Equal(t, "remote backup server returned status 502: upstream down", err.Error())
}
