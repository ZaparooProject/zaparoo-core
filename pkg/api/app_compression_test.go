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
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compressAppTestFS(t testing.TB, raw fstest.MapFS) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{}
	for name, file := range raw {
		var data bytes.Buffer
		writer := gzip.NewWriter(&data)
		_, err := writer.Write(file.Data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		result[name+".gz"] = &fstest.MapFile{Data: data.Bytes(), ModTime: file.ModTime}
	}
	return result
}

func TestAppEncodings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		header   string
		gzip     int
		identity int
	}{
		{"", 0, 1000},
		{"gzip", 1000, 1000},
		{"br, gzip", 1000, 1000},
		{"GZip; Q=0.8, identity;q=0.3", 800, 300},
		{"gzip;q=0", 0, 1000},
		{"gzip;q=0.5", 500, 1000},
		{"*", 1000, 1000},
		{"*;q=0", 0, 0},
		{"gzip;q=0, *", 0, 1000},
		{"gzip, *;q=0", 1000, 0},
		{"identity;q=1, *;q=0", 0, 1000},
		{"identity;q=0, gzip;q=0", 0, 0},
		{"gzip;q=0.125, identity;q=0.001", 125, 1},
		{"gzip;q=NaN", 0, 1000},
		{"gzip;q=-1", 0, 1000},
		{"gzip;q=1.001", 0, 1000},
		{"gzip;q=0.1234", 0, 1000},
		{"gzip;q=0.2.3", 0, 1000},
		{"gzip;q=0, gzip", 0, 1000},
		{"gzip;q=1.000", 1000, 1000},
		{"gzip;broken", 0, 1000},
		{"gzip;q=0;q=1", 0, 1000},
		{"gzip;q=1;q=0", 0, 1000},
	} {
		t.Run(tt.header, func(t *testing.T) {
			t.Parallel()
			gz, identity := appEncodings([]string{tt.header})
			assert.Equal(t, tt.gzip, gz)
			assert.Equal(t, tt.identity, identity)
		})
	}
	gz, identity := appEncodings([]string{"br", "gzip;q=0.7", "identity;q=0"})
	assert.Equal(t, 700, gz)
	assert.Zero(t, identity)
}

func TestCompressedAppServing(t *testing.T) {
	t.Parallel()
	modified := time.Date(2026, time.June, 4, 12, 0, 0, 0, time.UTC)
	raw := fstest.MapFS{
		"index.html":       {Data: []byte("<!DOCTYPE html><title>app</title>"), ModTime: modified},
		"assets/app.js":    {Data: []byte("console.log('compressed app');"), ModTime: modified},
		"assets/unknown":   {Data: []byte("<!DOCTYPE html><title>sniff</title>"), ModTime: modified},
		"assets/font.woff": {Data: []byte("font data"), ModTime: modified},
	}
	compressed := compressAppTestFS(t, raw)
	handler := fsCustom404(http.FS(compressed))
	for _, route := range []string{
		"/", "/index.html", "/settings/deep/link", "/assets/", "/assets/app.js",
		"/assets/unknown", "/assets/font.woff",
	} {
		for _, encoding := range []string{"", "gzip", "gzip;q=0", "br", "gzip, identity;q=0"} {
			t.Run(route+":"+encoding, func(t *testing.T) {
				t.Parallel()
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, route, http.NoBody)
				req.Header.Set("Accept-Encoding", encoding)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
				assert.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
				assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
				name := strings.TrimPrefix(route, "/")
				if _, exists := raw[name]; !exists {
					name = "index.html"
				}
				body := rec.Body.Bytes()
				if encoding == "gzip" || encoding == "gzip, identity;q=0" {
					assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
					assert.Equal(t, compressed[name+".gz"].Data, body)
					reader, err := gzip.NewReader(bytes.NewReader(body))
					require.NoError(t, err)
					body, err = io.ReadAll(reader)
					require.NoError(t, err)
					require.NoError(t, reader.Close())
				} else {
					assert.Empty(t, rec.Header().Get("Content-Encoding"))
				}
				assert.Equal(t, raw[name].Data, body)
				assert.Equal(t, strconv.Itoa(rec.Body.Len()), rec.Header().Get("Content-Length"))
				if name == "index.html" {
					assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
					assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
				} else {
					assert.Empty(t, rec.Header().Get("Cache-Control"))
				}
				if name == "assets/unknown" {
					assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
				}
			})
		}
	}

	for _, tt := range []struct {
		name     string
		method   string
		encoding string
		rangeVal string
		modified string
		status   int
	}{
		{"head gzip", http.MethodHead, "gzip", "", "", http.StatusOK},
		{"head identity", http.MethodHead, "", "", "", http.StatusOK},
		{"conditional gzip", http.MethodGet, "gzip", "", modified.Format(http.TimeFormat), http.StatusNotModified},
		{"conditional identity", http.MethodGet, "", "", modified.Format(http.TimeFormat), http.StatusNotModified},
		{"range identity", http.MethodGet, "gzip", "bytes=0-3", "", http.StatusPartialContent},
		{"range gzip only", http.MethodGet, "gzip, identity;q=0", "bytes=0-3", "", http.StatusOK},
		{"not acceptable", http.MethodGet, "*;q=0", "", "", http.StatusNotAcceptable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/assets/app.js", http.NoBody)
			req.Header.Set("Accept-Encoding", tt.encoding)
			req.Header.Set("Range", tt.rangeVal)
			req.Header.Set("If-Modified-Since", tt.modified)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			assert.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")
			if tt.method == http.MethodHead || tt.status == http.StatusNotModified {
				assert.Empty(t, rec.Body.Bytes())
			}
			if tt.status == http.StatusPartialContent {
				assert.Equal(t, "cons", rec.Body.String())
				assert.Empty(t, rec.Header().Get("Content-Encoding"))
			}
			if tt.method == http.MethodHead && tt.encoding == "gzip" {
				assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
				wantLength := strconv.Itoa(len(compressed["assets/app.js.gz"].Data))
				assert.Equal(t, wantLength, rec.Header().Get("Content-Length"))
			}
			if tt.name == "range gzip only" {
				assert.Equal(t, compressed["assets/app.js.gz"].Data, rec.Body.Bytes())
				assert.Equal(t, "bytes=0-3", req.Header.Get("Range"), "handler must not mutate the request")
			}
		})
	}
}

func TestCompressedAppCorruptFallback(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{[]byte("not gzip"), {0x1f, 0x8b, 8}} {
		handler := fsCustom404(http.FS(fstest.MapFS{"index.html.gz": {Data: data}}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Empty(t, rec.Header().Get("Content-Encoding"))
	}
}

func FuzzAppEncodings(f *testing.F) {
	for _, value := range []string{"gzip", "*;q=0", "gzip;q=1, identity;q=0", "gzip;q=NaN"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		gz, identity := appEncodings([]string{value})
		if gz < 0 || gz > 1000 || identity < 0 || identity > 1000 {
			t.Fatalf("invalid qualities: gzip=%d identity=%d", gz, identity)
		}
	})
}

func BenchmarkCompressedApp(b *testing.B) {
	content := bytes.Repeat([]byte("console.log('app');"), 4096)
	root := compressAppTestFS(b, fstest.MapFS{"app.js": {Data: content}})
	handler := fsCustom404(http.FS(root))
	for _, encoding := range []string{"gzip", "identity"} {
		b.Run(encoding, func(b *testing.B) {
			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/app.js", http.NoBody)
			req.Header.Set("Accept-Encoding", encoding)
			b.ReportAllocs()
			for b.Loop() {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatal(rec.Code)
				}
			}
		})
	}
}
