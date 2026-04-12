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
	"io"
	"net/http"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/rs/zerolog/log"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	attestationOwner = "ZaparooProject"
	attestationRepo  = "zaparoo-core"
	oidcIssuer       = "https://token.actions.githubusercontent.com"
	sanRegex         = `^https://github\.com/ZaparooProject/zaparoo-core/`

	// maxAttestationSize limits the GitHub attestation API response to 4 MiB.
	maxAttestationSize = 4 << 20
)

// sigstoreValidator wraps ChecksumValidator with Sigstore attestation
// verification. Checksum validation runs first (cheap, local), then the
// release archive's SHA-256 digest is verified against a Sigstore bundle
// fetched from GitHub's attestation API.
type sigstoreValidator struct {
	inner *selfupdate.ChecksumValidator
	ctx   context.Context //nolint:containedctx // Validator interface doesn't accept context

	// Testing hooks — when nil, the default implementations are used.
	fetchFn    func(ctx context.Context, digest string) ([]json.RawMessage, error)
	verifyFn   func(v *verify.Verifier, bundleJSON json.RawMessage, digest []byte) error
	verifierFn func() (*verify.Verifier, error)
}

func (v *sigstoreValidator) GetValidationAssetName(releaseFilename string) string {
	return v.inner.GetValidationAssetName(releaseFilename)
}

func (v *sigstoreValidator) Validate(filename string, release, asset []byte) error {
	if err := v.inner.Validate(filename, release, asset); err != nil {
		return fmt.Errorf("checksum validation: %w", err)
	}

	digest := sha256.Sum256(release)
	hexDigest := hex.EncodeToString(digest[:])

	fetchFn := v.fetchFn
	if fetchFn == nil {
		fetchFn = fetchAttestations
	}

	bundles, err := fetchFn(v.ctx, hexDigest)
	if err != nil {
		return fmt.Errorf("fetching attestations: %w", err)
	}

	verifierFn := v.verifierFn
	if verifierFn == nil {
		verifierFn = newSigstoreVerifier
	}

	verifier, err := verifierFn()
	if err != nil {
		return fmt.Errorf("creating sigstore verifier: %w", err)
	}

	verifyFn := v.verifyFn
	if verifyFn == nil {
		verifyFn = verifySigstoreBundle
	}

	// At least one attestation must verify successfully.
	var lastErr error
	for _, b := range bundles {
		if err := verifyFn(verifier, b, digest[:]); err != nil {
			lastErr = err
			log.Debug().Err(err).Msg("attestation bundle verification failed, trying next")
			continue
		}
		return nil
	}

	return fmt.Errorf("no valid attestation found: %w", lastErr)
}

// attestationEntry is a single entry in the GitHub attestation API response.
type attestationEntry struct {
	Bundle json.RawMessage `json:"bundle"`
}

// attestationResponse matches the GitHub attestation API response.
type attestationResponse struct {
	Attestations []attestationEntry `json:"attestations"`
}

// fetchAttestations retrieves Sigstore bundles from GitHub's attestation API
// for the given SHA-256 hex digest. The endpoint is public and does not
// require authentication.
func fetchAttestations(ctx context.Context, hexDigest string) ([]json.RawMessage, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/attestations/sha256:%s",
		attestationOwner, attestationRepo, hexDigest,
	)
	return doFetchAttestations(ctx, url)
}

// doFetchAttestations performs the HTTP request and response parsing for
// attestation fetching. Separated from fetchAttestations for testability.
func doFetchAttestations(ctx context.Context, url string) ([]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting attestations: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attestation API returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAttestationSize))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var ar attestationResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(ar.Attestations) == 0 {
		return nil, errors.New("no attestations found for digest")
	}

	bundles := make([]json.RawMessage, 0, len(ar.Attestations))
	for _, a := range ar.Attestations {
		bundles = append(bundles, a.Bundle)
	}

	return bundles, nil
}

// newSigstoreVerifier creates a Verifier configured for the Sigstore Public
// Good instance. The TUF trusted root is fetched once and reused for all
// bundle verifications within a single update check.
func newSigstoreVerifier() (*verify.Verifier, error) {
	trustedRoot, err := root.FetchTrustedRoot()
	if err != nil {
		return nil, fmt.Errorf("fetching trusted root: %w", err)
	}

	verifier, err := verify.NewVerifier(
		trustedRoot,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return nil, fmt.Errorf("creating verifier: %w", err)
	}

	return verifier, nil
}

// verifySigstoreBundle verifies a single Sigstore bundle using the provided
// verifier. Checks the certificate identity matches the expected GitHub
// Actions workflow and the artifact digest matches the release archive.
func verifySigstoreBundle(v *verify.Verifier, bundleJSON json.RawMessage, artifactDigest []byte) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("parsing bundle: %w", err)
	}

	certID, err := verify.NewShortCertificateIdentity(
		oidcIssuer, "", "", sanRegex,
	)
	if err != nil {
		return fmt.Errorf("creating identity: %w", err)
	}

	policy := verify.NewPolicy(
		verify.WithArtifactDigest("sha256", artifactDigest),
		verify.WithCertificateIdentity(certID),
	)

	if _, err := v.Verify(&b, policy); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	return nil
}
