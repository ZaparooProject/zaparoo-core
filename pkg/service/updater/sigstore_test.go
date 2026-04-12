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

package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checksumLine builds a checksums.txt line compatible with ChecksumValidator.
func checksumLine(t *testing.T, filename string, content []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(content)
	return []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), filename))
}

// sigstoreValidator tests

func TestSigstoreValidator_GetValidationAssetName(t *testing.T) {
	t.Parallel()

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	}

	assert.Equal(t, "checksums.txt", v.GetValidationAssetName("zaparoo-linux_amd64.tar.gz"))
}

func TestSigstoreValidator_ChecksumRunsFirst(t *testing.T) {
	t.Parallel()

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			t.Error("fetch should not be called when checksum fails")
			return nil, nil
		},
	}

	err := v.Validate("test.tar.gz", []byte("binary"), []byte("bad checksum"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum validation")
}

func TestSigstoreValidator_FetchFailure(t *testing.T) {
	t.Parallel()

	release := []byte("test binary content")
	checksums := checksumLine(t, "test.tar.gz", release)

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			return nil, errors.New("network error")
		},
	}

	err := v.Validate("test.tar.gz", release, checksums)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching attestations")
}

func TestSigstoreValidator_VerifyFailure(t *testing.T) {
	t.Parallel()

	release := []byte("test binary content")
	checksums := checksumLine(t, "test.tar.gz", release)

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{json.RawMessage(`{"fake": "bundle"}`)}, nil
		},
		verifierFn: func() (*verify.Verifier, error) { return &verify.Verifier{}, nil },
		verifyFn: func(_ *verify.Verifier, _ json.RawMessage, _ []byte) error {
			return errors.New("verification failed")
		},
	}

	err := v.Validate("test.tar.gz", release, checksums)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid attestation found")
}

func TestSigstoreValidator_EmptyAttestations(t *testing.T) {
	t.Parallel()

	release := []byte("test binary content")
	checksums := checksumLine(t, "test.tar.gz", release)

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			return nil, errors.New("no attestations found for digest")
		},
	}

	err := v.Validate("test.tar.gz", release, checksums)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no attestations found")
}

func TestSigstoreValidator_Success(t *testing.T) {
	t.Parallel()

	release := []byte("test binary content")
	checksums := checksumLine(t, "test.tar.gz", release)

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{json.RawMessage(`{"bundle": true}`)}, nil
		},
		verifierFn: func() (*verify.Verifier, error) { return &verify.Verifier{}, nil },
		verifyFn: func(_ *verify.Verifier, _ json.RawMessage, _ []byte) error {
			return nil
		},
	}

	err := v.Validate("test.tar.gz", release, checksums)
	require.NoError(t, err)
}

func TestSigstoreValidator_MultipleBundlesFirstFails(t *testing.T) {
	t.Parallel()

	release := []byte("test binary content")
	checksums := checksumLine(t, "test.tar.gz", release)
	callCount := 0

	v := &sigstoreValidator{
		inner: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		ctx:   t.Context(),
		fetchFn: func(_ context.Context, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{
				json.RawMessage(`{"bad": "bundle"}`),
				json.RawMessage(`{"good": "bundle"}`),
			}, nil
		},
		verifierFn: func() (*verify.Verifier, error) { return &verify.Verifier{}, nil },
		verifyFn: func(_ *verify.Verifier, _ json.RawMessage, _ []byte) error {
			callCount++
			if callCount == 1 {
				return errors.New("bad bundle")
			}
			return nil
		},
	}

	err := v.Validate("test.tar.gz", release, checksums)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

// doFetchAttestations HTTP tests

func TestDoFetchAttestations_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attestations": [{"bundle": {"test": true}}]}`))
	}))
	defer server.Close()

	bundles, err := doFetchAttestations(t.Context(), server.URL)
	require.NoError(t, err)
	assert.Len(t, bundles, 1)
}

func TestDoFetchAttestations_Non200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	bundles, err := doFetchAttestations(t.Context(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Nil(t, bundles)
}

func TestDoFetchAttestations_EmptyList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attestations": []}`))
	}))
	defer server.Close()

	bundles, err := doFetchAttestations(t.Context(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no attestations found")
	assert.Nil(t, bundles)
}

func TestDoFetchAttestations_MalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	bundles, err := doFetchAttestations(t.Context(), server.URL)
	require.Error(t, err)
	assert.Nil(t, bundles)
}

func TestDoFetchAttestations_CancelledContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attestations": [{"bundle": {}}]}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	bundles, err := doFetchAttestations(ctx, server.URL)
	require.Error(t, err)
	assert.Nil(t, bundles)
}

func TestDoFetchAttestations_OversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write valid JSON prefix then pad beyond maxAttestationSize (4 MiB).
		// LimitReader truncates the body, making json.Unmarshal fail.
		prefix := []byte(`{"attestations": [{"bundle": "`)
		_, _ = w.Write(prefix)
		padding := make([]byte, maxAttestationSize+1024)
		for i := range padding {
			padding[i] = 'A'
		}
		_, _ = w.Write(padding)
		_, _ = w.Write([]byte(`"}]}`))
	}))
	defer server.Close()

	bundles, err := doFetchAttestations(t.Context(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding response")
	assert.Nil(t, bundles)
}
