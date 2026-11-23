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

package online

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// controlCaller drives a control handler the way the sim's control client
// does, with paths relative to the mount point.
func controlCaller(h http.Handler) fakectl.Caller {
	return func(ctx context.Context, method, path string, body []byte) ([]byte, error) {
		var reader io.Reader = http.NoBody
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, "/"+strings.TrimLeft(path, "/"), reader)
		if err != nil {
			return nil, fmt.Errorf("building control request: %w", err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code < 200 || rec.Code > 299 {
			return nil, fmt.Errorf("control %s %s: status %d: %s", method, path, rec.Code,
				strings.TrimSpace(rec.Body.String()))
		}
		return rec.Body.Bytes(), nil
	}
}

func newRequest(method, path string, body io.Reader) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), method, path, body)
}

func TestSetFaultValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fault   Fault
		wantErr string
	}{
		{name: "status kind", fault: Fault{PathPrefix: "/v1/device/heartbeat", Kind: FaultServerError}},
		{name: "timeout", fault: Fault{PathPrefix: "/", Kind: FaultTimeout}},
		{name: "relative prefix", fault: Fault{PathPrefix: "v1", Kind: FaultSlow}, wantErr: "must start with /"},
		{name: "unknown kind", fault: Fault{PathPrefix: "/v1", Kind: "418"}, wantErr: "unknown fault kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := New(Options{}).SetFault(tt.fault)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestMatchFaultLongestPrefixWins(t *testing.T) {
	t.Parallel()
	srv := New(Options{})
	require.NoError(t, srv.SetFault(Fault{PathPrefix: "/v1/device", Kind: FaultServerError}))
	require.NoError(t, srv.SetFault(Fault{PathPrefix: "/v1/device/backups", Kind: FaultForbidden}))
	require.NoError(t, srv.SetFault(Fault{PathPrefix: "/v1/device", Kind: FaultUnavailable}))

	tests := []struct {
		path      string
		wantKind  string
		wantFound bool
	}{
		{path: "/v1/device/backups/backup-1", wantKind: FaultForbidden, wantFound: true},
		{path: "/v1/device/heartbeat", wantKind: FaultUnavailable, wantFound: true},
		{path: "/.well-known/zaparoo", wantFound: false},
	}
	for _, tt := range tests {
		fault, found := srv.matchFault(tt.path)
		assert.Equal(t, tt.wantFound, found, tt.path)
		assert.Equal(t, tt.wantKind, fault.Kind, tt.path)
	}
	assert.Len(t, srv.Faults(), 2, "setting a prefix again replaces its fault")

	srv.ClearFault("/v1/device/backups")
	assert.Equal(t, []Fault{{PathPrefix: "/v1/device", Kind: FaultUnavailable}}, srv.Faults())
	srv.ClearFaults()
	assert.Empty(t, srv.Faults())
}

func TestFaultResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind       string
		code       string
		retryAfter string
		status     int
	}{
		{kind: FaultUnauthorized, status: http.StatusUnauthorized, code: "unauthorized"},
		{kind: FaultForbidden, status: http.StatusForbidden, code: "forbidden"},
		{kind: FaultRateLimited, status: http.StatusTooManyRequests, code: "rate_limited", retryAfter: "1"},
		{kind: FaultServerError, status: http.StatusInternalServerError, code: "internal_error"},
		{kind: FaultUnavailable, status: http.StatusServiceUnavailable, code: "unavailable", retryAfter: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()
			srv := New(Options{})
			require.NoError(t, srv.SetFault(Fault{PathPrefix: "/", Kind: tt.kind}))
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, newRequest(http.MethodGet, pathWellKnown, http.NoBody))

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, tt.retryAfter, rec.Header().Get("Retry-After"))
			var body map[string]apiError
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.code, body["error"].Code)
			require.Len(t, srv.Requests(), 1, "faulted requests are still logged")
			assert.Equal(t, tt.status, srv.Requests()[0].Status)
		})
	}
}

