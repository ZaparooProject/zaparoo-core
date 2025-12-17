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
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// mockHTTPClient implements httpDoer for testing.
type mockHTTPClient struct {
	mock.Mock
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	resp, ok := args.Get(0).(*http.Response)
	if !ok && args.Get(0) != nil {
		return nil, args.Error(1) //nolint:wrapcheck // test mock
	}
	return resp, args.Error(1) //nolint:wrapcheck // test mock
}

func TestPreWarmZapLinkHosts_NoInternet(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	checkInternet := func(int) bool { return false }

	// Should return early without calling the database
	PreWarmZapLinkHosts(db, "mister", checkInternet)

	// No expectations on mockUserDB since we return early
	mockUserDB.AssertNotCalled(t, "GetSupportedZapLinkHosts")
}

func TestPreWarmZapLinkHosts_EmptyHosts(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	checkInternet := func(int) bool { return true }
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil)

	PreWarmZapLinkHosts(db, "mister", checkInternet)

	mockUserDB.AssertExpectations(t)
}

func TestPreWarmZapLinkHosts_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	checkInternet := func(int) bool { return true }
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string(nil), errors.New("db error"))

	PreWarmZapLinkHosts(db, "mister", checkInternet)

	mockUserDB.AssertExpectations(t)
}

func TestPreWarmHost_HTTPError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	mockClient := &mockHTTPClient{}
	mockClient.On("Do", mock.Anything).Return(nil, errors.New("connection refused"))

	// Should handle error gracefully without panicking
	preWarmHost("https://example.com", db, "mister", mockClient)

	mockClient.AssertExpectations(t)
	mockUserDB.AssertNotCalled(t, "UpdateZapLinkHost")
}

func TestPreWarmHost_NonOKStatus(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	mockClient := &mockHTTPClient{}
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	mockClient.On("Do", mock.Anything).Return(resp, nil)

	preWarmHost("https://example.com", db, "mister", mockClient)

	mockClient.AssertExpectations(t)
	// UpdateZapLinkHost should not be called for non-OK status
	mockUserDB.AssertNotCalled(t, "UpdateZapLinkHost")
}

func TestPreWarmHost_Success(t *testing.T) {
	t.Parallel()

	headRequestReceived := false

	// Use httptest to create a mock server that handles both HEAD and GET requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, WellKnownPath))

		// Verify headers are set on all requests
		assert.NotEmpty(t, r.Header.Get(HeaderZaparooOS))
		assert.NotEmpty(t, r.Header.Get(HeaderZaparooArch))
		assert.Equal(t, "mister", r.Header.Get(HeaderZaparooPlatform))

		if r.Method == http.MethodHead {
			headRequestReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}

		// GET request from queryZapLinkSupport - return valid JSON
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"zapscript": 1}`))
			return
		}
	}))
	defer server.Close()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Expect UpdateZapLinkHost to be called on success
	mockUserDB.On("UpdateZapLinkHost", server.URL, 1).Return(nil)

	preWarmHost(server.URL, db, "mister", server.Client())

	assert.True(t, headRequestReceived, "HEAD request should have been made")
	mockUserDB.AssertExpectations(t)
}

func TestPreWarmHost_VerifiesHeaders(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	mockClient := &mockHTTPClient{}
	mockClient.On("Do", mock.Anything).Run(func(args mock.Arguments) {
		req, ok := args.Get(0).(*http.Request)
		if ok {
			capturedReq = req
		}
	}).Return(nil, errors.New("test error"))

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	preWarmHost("https://example.com", db, "batocera", mockClient)

	require.NotNil(t, capturedReq)
	assert.Equal(t, http.MethodHead, capturedReq.Method)
	assert.Equal(t, "https://example.com/.well-known/zaparoo", capturedReq.URL.String())
	assert.Equal(t, runtime.GOOS, capturedReq.Header.Get(HeaderZaparooOS))
	assert.Equal(t, runtime.GOARCH, capturedReq.Header.Get(HeaderZaparooArch))
	assert.Equal(t, "batocera", capturedReq.Header.Get(HeaderZaparooPlatform))
}
