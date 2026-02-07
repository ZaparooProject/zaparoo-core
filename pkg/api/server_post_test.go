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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// createTestPostHandler creates a POST handler with mocked dependencies for testing.
func createTestPostHandler(t *testing.T) (http.HandlerFunc, *MethodMap) {
	t.Helper()

	methodMap := NewMethodMap()

	// Add test methods
	err := methodMap.AddMethod("test.echo", func(_ requests.RequestEnv) (any, error) {
		return map[string]string{"echo": "success"}, nil
	})
	require.NoError(t, err)

	err = methodMap.AddMethod("test.error", func(_ requests.RequestEnv) (any, error) {
		return nil, errors.New("test error")
	})
	require.NoError(t, err)

	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithPort(fs, configDir, 0)
	require.NoError(t, err)

	st, _ := state.NewState(platform, "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
	})

	db := &database.Database{
		UserDB:  helpers.NewMockUserDBI(),
		MediaDB: helpers.NewMockMediaDBI(),
	}

	tokenQueue := make(chan tokens.Token, 1)
	t.Cleanup(func() {
		close(tokenQueue)
	})

	handler := handlePostRequest(methodMap, platform, cfg, st, tokenQueue, db, nil, nil)
	return handler, methodMap
}

// TestHandlePostRequest_ValidRequest tests that a valid JSON-RPC request returns HTTP 200.
func TestHandlePostRequest_ValidRequest(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":"` + uuid.New().String() + `","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "successful request should return HTTP 200")

	// Verify response is valid JSON-RPC
	var resp models.ResponseObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err, "response should be valid JSON")
	require.Equal(t, "2.0", resp.JSONRPC)
	require.NotNil(t, resp.Result, "successful response should have a result")
}

// TestHandlePostRequest_InvalidJSON tests that malformed JSON returns HTTP 200 with JSON-RPC parse error.
func TestHandlePostRequest_InvalidJSON(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(`{invalid json`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// JSON-RPC 2.0 spec: HTTP 200 even for JSON-RPC errors (error is in the body)
	require.Equal(t, http.StatusOK, rr.Code, "JSON-RPC error should still return HTTP 200")

	// Verify JSON-RPC error response
	var resp models.ResponseErrorObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err, "response should be valid JSON")
	require.NotNil(t, resp.Error, "should contain error object")
	require.Equal(t, -32700, resp.Error.Code, "should be parse error code")
}

// TestHandlePostRequest_UnknownMethod tests that an unknown method returns HTTP 200 with method not found error.
func TestHandlePostRequest_UnknownMethod(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":"` + uuid.New().String() + `","method":"nonexistent.method"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "JSON-RPC error should still return HTTP 200")

	var resp models.ResponseErrorObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32601, resp.Error.Code, "should be method not found error code")
}

// TestHandlePostRequest_WrongContentType tests that non-JSON content type returns HTTP 415.
func TestHandlePostRequest_WrongContentType(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(`{"test":"data"}`))
	req.Header.Set("Content-Type", "text/plain")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusUnsupportedMediaType, rr.Code, "wrong content-type should return 415")
}

// TestHandlePostRequest_ContentTypeWithCharset tests that content-type with charset is accepted.
func TestHandlePostRequest_ContentTypeWithCharset(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":"` + uuid.New().String() + `","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "content-type with charset should be accepted")
}