func TestFaultSlowServesAfterDelay(t *testing.T) {
	t.Parallel()
	srv := New(Options{SlowDelay: 20 * time.Millisecond})
	require.NoError(t, srv.SetFault(Fault{PathPrefix: "/", Kind: FaultSlow}))
	start := time.Now()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, newRequest(http.MethodGet, pathWellKnown, http.NoBody))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestFaultTimeoutHoldsUntilClientGivesUp(t *testing.T) {
	t.Parallel()
	srv := New(Options{})
	require.NoError(t, srv.SetFault(Fault{PathPrefix: "/", Kind: FaultTimeout}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, pathWellKnown, http.NoBody)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	assert.Empty(t, rec.Body.String(), "a timed-out request gets no answer")
}

func TestCanonicalManifest(t *testing.T) {
	t.Parallel()
	a := ManifestEntry{Path: "a.txt", SHA256: strings.Repeat("a", 64), Size: 1}
	b := ManifestEntry{Path: "b.txt", SHA256: strings.Repeat("b", 64), Size: 2}
	tests := []struct {
		categories map[string][]ManifestEntry
		name       string
		want       string
	}{
		{
			name:       "entries sorted by path",
			categories: map[string][]ManifestEntry{"zaparoo": {b, a}},
			want: `{"format":"` + manifestFormat + `","categories":{"zaparoo":[` +
				`{"path":"a.txt","sha256":"` + a.SHA256 + `","size":1},` +
				`{"path":"b.txt","sha256":"` + b.SHA256 + `","size":2}]}}`,
		},
		{
			name:       "empty categories dropped",
			categories: map[string][]ManifestEntry{"zaparoo": {a}, "saves": {}},
			want: `{"format":"` + manifestFormat + `","categories":{"zaparoo":[` +
				`{"path":"a.txt","sha256":"` + a.SHA256 + `","size":1}]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, hash := canonicalManifest(tt.categories)
			assert.JSONEq(t, tt.want, string(data))
			assert.Equal(t, tt.want, string(data), "the bytes are compact and in Core's field order")
			sum := sha256.Sum256(data)
			assert.Equal(t, hex.EncodeToString(sum[:]), hash)
		})
	}
}

// buildPack lays out objects the way Core's pack writer does.
func buildPack(t *testing.T, objects ...[]byte) []byte {
	t.Helper()
	var body bytes.Buffer
	entries := make([]packEntry, 0, len(objects))
	for _, object := range objects {
		sum := sha256.Sum256(object)
		entries = append(entries, packEntry{
			Hash: hex.EncodeToString(sum[:]), Offset: int64(body.Len()), Length: int64(len(object)),
		})
		body.Write(object)
	}
	footer, err := json.Marshal(entries)
	require.NoError(t, err)
	body.Write(footer)
	var trailer [packTrailerSize]byte
	binary.BigEndian.PutUint32(trailer[:], uint32(len(footer))) //nolint:gosec // Test footers are small.
	body.Write(trailer[:])
	return body.Bytes()
}

func TestParsePack(t *testing.T) {
	t.Parallel()
	valid := buildPack(t, []byte("one"), []byte("two"))
	tampered := bytes.Clone(valid)
	tampered[0] = 'X'
	tests := []struct {
		name    string
		wantErr string
		body    []byte
		want    int
	}{
		{name: "valid", body: valid, want: 2},
		{name: "too short", body: []byte{0, 1}, wantErr: "shorter than its trailer"},
		{name: "footer out of range", body: []byte{0, 0, 0, 99}, wantErr: "out of range"},
		{name: "object changed", body: tampered, wantErr: "does not match its hash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries, err := parsePack(tt.body)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, entries, tt.want)
		})
	}
}

func TestUnknownRouteIsJSONNotFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	New(Options{}).Handler().ServeHTTP(rec, newRequest(http.MethodGet, "/v1/nope", http.NoBody))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"not_found"`)
}

func TestDeviceRoutesNeedAKnownBearer(t *testing.T) {
	t.Parallel()
	srv := New(Options{})
	_, err := srv.SeedDevice("dev-1", "linux", "zpd1_known")
	require.NoError(t, err)
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "unknown", header: "Bearer zpd1_other", want: http.StatusUnauthorized},
		{name: "known", header: "Bearer zpd1_known", want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := newRequest(http.MethodGet, "/v1/device/me", http.NoBody)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			assert.Equal(t, tt.want, rec.Code)
		})
	}
}

