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
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFsCustom404(t *testing.T) {
	t.Parallel()

	// Create a mock filesystem with test files
	mockFS := fstest.MapFS{
		"index.html":        {Data: []byte("<!DOCTYPE html><html><body>SPA</body></html>")},
		"assets/app.js":     {Data: []byte("console.log('app');")},
		"assets/style.css":  {Data: []byte("body { color: red; }")},
		"assets/data.json":  {Data: []byte(`{"key": "value"}`)},
		"assets/icon.svg":   {Data: []byte("<svg></svg>")},
		"assets/logo.png":   {Data: []byte("PNG binary data")},
		"assets/font.woff2": {Data: []byte("WOFF2 binary data")},
	}

	handler := fsCustom404(http.FS(mockFS))

	tests := []struct {
		name                 string
		path                 string
		expectedContentType  string
		expectedBody         string
		expectedContentTypes []string
		expectedStatus       int
		checkNosniff         bool
		checkNoCache         bool
	}{
		{
			name:                "serves index.html at root",
			path:                "/",
			expectedStatus:      http.StatusOK,
			expectedContentType: "text/html; charset=utf-8",
			expectedBody:        "<!DOCTYPE html>",
			checkNosniff:        true,
			checkNoCache:        true,
		},
		{
			name:           "serves JavaScript with correct MIME",
			path:           "/assets/app.js",
			expectedStatus: http.StatusOK,
			// MIME type varies by OS: Linux/macOS use text/javascript, Windows uses application/javascript
			expectedContentTypes: []string{"text/javascript; charset=utf-8", "application/javascript"},
			expectedBody:         "console.log",
			checkNosniff:         true,
		},
		{
			name:                "serves CSS with correct MIME",
			path:                "/assets/style.css",
			expectedStatus:      http.StatusOK,
			expectedContentType: "text/css; charset=utf-8",
			expectedBody:        "body {",
			checkNosniff:        true,
		},
		{
			name:                "serves JSON with correct MIME",
			path:                "/assets/data.json",
			expectedStatus:      http.StatusOK,
			expectedContentType: "application/json",
			expectedBody:        `"key"`,
			checkNosniff:        true,
		},
		{
			name:                "serves SVG with correct MIME",
			path:                "/assets/icon.svg",
			expectedStatus:      http.StatusOK,
			expectedContentType: "image/svg+xml",
			expectedBody:        "<svg>",
			checkNosniff:        true,
		},
		{
			name:                "serves WOFF2 with correct MIME",
			path:                "/assets/font.woff2",
			expectedStatus:      http.StatusOK,
			expectedContentType: "font/woff2",
			checkNosniff:        true,
		},
		{
			name:                "SPA fallback for unknown path",
			path:                "/some/unknown/route",
			expectedStatus:      http.StatusOK,
			expectedContentType: "text/html; charset=utf-8",
			expectedBody:        "<!DOCTYPE html>",
			checkNosniff:        true,
			checkNoCache:        true,
		},
		{
			name:                "SPA fallback for deep nested path",
			path:                "/app/settings/logs",
			expectedStatus:      http.StatusOK,
			expectedContentType: "text/html; charset=utf-8",
			expectedBody:        "<!DOCTYPE html>",
			checkNosniff:        true,
			checkNoCache:        true,
		},
		{
			name:                "directory request serves index",
			path:                "/assets/",
			expectedStatus:      http.StatusOK,
			expectedContentType: "text/html; charset=utf-8",
			checkNosniff:        true,
			checkNoCache:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			actualContentType := rec.Header().Get("Content-Type")
			if len(tt.expectedContentTypes) > 0 {
				assert.Contains(t, tt.expectedContentTypes, actualContentType,
					"Content-Type %q not in expected types %v", actualContentType, tt.expectedContentTypes)
			} else {
				assert.Equal(t, tt.expectedContentType, actualContentType)
			}

			if tt.expectedBody != "" {
				assert.Contains(t, rec.Body.String(), tt.expectedBody)
			}

			if tt.checkNosniff {
				assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			}

			if tt.checkNoCache {
				assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestFsCustom404_MissingIndex(t *testing.T) {
	t.Parallel()

	// Filesystem with no index.html
	mockFS := fstest.MapFS{
		"other.txt": {Data: []byte("not index")},
	}

	handler := fsCustom404(http.FS(mockFS))

	req := httptest.NewRequest(http.MethodGet, "/unknown", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMimeFallbacks(t *testing.T) {
	t.Parallel()

	// Verify fallbacks for types Go doesn't have built-in
	expectedFallbacks := map[string]string{
		".woff":  "font/woff",
		".woff2": "font/woff2",
	}

	for ext, expected := range expectedFallbacks {
		actual, ok := mimeFallbacks[ext]
		require.True(t, ok, "missing fallback for %s", ext)
		assert.Equal(t, expected, actual, "wrong fallback for %s", ext)
	}
}
