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

package tlsroots

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCertPoolWithFallback_UsesFirstValidBundle(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	missingPath := filepath.Join(tempDir, "missing.pem")
	invalidPath := filepath.Join(tempDir, "invalid.pem")
	validPath := filepath.Join(tempDir, "valid.pem")

	require.NoError(t, os.WriteFile(invalidPath, []byte("not a certificate"), 0o600))
	require.NoError(t, os.WriteFile(validPath, certPEM(t, server.Certificate()), 0o600))

	pool, usedPath, err := CertPoolWithFallback([]string{missingPath, invalidPath, validPath})
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, validPath, usedPath)
}

func TestCertPoolWithFallback_PrefersFallbackOverSSLCertFile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "env.pem")
	fallbackPath := filepath.Join(tempDir, "fallback.pem")
	restoreTLSGlobals(t)
	t.Setenv("SSL_CERT_FILE", envPath)
	require.NoError(t, os.WriteFile(envPath, certPEM(t, server.Certificate()), 0o600))
	require.NoError(t, os.WriteFile(fallbackPath, certPEM(t, server.Certificate()), 0o600))

	pool, usedPath, err := CertPoolWithFallback([]string{fallbackPath})
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, fallbackPath, usedPath)
}

func TestCertPoolWithFallback_UsesSSLCertFileWhenNoFallbackIsValid(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "env.pem")
	missingPath := filepath.Join(tempDir, "missing.pem")
	restoreTLSGlobals(t)
	t.Setenv("SSL_CERT_FILE", envPath)
	require.NoError(t, os.WriteFile(envPath, certPEM(t, server.Certificate()), 0o600))

	pool, usedPath, err := CertPoolWithFallback([]string{missingPath})
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, envPath, usedPath)
}

func TestConfigureDefaults_TrustsFallbackCertificateWithDefaultClient(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "cacert.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM(t, server.Certificate()), 0o600))
	restoreTLSGlobals(t)
	t.Setenv("SSL_CERT_FILE", "")

	usedPath, err := ConfigureDefaults([]string{certPath})
	require.NoError(t, err)
	assert.Equal(t, certPath, usedPath)
	assert.Equal(t, certPath, os.Getenv("SSL_CERT_FILE"))

	resp, err := http.DefaultClient.Get(server.URL) //nolint:gosec,noctx // test server URL
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, resp.Body.Close())
	}()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTransport_TrustsConfiguredRootsWithCustomTransport(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "cacert.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM(t, server.Certificate()), 0o600))
	restoreTLSGlobals(t)
	t.Setenv("SSL_CERT_FILE", "")

	usedPath, err := ConfigureDefaults([]string{certPath})
	require.NoError(t, err)
	assert.Equal(t, certPath, usedPath)

	transport := Transport(&http.Transport{})

	client := &http.Client{Transport: transport}
	resp, err := client.Get(server.URL) //nolint:gosec,noctx // test server URL
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, resp.Body.Close())
	}()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func restoreTLSGlobals(t *testing.T) {
	t.Helper()

	oldDefaultTransport := http.DefaultTransport
	oldDefaultClientTransport := http.DefaultClient.Transport
	configuredRoots.mu.Lock()
	oldPool := configuredRoots.pool
	oldPath := configuredRoots.path
	configuredRoots.mu.Unlock()

	t.Cleanup(func() {
		http.DefaultTransport = oldDefaultTransport
		http.DefaultClient.Transport = oldDefaultClientTransport
		configuredRoots.mu.Lock()
		configuredRoots.pool = oldPool
		configuredRoots.path = oldPath
		configuredRoots.mu.Unlock()
	})
}

func certPEM(t *testing.T, cert *x509.Certificate) []byte {
	t.Helper()

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
}