func TestLinkActionsNeedAPendingRequest(t *testing.T) {
	t.Parallel()
	srv := New(Options{})
	_, err := srv.ApproveLink("")
	require.ErrorIs(t, err, ErrNoLinkRequest)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, newRequest(http.MethodPost, pathLinkCreate, http.NoBody))
	require.Equal(t, http.StatusCreated, rec.Code)
	requests := srv.LinkRequests()
	require.Len(t, requests, 1)
	id := requests[0].ID

	denied, err := srv.DenyLink(id)
	require.NoError(t, err)
	assert.Equal(t, LinkDenied, denied.Status)
	_, err = srv.ApproveLink(id)
	require.ErrorContains(t, err, "is denied, not pending")
	_, err = srv.ApproveLink("link-missing")
	require.ErrorIs(t, err, ErrNoLinkRequest)
}

func TestEnqueueOperationRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	srv := New(Options{})
	_, err := srv.EnqueueOperation("format_disk", nil)
	require.Error(t, err)
	op, err := srv.EnqueueOperation("echo", json.RawMessage(`{"message":"hi"}`))
	require.NoError(t, err)
	assert.Equal(t, OpQueued, op.Status)
	assert.Len(t, srv.Operations(), 1)
}

func TestCLI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		setup   func(t *testing.T, srv *Server)
		check   func(t *testing.T, srv *Server, out string)
		name    string
		wantErr string
		args    []string
	}{
		{name: "no command", wantErr: "usage: online"},
		{name: "unknown command", args: []string{"frobnicate"}, wantErr: "unknown or malformed command"},
		{
			name: "account set",
			args: []string{"account", "set", "--limited"},
			check: func(t *testing.T, srv *Server, out string) {
				t.Helper()
				assert.Equal(t, Account{Limited: true}, srv.Account())
				assert.JSONEq(t, `{"premium":false,"limited":true}`, out)
			},
		},
		{name: "account set bad flag", args: []string{"account", "set", "--gold"}, wantErr: "--premium and --limited"},
		{
			name: "link approve newest",
			args: []string{"link-approve"},
			setup: func(t *testing.T, srv *Server) {
				t.Helper()
				rec := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rec, newRequest(http.MethodPost, pathLinkCreate, http.NoBody))
				require.Equal(t, http.StatusCreated, rec.Code)
			},
			check: func(t *testing.T, srv *Server, out string) {
				t.Helper()
				assert.Equal(t, LinkApproved, srv.LinkRequests()[0].Status)
				assert.Contains(t, out, `"status": "approved"`)
			},
		},
		{name: "link deny with none pending", args: []string{"link-deny"}, wantErr: "no pending link request"},
		{
			name: "op enqueue",
			args: []string{"op", "enqueue", "echo", `{"message":"hi"}`},
			check: func(t *testing.T, srv *Server, _ string) {
				t.Helper()
				ops := srv.Operations()
				require.Len(t, ops, 1)
				assert.JSONEq(t, `{"message":"hi"}`, string(ops[0].Params))
			},
		},
		{name: "op enqueue bad json", args: []string{"op", "enqueue", "echo", "{"}, wantErr: "not valid JSON"},
		{name: "op enqueue unknown kind", args: []string{"op", "enqueue", "nope"}, wantErr: "status 400"},
		{
			name: "fault set and list",
			args: []string{"fault", "set", "/v1/device/backups", "429"},
			check: func(t *testing.T, srv *Server, out string) {
				t.Helper()
				assert.Equal(t, []Fault{{PathPrefix: "/v1/device/backups", Kind: FaultRateLimited}}, srv.Faults())
				assert.JSONEq(t, `[{"pathPrefix":"/v1/device/backups","kind":"429"}]`, out)
			},
		},
		{name: "fault set bad kind", args: []string{"fault", "set", "/v1", "418"}, wantErr: "unknown fault kind"},
		{
			name: "fault clear one",
			args: []string{"fault", "clear", "/v1/device/heartbeat"},
			setup: func(t *testing.T, srv *Server) {
				t.Helper()
				require.NoError(t, srv.SetFault(Fault{PathPrefix: "/v1/device/heartbeat", Kind: FaultSlow}))
				require.NoError(t, srv.SetFault(Fault{PathPrefix: "/v1/device/me", Kind: FaultSlow}))
			},
			check: func(t *testing.T, srv *Server, _ string) {
				t.Helper()
				assert.Equal(t, []Fault{{PathPrefix: "/v1/device/me", Kind: FaultSlow}}, srv.Faults())
			},
		},
		{
			name: "fault clear all",
			args: []string{"fault", "clear"},
			setup: func(t *testing.T, srv *Server) {
				t.Helper()
				require.NoError(t, srv.SetFault(Fault{PathPrefix: "/", Kind: FaultTimeout}))
			},
			check: func(t *testing.T, srv *Server, out string) {
				t.Helper()
				assert.Empty(t, srv.Faults())
				assert.JSONEq(t, `[]`, out)
			},
		},
		{
			name: "requests clear",
			args: []string{"requests", "clear"},
			setup: func(t *testing.T, srv *Server) {
				t.Helper()
				srv.Handler().ServeHTTP(httptest.NewRecorder(),
					newRequest(http.MethodGet, pathWellKnown, http.NoBody))
				require.Len(t, srv.Requests(), 1)
			},
			check: func(t *testing.T, srv *Server, _ string) {
				t.Helper()
				assert.Empty(t, srv.Requests())
			},
		},
		{
			name: "backups ls empty",
			args: []string{"backups", "ls"},
			check: func(t *testing.T, _ *Server, out string) {
				t.Helper()
				assert.JSONEq(t, `[]`, out)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := New(Options{})
			if tt.setup != nil {
				tt.setup(t, srv)
			}
			var out bytes.Buffer
			err := CLI(context.Background(), tt.args, controlCaller(srv.ControlHandler()), &out)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(out.String(), "\n"))
			if tt.check != nil {
				tt.check(t, srv, out.String())
			}
		})
	}
}

