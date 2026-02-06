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

package tui

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

func TestFormatLogEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "error level",
			input:    `{"level":"error","time":"2025-11-20T13:04:23Z","message":"service failed to start"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:23 service failed to start",
		},
		{
			name:     "warn level",
			input:    `{"level":"warn","time":"2025-11-20T13:04:22Z","message":"config file not found"}`,
			expected: "[yellow::b] WARN[-:-:-] 13:04:22 config file not found",
		},
		{
			name:     "info level",
			input:    `{"level":"info","time":"2025-11-20T13:04:21Z","message":"service starting"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:21 service starting",
		},
		{
			name:     "debug level",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"loading config"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 loading config",
		},
		{
			name:     "invalid JSON",
			input:    "this is not json",
			expected: "this is not json",
		},
		{
			name:     "stack trace line",
			input:    "    at some.function (file.go:123)",
			expected: "    at some.function (file.go:123)",
		},
		{
			name:     "unknown level",
			input:    `{"level":"trace","time":"2025-11-20T13:04:20Z","message":"test"}`,
			expected: "[white::b]TRACE[-:-:-] 13:04:20 test",
		},
		{
			name:     "missing message field",
			input:    `{"level":"info","time":"2025-11-20T13:04:20Z"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:20 ",
		},
		{
			name:     "malformed timestamp",
			input:    `{"level":"info","time":"invalid","message":"test"}`,
			expected: "[green::b] INFO[-:-:-] invalid test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogEntry(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLogContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "multiple lines newest first",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"first"}
{"level":"warn","time":"2025-11-20T13:04:21Z","message":"second"}
{"level":"error","time":"2025-11-20T13:04:22Z","message":"third"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 third\n" +
				"[yellow::b] WARN[-:-:-] 13:04:21 second\n" +
				"[green::b] INFO[-:-:-] 13:04:20 first",
		},
		{
			name:     "empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "\n\n  \n\n",
			expected: "",
		},
		{
			name: "mixed valid and invalid JSON",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"valid"}
not json line
{"level":"error","time":"2025-11-20T13:04:22Z","message":"valid2"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 valid2\nnot json line\n[green::b] INFO[-:-:-] 13:04:20 valid",
		},
		{
			name:     "single line",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"single"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogContent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadLastLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		expected    string
		numLines    int
		expectError bool
	}{
		{
			name:     "read last 3 lines from 5",
			content:  "line1\nline2\nline3\nline4\nline5\n",
			numLines: 3,
			expected: "line3\nline4\nline5",
		},
		{
			name:     "read more lines than exist",
			content:  "line1\nline2\n",
			numLines: 10,
			expected: "line1\nline2",
		},
		{
			name:     "read all lines",
			content:  "line1\nline2\nline3\n",
			numLines: 3,
			expected: "line1\nline2\nline3",
		},
		{
			name:     "empty file",
			content:  "",
			numLines: 10,
			expected: "",
		},
		{
			name:     "single line no newline",
			content:  "single",
			numLines: 10,
			expected: "single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.log")
			err := os.WriteFile(tmpFile, []byte(tt.content), 0o600)
			require.NoError(t, err)

			// Test readLastLines
			result, err := readLastLines(tmpFile, tt.numLines)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestReadLastLinesNonexistentFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "doesnotexist.log")

	result, err := readLastLines(nonexistent, 10)

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "failed to read log file")
}

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

	url, err := uploadLogContent(logContent, server.URL, server.Client())

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

	url, err := uploadLogContent([]byte("test"), server.URL, server.Client())

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

			_, err := uploadLogContent([]byte("test"), server.URL, server.Client())

			require.ErrorIs(t, err, errUploadStatus)
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

	url, err := uploadLogContent([]byte{}, server.URL, server.Client())

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

	url, err := uploadLogContent(largeContent, server.URL, server.Client())

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
			expectedErr: errUploadPrepare,
		},
		{
			name:        "empty URL",
			url:         "",
			expectedErr: errUploadConnect, // Empty URL passes request creation but fails at Do()
		},
		{
			name:        "invalid scheme",
			url:         "notascheme://example.com",
			expectedErr: errUploadConnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := uploadLogContent([]byte("test"), tt.url, &http.Client{})

			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestUploadLogContent_ConnectionError(t *testing.T) {
	t.Parallel()

	_, err := uploadLogContent([]byte("test"), "http://localhost:1", &http.Client{})

	require.ErrorIs(t, err, errUploadConnect)
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

	_, err := uploadLogContent([]byte("test"), "http://example.com", client)

	require.ErrorIs(t, err, errUploadResponse)
	assert.ErrorContains(t, err, "simulated read failure")
}

// roundTripperFunc allows using a function as an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDoUploadLog_Success(t *testing.T) {
	t.Parallel()

	expectedURL := "https://logs.zaparoo.org/abc123.log"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedURL))
	}))
	defer server.Close()

	result := doUploadLog([]byte("test log content"), server.URL, server.Client())

	assert.Equal(t, "Log file URL:\n\n"+expectedURL, result)
}

func TestDoUploadLog_ConnectionError(t *testing.T) {
	t.Parallel()

	result := doUploadLog([]byte("test"), "http://localhost:1", &http.Client{})

	assert.Equal(t, "Unable to connect to upload service.", result)
}

func TestDoUploadLog_UploadError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	result := doUploadLog([]byte("test"), server.URL, server.Client())

	assert.Equal(t, "Unable to upload log file.", result)
}

func TestCopyLogToSd_Success(t *testing.T) {
	t.Parallel()

	// Setup: create temp directories and log file
	logDir := t.TempDir()
	destDir := t.TempDir()

	logContent := []byte("test log content\nline 2\nline 3")
	logPath := filepath.Join(logDir, config.LogFile)
	err := os.WriteFile(logPath, logContent, 0o600)
	require.NoError(t, err)

	destPath := filepath.Join(destDir, "exported.log")

	// Create mock platform with custom LogDir
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		LogDir: logDir,
	})

	// Execute
	result := copyLogToSd(mockPlatform, destPath, "SD Card")

	// Verify the result message
	assert.Contains(t, result, "Copied")
	assert.Contains(t, result, config.LogFile)
	assert.Contains(t, result, "SD Card")

	// Verify file was actually copied with correct content
	//nolint:gosec // destPath is from t.TempDir(), safe in tests
	copiedContent, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, logContent, copiedContent)

	mockPlatform.AssertExpectations(t)
}

func TestCopyLogToSd_Error(t *testing.T) {
	t.Parallel()

	// Setup: create temp directory but no log file (simulates missing log)
	logDir := t.TempDir()
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "exported.log")

	// Create mock platform with LogDir pointing to empty directory
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		LogDir: logDir,
	})

	// Execute - should fail because source log file doesn't exist
	result := copyLogToSd(mockPlatform, destPath, "SD Card")

	// Verify the error message
	assert.Contains(t, result, "Unable to copy")
	assert.Contains(t, result, "SD Card")

	// Verify destination file was not created
	_, err := os.Stat(destPath)
	assert.True(t, os.IsNotExist(err), "Destination file should not exist after failed copy")

	mockPlatform.AssertExpectations(t)
}