// TestHandlePostRequest_Notification tests that a notification (request without ID) is handled correctly.
// Per JSON-RPC 2.0 spec: "The Server MUST NOT reply to a Notification"
func TestHandlePostRequest_Notification(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// JSON-RPC notification (no ID field)
	reqBody := `{"jsonrpc":"2.0","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// Server MUST NOT reply to notifications - expect 204 No Content
	require.Equal(t, http.StatusNoContent, rr.Code, "notification should return HTTP 204 No Content")
	require.Empty(t, rr.Body.Bytes(), "notification should have empty response body")
}

// TestHandlePostRequest_MethodError tests that a method returning an error produces correct JSON-RPC error.
func TestHandlePostRequest_MethodError(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":"` + uuid.New().String() + `","method":"test.error"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "method error should still return HTTP 200")

	var resp models.ResponseErrorObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Contains(t, resp.Error.Message, "test error")
}

// TestHandlePostRequest_OversizedBody tests that oversized request bodies are rejected.
func TestHandlePostRequest_OversizedBody(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// Create a body larger than 1MB
	largeBody := strings.Repeat("x", 2<<20) // 2MB
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// Body limit triggers HTTP 413 Request Entity Too Large
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code, "oversized body should return HTTP 413")
	require.Contains(t, rr.Body.String(), "Request body too large", "should indicate body size limit exceeded")
}

// TestHandlePostRequest_EmptyBody tests that an empty request body is handled correctly.
func TestHandlePostRequest_EmptyBody(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// Empty body should return JSON-RPC parse error or invalid request
	require.Equal(t, http.StatusOK, rr.Code, "empty body should return HTTP 200 with JSON-RPC error")

	var resp models.ResponseErrorObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
}

// TestHandlePostRequest_InvalidJSONRPCVersion tests that wrong JSON-RPC version is rejected.
func TestHandlePostRequest_InvalidJSONRPCVersion(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"1.0","id":"` + uuid.New().String() + `","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "invalid version should return HTTP 200 with JSON-RPC error")

	var resp models.ResponseErrorObject
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32600, resp.Error.Code, "should be invalid request error code")
}

// TestHandlePostRequest_StringID tests that string IDs are echoed back correctly.
func TestHandlePostRequest_StringID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":"my-custom-string-id","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "my-custom-string-id", resp["id"], "string ID should be echoed back as string")
}

// TestHandlePostRequest_NumberID tests that number IDs are echoed back correctly.
func TestHandlePostRequest_NumberID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	reqBody := `{"jsonrpc":"2.0","id":12345,"method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.InDelta(t, float64(12345), resp["id"], 0.001, "number ID should be echoed back as number")
}

// TestHandlePostRequest_MissingID tests that missing ID is treated as notification (no response).
// Per JSON-RPC 2.0 spec: "The Server MUST NOT reply to a Notification"
func TestHandlePostRequest_MissingID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// Request without ID field = notification
	reqBody := `{"jsonrpc":"2.0","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	// Server MUST NOT reply to notifications - expect 204 No Content
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Empty(t, rr.Body.Bytes(), "notification should not have response body")
}

// TestHandlePostRequest_NullID tests that explicit null ID is treated as request (must respond with null ID).
func TestHandlePostRequest_NullID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// Request with explicit null ID
	reqBody := `{"jsonrpc":"2.0","id":null,"method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	// Explicit null ID should be echoed back as null
	require.Nil(t, resp["id"], "explicit null ID should be echoed back as null")
	// But it should have a result (unlike notification which wouldn't process)
	require.NotNil(t, resp["result"], "request with null ID should have a result")
}

// TestHandlePostRequest_UUIDStringID tests backward compatibility with UUID string IDs.
func TestHandlePostRequest_UUIDStringID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	testUUID := uuid.New().String()
	reqBody := `{"jsonrpc":"2.0","id":"` + testUUID + `","method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, testUUID, resp["id"], "UUID string ID should be echoed back unchanged")
}

// TestHandlePostRequest_InvalidObjectID tests that object IDs are rejected.
func TestHandlePostRequest_InvalidObjectID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// Object ID is invalid per JSON-RPC spec
	reqBody := `{"jsonrpc":"2.0","id":{"nested":"object"},"method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	// Should return an error response for invalid ID type
	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	// The request should fail to parse due to invalid ID
	require.NotNil(t, resp["error"], "object ID should cause an error")
}

// TestHandlePostRequest_InvalidArrayID tests that array IDs are rejected.
func TestHandlePostRequest_InvalidArrayID(t *testing.T) {
	t.Parallel()

	handler, _ := createTestPostHandler(t)

	// Array ID is invalid per JSON-RPC spec
	reqBody := `{"jsonrpc":"2.0","id":[1,2,3],"method":"test.echo"}`
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	// Should return an error response for invalid ID type
	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	// The request should fail to parse due to invalid ID
	require.NotNil(t, resp["error"], "array ID should cause an error")
}