func TestStateDirPersistsAcrossRestarts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := New(Options{StateDir: dir})
	require.NoError(t, first.Err())
	_, err := first.SeedDevice("dev-1", "linux", "zpd1_saved")
	require.NoError(t, err)
	require.NoError(t, first.SetAccount(Account{Premium: true, Limited: true}))
	_, err = first.EnqueueOperation("echo", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.NoError(t, first.Close())
	require.FileExists(t, filepath.Join(dir, stateFile))

	second := New(Options{StateDir: dir})
	require.NoError(t, second.Err())
	assert.Equal(t, Account{Premium: true, Limited: true}, second.Account())
	require.Len(t, second.Devices(), 1)
	assert.Equal(t, "dev-1", second.Devices()[0].ID)
	assert.Len(t, second.Operations(), 1)

	req := newRequest(http.MethodGet, "/v1/device/me", http.NoBody)
	req.Header.Set("Authorization", "Bearer zpd1_saved")
	rec := httptest.NewRecorder()
	second.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "the seeded bearer survives a restart")
}

func TestStateDirCorruptStateIsReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, stateFile), []byte("{"), 0o600))
	srv := New(Options{StateDir: dir})
	require.ErrorContains(t, srv.Err(), "decoding fake online state")
	_, err := srv.Start("127.0.0.1:0")
	require.Error(t, err)
}
