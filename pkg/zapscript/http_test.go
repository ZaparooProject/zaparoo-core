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

package zapscript

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdHTTPGet_AppliesBearerAuth(t *testing.T) {
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		server.URL: {Bearer: "test-bearer-token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{server.URL + "/api/data"},
		},
	}

	result, err := cmdHTTPGet(nil, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)

	select {
	case headers := <-received:
		assert.Equal(t, "Bearer test-bearer-token", headers.Get("Authorization"))
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPGet_AppliesBasicAuth(t *testing.T) {
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		server.URL: {Username: "myuser", Password: "mypass"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{server.URL + "/resource"},
		},
	}

	result, err := cmdHTTPGet(nil, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)

	select {
	case headers := <-received:
		authHeader := headers.Get("Authorization")
		require.True(t, strings.HasPrefix(authHeader, "Basic "))
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
		require.NoError(t, err)
		assert.Equal(t, "myuser:mypass", string(decoded))
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPGet_NoAuthWhenNotConfigured(t *testing.T) {
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.ClearAuthCfgForTesting()
	t.Cleanup(config.ClearAuthCfgForTesting)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{server.URL + "/open"},
		},
	}

	result, err := cmdHTTPGet(nil, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)

	select {
	case headers := <-received:
		assert.Empty(t, headers.Get("Authorization"))
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPPost_AppliesBearerAuth(t *testing.T) {
	type requestCapture struct {
		headers     http.Header
		contentType string
		body        string
	}
	received := make(chan requestCapture, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- requestCapture{
			headers:     r.Header.Clone(),
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		server.URL: {Bearer: "post-token-xyz"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPPost,
			Args: []string{server.URL + "/webhook", "application/json", `{"key":"value"}`},
		},
	}

	result, err := cmdHTTPPost(nil, env)
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)

	select {
	case rc := <-received:
		assert.Equal(t, "Bearer post-token-xyz", rc.headers.Get("Authorization"))
		assert.Equal(t, "application/json", rc.contentType)
		assert.JSONEq(t, `{"key":"value"}`, rc.body)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPGet_AllowListEmpty_AllAllowed(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.ClearAuthCfgForTesting()
	t.Cleanup(config.ClearAuthCfgForTesting)

	cfg := &config.Instance{}
	// No allow list set — should allow all URLs

	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{server.URL + "/open"},
		},
	}

	_, err := cmdHTTPGet(nil, env)
	require.NoError(t, err)

	select {
	case <-received:
		// request went through
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPGet_AllowListBlocks(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`[zapscript]
allow_http = ['https://example\.com/.*']`))

	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{"https://evil.com/attack"},
		},
	}

	_, err := cmdHTTPGet(nil, env)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHTTPNotAllowed)
}

func TestCmdHTTPGet_AllowListPermits(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.ClearAuthCfgForTesting()
	t.Cleanup(config.ClearAuthCfgForTesting)

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[zapscript]\nallow_http = ['%s/.*']", server.URL)))

	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPGet,
			Args: []string{server.URL + "/allowed"},
		},
	}

	_, err := cmdHTTPGet(nil, env)
	require.NoError(t, err)

	select {
	case <-received:
		// request went through
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPPost_AllowListBlocks(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`[zapscript]
allow_http = ['https://example\.com/.*']`))

	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPPost,
			Args: []string{"https://evil.com/attack", "application/json", "{}"},
		},
	}

	_, err := cmdHTTPPost(nil, env)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHTTPNotAllowed)
}

func TestCmdHTTPPost_AllowListPermits(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config.ClearAuthCfgForTesting()
	t.Cleanup(config.ClearAuthCfgForTesting)

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(fmt.Sprintf("[zapscript]\nallow_http = ['%s/.*']", server.URL)))

	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdHTTPPost,
			Args: []string{server.URL + "/allowed", "application/json", `{}`},
		},
	}

	_, err := cmdHTTPPost(nil, env)
	require.NoError(t, err)

	select {
	case <-received:
		// request went through
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for HTTP request")
	}
}

func TestCmdHTTPGet_ArgValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		args    []string
	}{
		{name: "no args", args: nil, wantErr: ErrArgCount},
		{name: "too many args", args: []string{"a", "b"}, wantErr: ErrArgCount},
		{name: "empty arg", args: []string{""}, wantErr: ErrRequiredArgs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := platforms.CmdEnv{
				Cmd: gozapscript.Command{
					Name: gozapscript.ZapScriptCmdHTTPGet,
					Args: tt.args,
				},
			}
			_, err := cmdHTTPGet(nil, env)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCmdHTTPPost_ArgValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		args    []string
	}{
		{name: "no args", args: nil, wantErr: ErrArgCount},
		{name: "one arg", args: []string{"a"}, wantErr: ErrArgCount},
		{name: "two args", args: []string{"a", "b"}, wantErr: ErrArgCount},
		{name: "four args", args: []string{"a", "b", "c", "d"}, wantErr: ErrArgCount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := platforms.CmdEnv{
				Cmd: gozapscript.Command{
					Name: gozapscript.ZapScriptCmdHTTPPost,
					Args: tt.args,
				},
			}
			_, err := cmdHTTPPost(nil, env)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
