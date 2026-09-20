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

package helpers

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadLogContent_RequestFormat(t *testing.T) {
	t.Parallel()

	logContent := []byte("test log content\nline 2\nline 3")
	expectedURL := "https://logs.zaparoo.org/abc123.log"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request is constructed correctly
		assert.Equal(t, http.MethodPost, r.Method)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "multipart/form-data", mediaType)

		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify form field name and filename match rustypaste expectations
		assert.Equal(t, "file", part.FormName())
		assert.Equal(t, "core.log", part.FileName())

		body, err := io.ReadAll(part)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, logContent, body)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedURL))
	}))
	defer server.Close()

	url, err := UploadLogContent(logContent, server.URL, server.Client())

	require.NoError(t, err)
	assert.Equal(t, expectedURL, url)
}

func TestUploadLogContent_TrimsResponseWhitespace(t *testing.T) {
	t.Parallel()

	// rustypaste may return URLs with trailing newlines
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("  https://logs.zaparoo.org/xyz.log  \n"))
	}))
	defer server.Close()

	url, err := UploadLogContent([]byte("test"), server.URL, server.Client())

	require.NoError(t, err)
	assert.Equal(t, "https://logs.zaparoo.org/xyz.log", url)
}

func TestUploadLogContent_NonOKStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		responseBody string
		wantInError  []string
		statusCode   int
	}{
		{
			name:         "500 internal server error",
			statusCode:   http.StatusInternalServerError,
			responseBody: "internal error occurred",
			wantInError:  []string{"500", "internal error occurred"},
		},
		{
			name:         "403 forbidden",
			statusCode:   http.StatusForbidden,
			responseBody: "access denied",
			wantInError:  []string{"403", "access denied"},
		},
		{
			name:         "413 payload too large",
			statusCode:   http.StatusRequestEntityTooLarge,
			responseBody: "file exceeds maximum size",
			wantInError:  []string{"413", "file exceeds maximum size"},
		},
		{
			name:         "429 rate limited",
			statusCode:   http.StatusTooManyRequests,
			responseBody: "rate limit exceeded",
			wantInError:  []string{"429", "rate limit exceeded"},
		},
		{
			name:         "empty response body",
			statusCode:   http.StatusBadGateway,
			responseBody: "",
			wantInError:  []string{"502"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			_, err := UploadLogContent([]byte("test"), server.URL, server.Client())

			require.ErrorIs(t, err, ErrUploadStatus)
			for _, want := range tt.wantInError {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestUploadLogContent_EmptyContent(t *testing.T) {
	t.Parallel()

	expectedURL := "https://logs.zaparoo.org/empty.log"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify empty content is still sent correctly
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "multipart/form-data", mediaType)

		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(part)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Empty(t, body, "empty content should result in empty body")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedURL))
	}))
	defer server.Close()

	url, err := UploadLogContent([]byte{}, server.URL, server.Client())

	require.NoError(t, err)
	assert.Equal(t, expectedURL, url)
}

func TestUploadLogContent_LargeContent(t *testing.T) {
	t.Parallel()

	// Create 1MB of log content
	largeContent := make([]byte, 1024*1024)
	for i := range largeContent {
		largeContent[i] = byte('A' + (i % 26))
	}

	expectedURL := "https://logs.zaparoo.org/large.log"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify large content is received correctly
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "multipart/form-data", mediaType)

		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(part)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Len(t, body, len(largeContent), "large content should be fully transmitted")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedURL))
	}))
	defer server.Close()

	url, err := UploadLogContent(largeContent, server.URL, server.Client())

	require.NoError(t, err)
	assert.Equal(t, expectedURL, url)
}

func TestUploadLogContent_InvalidURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expectedErr error
		name        string
		url         string
	}{
		{
			name:        "missing scheme",
			url:         "://missing-scheme",
			expectedErr: ErrUploadPrepare,
		},
		{
			name:        "empty URL",
			url:         "",
			expectedErr: ErrUploadConnect, // Empty URL passes request creation but fails at Do()
		},
		{
			name:        "invalid scheme",
			url:         "notascheme://example.com",
			expectedErr: ErrUploadConnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := UploadLogContent([]byte("test"), tt.url, &http.Client{})

			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestUploadLogContent_ConnectionError(t *testing.T) {
	t.Parallel()

	_, err := UploadLogContent([]byte("test"), "http://localhost:1", &http.Client{})

	require.ErrorIs(t, err, ErrUploadConnect)
}

// errorReader is a reader that always returns an error.
type errorReader struct {
	err error
}

func (e errorReader) Read(_ []byte) (int, error) {
	return 0, e.err
}

func (errorReader) Close() error {
	return nil
}

func TestUploadLogContent_ReadResponseError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("simulated read failure")

	// Custom transport that returns a response with a failing body
	client := &http.Client{
		Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       errorReader{err: readErr},
			}, nil
		}),
	}

	_, err := UploadLogContent([]byte("test"), "http://example.com", client)

	require.ErrorIs(t, err, ErrUploadResponse)
	assert.ErrorContains(t, err, "simulated read failure")
}

// roundTripperFunc allows using a function as an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// DescribeUploadFailure is what replaced the message half of the TUI's old
// doUploadLog. It exists so every surface says the same thing about the same
// failure, and so the connection case — the one with an obvious cause and an
// obvious remedy — stays distinguishable from the rest.
func TestDescribeUploadFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	_, err := UploadLogContent([]byte("test"), server.URL, server.Client())
	require.Error(t, err)
	assert.Equal(t, "Unable to upload log file.", DescribeUploadFailure(err))

	_, err = UploadLogContent([]byte("test"), "http://localhost:1", &http.Client{})
	require.Error(t, err)
	assert.Equal(t, "Unable to connect to upload service.", DescribeUploadFailure(err))
}

// UploadLog is what every surface calls, so the pairing it performs — read the
// bundle, then post it — is worth covering as one piece. The bundle is what
// goes up, not just core.log: a crash only exists in the captured stderr.
func TestUploadLog_SendsTheBundleAndReturnsTheLink(t *testing.T) {
	t.Parallel()

	logDir, dataDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(logDir, config.LogFile), []byte(`{"message":"routine"}`+"\n"), 0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(logDir, config.StderrFile), []byte("panic: the interesting part\n"), 0o600,
	))

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: logDir, DataDir: dataDir})

	var received string
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Failures are recorded and asserted on after the request: a failed
		// assertion here would stop the handler's goroutine rather than the
		// test's, leaving the client waiting on a response that never comes.
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			handlerErr = err
			return
		}
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil {
			handlerErr = err
			return
		}
		body, err := io.ReadAll(part)
		if err != nil {
			handlerErr = err
			return
		}
		received = string(body)
		_, _ = w.Write([]byte("https://logs.zaparoo.org/abc123.log\n"))
	}))
	defer server.Close()

	url, err := uploadLogTo(pl, server.URL, server.Client())
	require.NoError(t, err)
	require.NoError(t, handlerErr, "the upload request was not shaped as expected")

	assert.Equal(t, "https://logs.zaparoo.org/abc123.log", url, "the response body is the link")
	assert.Contains(t, received, "routine")
	assert.Contains(t, received, "panic: the interesting part",
		"the captured stderr has to travel with the log")
}

// A missing log file is the one read failure a user can actually cause, by
// asking for an upload before anything has been written.
func TestUploadLog_ReportsAMissingLog(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})

	_, err := uploadLogTo(pl, "http://localhost:1", &http.Client{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUploadConnect,
		"a log that could not be read never reached the network")
}
