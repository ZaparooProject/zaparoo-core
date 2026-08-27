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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyAdmissionMiddleware(t *testing.T) {
	t.Parallel()

	//nolint:govet // Test table field order favors readability.
	tests := []struct {
		name       string
		platformID string
		keys       []string
		key        string
		remoteAddr string
		wantStatus int
	}{
		{
			name: "Linux rejects legacy", platformID: platformids.Linux,
			remoteAddr: "192.0.2.1:1234", wantStatus: http.StatusUnauthorized,
		},
		{
			name: "MiSTer admits legacy", platformID: platformids.Mister,
			remoteAddr: "192.0.2.1:1234", wantStatus: http.StatusNoContent,
		},
		{
			name: "Linux admits API key admin", platformID: platformids.Linux,
			keys: []string{"secret"}, key: "secret", remoteAddr: "192.0.2.1:1234",
			wantStatus: http.StatusNoContent,
		},
		{
			name: "localhost remains admitted", platformID: platformids.Linux,
			remoteAddr: "127.0.0.1:1234", wantStatus: http.StatusNoContent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			auth := apimiddleware.NewAuthConfig(func() []string { return tt.keys })
			handler := apimiddleware.HTTPAuthMiddleware(auth)(
				legacyAdmissionMiddleware(tt.platformID)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tt.key != "" {
						assert.True(t, apimiddleware.APIKeyAuthenticated(r))
					}
					w.WriteHeader(http.StatusNoContent)
				})),
			)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			if tt.key != "" {
				req.Header.Set("Authorization", "Bearer "+tt.key)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestMethodLegacyMetadataFailsClosed(t *testing.T) {
	t.Parallel()

	methodMap := NewMethodMap()
	allowed, ok := methodMap.getDefinition(models.MethodRun)
	require.True(t, ok)
	assert.True(t, allowed.legacyAllowed)
	for _, method := range []string{
		models.MethodSettingsAuthClaim,
		models.MethodSettingsAuthStatus,
		models.MethodSettingsAuthLink,
		models.MethodSettingsAuthLinkStatus,
	} {
		definition, found := methodMap.getDefinition(method)
		require.True(t, found, method)
		assert.True(t, definition.legacyAllowed, method)
		assert.True(t, unauthenticatedBootstrapMethods[method], method)
	}
	_, bootstrapErr := handleRequest(methodMap, requests.RequestEnv{
		PlatformID: platformids.Linux,
	}, models.RequestObject{
		JSONRPC: "2.0",
		Method:  models.MethodSettingsAuthLink,
		Params:  json.RawMessage(`{`),
	})
	require.NotNil(t, bootstrapErr)
	assert.Contains(t, bootstrapErr.Message, "invalid params")

	_, legacyErr := handleRequest(methodMap, requests.RequestEnv{
		PlatformID: platformids.Linux,
	}, models.RequestObject{JSONRPC: "2.0", Method: models.MethodRun})
	require.NotNil(t, legacyErr)
	assert.Contains(t, legacyErr.Message, "client role does not permit")

	denied, ok := methodMap.getDefinition(models.MethodUpdateApply)
	require.True(t, ok)
	assert.False(t, denied.legacyAllowed)

	require.NoError(t, methodMap.AddMethod("test.closed", func(requests.RequestEnv) (any, error) {
		return "unexpected", nil
	}))
	result, rpcErr := handleRequest(methodMap, requests.RequestEnv{
		PlatformID: platformids.Mister,
	}, models.RequestObject{JSONRPC: "2.0", Method: "test.closed"})
	assert.Nil(t, result)
	require.NotNil(t, rpcErr)
	assert.Contains(t, rpcErr.Message, "client role does not permit")
}
