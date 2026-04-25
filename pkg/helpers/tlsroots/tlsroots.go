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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

var configuredRoots struct {
	pool *x509.CertPool
	path string
	mu   syncutil.Mutex
}

// CertPoolWithFallback returns the system certificate pool augmented with the
// first valid PEM bundle from fallbackPaths. SSL_CERT_FILE is used only when no
// explicit fallback path is valid. The returned path is empty when no bundle was
// used.
func CertPoolWithFallback(fallbackPaths []string) (*x509.CertPool, string, error) {
	pool, systemErr := x509.SystemCertPool()
	if pool == nil {
		pool = x509.NewCertPool()
	}

	for _, path := range fallbackPaths {
		if appendBundle(pool, path) {
			return pool, path, nil
		}
	}

	if envPath := os.Getenv("SSL_CERT_FILE"); envPath != "" {
		if appendBundle(pool, envPath) {
			return pool, envPath, nil
		}
	}

	if systemErr != nil {
		return nil, "", fmt.Errorf("failed to load system certificate pool: %w", systemErr)
	}
	return pool, "", nil
}

// ConfigureDefaults configures process-wide HTTP defaults to use system roots
// augmented with a MiSTer fallback CA bundle. It also sets SSL_CERT_FILE when a
// fallback path was selected so later SystemCertPool calls use the same bundle.
func ConfigureDefaults(fallbackPaths []string) (string, error) {
	configuredRoots.mu.Lock()
	defer configuredRoots.mu.Unlock()

	pool, path, err := CertPoolWithFallback(fallbackPaths)
	if err != nil {
		return "", err
	}
	if path != "" && os.Getenv("SSL_CERT_FILE") != path {
		if err := os.Setenv("SSL_CERT_FILE", path); err != nil {
			return "", fmt.Errorf("setting SSL_CERT_FILE: %w", err)
		}
	}

	oldDefault := http.DefaultTransport
	transport := TransportWithRoots(defaultHTTPTransport(), pool)
	http.DefaultTransport = transport
	if http.DefaultClient.Transport == nil || http.DefaultClient.Transport == oldDefault {
		http.DefaultClient.Transport = transport
	}

	configuredRoots.pool = pool
	configuredRoots.path = path
	return path, nil
}

// Transport clones base and applies the roots configured by ConfigureDefaults.
// If ConfigureDefaults has not selected a bundle, Transport returns a clone of
// base using normal Go TLS root behavior.
func Transport(base *http.Transport) *http.Transport {
	configuredRoots.mu.Lock()
	defer configuredRoots.mu.Unlock()

	return TransportWithRoots(base, configuredRoots.pool)
}

// TransportWithRoots clones base and configures it to use roots when non-nil.
func TransportWithRoots(base *http.Transport, roots *x509.CertPool) *http.Transport {
	if base == nil {
		base = defaultHTTPTransport()
	}

	transport := base.Clone()
	if roots == nil {
		return transport
	}

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots} //nolint:gosec // RootCAs preserves TLS verification
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.RootCAs = roots
	}

	return transport
}

func appendBundle(pool *x509.CertPool, path string) bool {
	pem, err := os.ReadFile(path) //nolint:gosec // paths are platform-owned CA bundle locations
	if err != nil {
		return false
	}
	return pool.AppendCertsFromPEM(pem)
}

func defaultHTTPTransport() *http.Transport {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	return defaultTransport
}
