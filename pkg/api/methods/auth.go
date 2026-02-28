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

package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

// claimClient is the HTTP client used for claim token redemption.
// It has an explicit timeout to prevent hanging on slow/malicious servers.
var claimClient = &http.Client{
	Timeout: 10 * time.Second,
}

// claimRequest is the body sent to the claim endpoint.
type claimRequest struct {
	Token string `json:"token"`
}

// claimResponse is the expected response from a claim endpoint.
type claimResponse struct {
	Bearer string `json:"bearer"`
}

// wellKnownFetcher fetches and parses a .well-known/zaparoo file from a base URL.
type wellKnownFetcher func(baseURL string) (*zapscript.WellKnown, error)

// HandleSettingsAuthClaim redeems a claim token against a remote auth server and stores
// the resulting credentials in auth.toml. It uses .well-known/zaparoo trust
// discovery to extend the credential to additional trusted domains.
//
//nolint:gocritic // single-use parameter in API handler
func HandleSettingsAuthClaim(env requests.RequestEnv, fetchWK wellKnownFetcher) (any, error) {
	var params models.SettingsAuthClaimParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Extract root domain (scheme + host) from the claim URL
	claimURL, err := url.Parse(params.ClaimURL)
	if err != nil {
		return nil, fmt.Errorf("invalid claim URL: %w", err)
	}
	if claimURL.Scheme != "https" {
		return nil, errors.New("claim URL must use HTTPS")
	}
	rootDomain := "https://" + claimURL.Host

	// Validate the root domain supports auth before redeeming the claim
	// token. This avoids consuming a one-shot token when the domain can't
	// support auth (deterministic failure), at the cost of a small race
	// window between the well-known check and the claim redemption.
	wk, err := fetchWK(rootDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch well-known for %s: %w", rootDomain, err)
	}
	if wk.Auth != 1 {
		return nil, fmt.Errorf("domain %s does not support auth (auth=%d)", rootDomain, wk.Auth)
	}

	// Update ZapLink cache for root domain
	if env.Database != nil {
		if updateErr := env.Database.UserDB.UpdateZapLinkHost(
			rootDomain, wk.ZapScript,
		); updateErr != nil {
			return nil, fmt.Errorf("failed to update zaplink host cache: %w", updateErr)
		}
	}

	// Redeem the claim token now that the domain is validated
	platform := env.Platform.ID()
	bearer, err := redeemClaimToken(env.Context, params.ClaimURL, params.Token, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to redeem claim token: %w", err)
	}

	// Persist the credential
	entry := config.CredentialEntry{Bearer: bearer}
	storedDomains := []string{rootDomain}

	saveErr := env.Config.SaveAuthEntry(rootDomain, entry)
	if saveErr != nil {
		return nil, fmt.Errorf("failed to save auth entry: %w", saveErr)
	}

	// Check trusted related domains (cap to limit SSRF amplification)
	const maxTrustedDomains = 10
	trusted := wk.Trusted
	if len(trusted) > maxTrustedDomains {
		trusted = trusted[:maxTrustedDomains]
	}
	for _, related := range trusted {
		relatedDomain := "https://" + related
		if confirmRelatedTrust(relatedDomain, rootDomain, env.Database, fetchWK) {
			if saveErr := env.Config.SaveAuthEntry(relatedDomain, entry); saveErr != nil {
				log.Warn().Err(saveErr).Str("related", related).
					Msg("failed to save auth entry for related domain")
				continue
			}
			storedDomains = append(storedDomains, relatedDomain)
		}
	}

	log.Info().Strs("domains", storedDomains).Msg("settings.auth.claim completed")
	return models.SettingsAuthClaimResponse{Domains: storedDomains}, nil
}

// redeemClaimToken sends the claim token to the claim URL and returns the
// bearer token from the response.
func redeemClaimToken(
	ctx context.Context,
	claimURL string,
	token string,
	platform string,
) (string, error) {
	body, err := json.Marshal(claimRequest{Token: token})
	if err != nil {
		return "", fmt.Errorf("failed to marshal claim request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, claimURL, bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create claim request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(zapscript.HeaderZaparooOS, runtime.GOOS)
	req.Header.Set(zapscript.HeaderZaparooArch, runtime.GOARCH)
	req.Header.Set(zapscript.HeaderZaparooPlatform, platform)

	resp, err := claimClient.Do(req) //nolint:gosec // G107: claim URL from user input, validated as HTTPS
	if err != nil {
		return "", fmt.Errorf("failed to contact claim server: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing claim response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, helpers.MaxResponseBodySize))
		return "", fmt.Errorf(
			"claim server returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}

	var claim claimResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, helpers.MaxResponseBodySize)).Decode(&claim); err != nil {
		return "", fmt.Errorf("failed to decode claim response: %w", err)
	}

	if claim.Bearer == "" {
		return "", errors.New("claim response missing bearer token")
	}

	return claim.Bearer, nil
}

// confirmRelatedTrust checks if a related domain confirms trust with the root
// domain by fetching its .well-known/zaparoo and verifying the auth and
// trusted fields.
func confirmRelatedTrust(
	relatedDomain string,
	rootDomain string,
	db *database.Database,
	fetchWK wellKnownFetcher,
) bool {
	wk, err := fetchWK(relatedDomain)
	if err != nil {
		log.Warn().Err(err).Str("related", relatedDomain).
			Msg("failed to fetch well-known for related domain")
		return false
	}

	// Update ZapLink cache for related domain
	if db != nil {
		if updateErr := db.UserDB.UpdateZapLinkHost(
			relatedDomain, wk.ZapScript,
		); updateErr != nil {
			log.Warn().Err(updateErr).Str("related", relatedDomain).
				Msg("failed to update zaplink host cache for related domain")
			return false
		}
	}

	if wk.Auth != 1 {
		log.Warn().Str("related", relatedDomain).
			Msg("related domain does not support auth")
		return false
	}

	// Extract root host from root domain URL for comparison
	rootURL, err := url.Parse(rootDomain)
	if err != nil {
		return false
	}

	for _, trusted := range wk.Trusted {
		if strings.EqualFold(trusted, rootURL.Host) {
			return true
		}
	}

	log.Warn().Str("related", relatedDomain).Str("root", rootDomain).
		Msg("related domain does not list root in trusted")
	return false
}
