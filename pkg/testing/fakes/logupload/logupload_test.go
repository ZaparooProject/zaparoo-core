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

package logupload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/logupload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func multipartBody(t *testing.T, field, content string) (string, *bytes.Buffer) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	require.NoError(t, w.WriteField("other", "ignored"))
	part, err := w.CreateFormFile(field, "core.log")
	require.NoError(t, err)
	_, err = io.WriteString(part, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return w.FormDataContentType(), body
}

func do(t *testing.T, method, target, contentType string, body io.Reader) (status int, reply string) {
	t.Helper()
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(out)
}

func TestReceive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body        func(t *testing.T) (string, io.Reader)
		name        string
		path        string
		wantContent string
		status      int
	}{
		{
			name: "multipart log", path: "/", status: http.StatusOK, wantContent: "line 1\nline 2\n",
			body: func(t *testing.T) (string, io.Reader) { return multipartBody(t, "file", "line 1\nline 2\n") },
		},
		{
			name: "missing field", path: "/", status: http.StatusBadRequest,
			body: func(t *testing.T) (string, io.Reader) { return multipartBody(t, "log", "x") },
		},
		{
			name: "not multipart", path: "/", status: http.StatusBadRequest,
			body: func(*testing.T) (string, io.Reader) { return "text/plain", strings.NewReader("x") },
		},
		{
			name: "broken multipart", path: "/", status: http.StatusBadRequest,
			body: func(*testing.T) (string, io.Reader) {
				return "multipart/form-data; boundary=zzz", strings.NewReader("garbage")
			},
		},
		{
			name: "other path", path: "/upload", status: http.StatusNotFound,
			body: func(t *testing.T) (string, io.Reader) { return multipartBody(t, "file", "x") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := logupload.New(logupload.Options{})
			ts := httptest.NewServer(s.Handler())
			t.Cleanup(ts.Close)
			contentType, body := tt.body(t)
			status, out := do(t, http.MethodPost, ts.URL+tt.path, contentType, body)
			require.Equal(t, tt.status, status, out)
			if tt.status != http.StatusOK {
				assert.Empty(t, s.Uploads())
				return
			}
			link := strings.TrimSpace(out)
			assert.Equal(t, ts.URL+"/1.log", link, "unstarted servers link back to the request host")
			uploads := s.Uploads()
			require.Len(t, uploads, 1)
			assert.Equal(t, "core.log", uploads[0].Filename)
			assert.Equal(t, tt.wantContent, string(uploads[0].Content))
			assert.Equal(t, len(tt.wantContent), uploads[0].Size)

			status, served := do(t, http.MethodGet, link, "", nil)
			assert.Equal(t, http.StatusOK, status)
			assert.Equal(t, tt.wantContent, served)
		})
	}
}

func TestLimitAndFaults(t *testing.T) {
	t.Parallel()
	s := logupload.New(logupload.Options{Limit: 2})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	for _, c := range []string{"a", "b", "c"} {
		ct, body := multipartBody(t, "file", c)
		status, _ := do(t, http.MethodPost, ts.URL+"/", ct, body)
		require.Equal(t, http.StatusOK, status)
	}
	uploads := s.Uploads()
	require.Len(t, uploads, 2)
	assert.Equal(t, []string{"2", "3"}, []string{uploads[0].ID, uploads[1].ID})
	status, _ := do(t, http.MethodGet, ts.URL+"/1.log", "", nil)
	assert.Equal(t, http.StatusNotFound, status, "dropped uploads are gone")

	require.NoError(t, s.SetFault(logupload.Fault{Status: http.StatusServiceUnavailable}))
	ct, body := multipartBody(t, "file", "d")
	status, _ = do(t, http.MethodPost, ts.URL+"/", ct, body)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Len(t, s.Uploads(), 2)
	s.ClearFault()
	s.ClearUploads()
	assert.Empty(t, s.Uploads())
}

func TestControlAndCLI(t *testing.T) {
	t.Parallel()
	s := logupload.New(logupload.Options{})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	ct, body := multipartBody(t, "file", "hello log")
	status, _ := do(t, http.MethodPost, ts.URL+"/", ct, body)
	require.Equal(t, http.StatusOK, status)
	call := fakehttp.HandlerCaller(s.ControlHandler())
	ctx := context.Background()

	tests := []struct {
		check   func(t *testing.T, out string)
		name    string
		wantErr string
		args    []string
	}{
		{name: "requests", args: []string{"requests"}, check: func(t *testing.T, out string) {
			var reqs []fakectl.Request
			require.NoError(t, json.Unmarshal([]byte(out), &reqs))
			require.Len(t, reqs, 1)
			assert.Equal(t, http.MethodPost, reqs[0].Method)
		}},
		{name: "uploads", args: []string{"uploads"}, check: func(t *testing.T, out string) {
			var uploads []logupload.Upload
			require.NoError(t, json.Unmarshal([]byte(out), &uploads))
			require.Len(t, uploads, 1)
			assert.Equal(t, "1", uploads[0].ID)
			assert.NotContains(t, out, "hello log", "the listing leaves content out")
		}},
		{name: "uploads get", args: []string{"uploads", "get", "1"}, check: func(t *testing.T, out string) {
			assert.Contains(t, out, `"content":"hello log"`)
		}},
		{name: "uploads get missing", args: []string{"uploads", "get", "9"}, wantErr: "status 404"},
		{name: "fault delay", args: []string{"fault", "delay", "10"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"fault":{"delay_ms":10}}`, out)
		}},
		{name: "fault clear", args: []string{"fault", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
		}},
		{name: "uploads clear", args: []string{"uploads", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
			assert.Empty(t, s.Uploads())
		}},
		{name: "uploads empty", args: []string{"uploads"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `[]`, out)
		}},
		{name: "uploads usage", args: []string{"uploads", "get"}, wantErr: "usage: uploads"},
		{name: "unknown", args: []string{"nope"}, wantErr: "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := logupload.CLI(ctx, tt.args, call, &out)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, strings.TrimSpace(out.String()))
		})
	}
}

// TestCoreUploadLogAgainstFake sends Core's real log bundle through
// helpers.UploadLog to the fake. Not parallel: the upload target is global.
func TestCoreUploadLogAgainstFake(t *testing.T) { //nolint:paralleltest // Global upload target.
	s := logupload.New(logupload.Options{})
	base, err := s.Start("127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	restore := helpers.SetLogUploadURL(base + "/")
	defer restore()

	logDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(logDir, config.LogFile),
		[]byte(`{"level":"info","message":"from the sim"}`+"\n"), 0o600))
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: logDir, DataDir: t.TempDir()})

	link, err := helpers.UploadLog(pl)
	require.NoError(t, err)
	assert.Equal(t, base+"/1.log", link)

	uploads := s.Uploads()
	require.Len(t, uploads, 1)
	assert.Equal(t, "core.log", uploads[0].Filename)
	assert.Contains(t, string(uploads[0].Content), "from the sim")

	status, served := do(t, http.MethodGet, link, "", nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, served, "from the sim")
}
