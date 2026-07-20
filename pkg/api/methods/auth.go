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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

// claimClient is the HTTP client used for claim token redemption.
// It has an explicit timeout to prevent hanging on slow/malicious servers.
var claimClient = &http.Client{
	Timeout: 10 * time.Second,
}

var revokeRemoteDevice = func(ctx context.Context, manager *backupsvc.Manager) error {
	return manager.RevokeRemoteLink(ctx)
}

// headerZaparooDeviceHint carries the device ID from config on claim
// redemption and link-request creation, so re-linking reuses the same
// server-side device record instead of creating a duplicate.
const headerZaparooDeviceHint = "Zaparoo-Device-Hint"

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

// HandleSettingsAuthStatus reports whether Core has a local bearer for an allowed auth URL.
// It does not validate the token remotely and never exposes token material or credential domains.
//
//nolint:gocritic // single-use parameter in API handler
func HandleSettingsAuthStatus(env requests.RequestEnv) (any, error) {
	var params models.SettingsAuthStatusParams
	if len(env.Params) > 0 {
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	if params.URL == "" {
		return nil, models.ClientErrf("invalid params: url is required")
	}
	configuredBackupURL := env.Config.BackupRemoteBaseURL()
	if !authStatusProbeAllowed(params.URL, configuredBackupURL) {
		return models.SettingsAuthStatusResponse{Linked: false}, nil
	}
	entry := config.LookupAuth(config.GetAuthCfg(), config.BackupAuthLookupURL(params.URL))
	return models.SettingsAuthStatusResponse{Linked: entry != nil && entry.Bearer != ""}, nil
}

func authStatusProbeAllowed(rawURL, configuredBackupURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "https" && slices.Contains(config.OfficialAuthHosts, host) {
		return true
	}
	configured, err := url.Parse(configuredBackupURL)
	if err != nil || configured.Scheme == "" || configured.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Scheme, configured.Scheme) && strings.EqualFold(parsed.Host, configured.Host)
}

// HandleSettingsAuthUnlink removes the device's Zaparoo Online credentials
// and marks remote backup unlinked. The claim/link flow tags every entry it
// creates with the root domain that created it (linked_via), so unlink
// removes the configured backup server's entry plus everything tagged with
// it — whatever the server's trusted list contained at link time.
// Credentials for other domains and API keys are untouched. The official
// backup-server device is revoked before local credentials are removed, so
// unlink never leaves a usable bearer behind.
//
//nolint:gocritic // single-use parameter in API handler
func HandleSettingsAuthUnlink(env requests.RequestEnv) (any, error) {
	if !isLocalOrAdmin(&env) {
		return nil, models.ClientErrf("unlink requires a local or admin client")
	}

	backupManager := backupsvc.NewManager(env.Config, env.Platform, env.Database)
	if env.State != nil {
		backupManager.WithCoordinator(env.State.BackupCoordinator())
	}
	if err := revokeRemoteDevice(env.Context, backupManager); err != nil {
		return nil, fmt.Errorf("failed to revoke remote device link: %w", err)
	}

	creds := config.GetAuthCfg()
	lookup := config.BackupAuthLookupURL(env.Config.BackupRemoteBaseURL())
	removed := []string{}
	for domain, stored := range creds {
		switch {
		case stored.LinkedVia != "" && strings.EqualFold(stored.LinkedVia, lookup):
			removed = append(removed, domain)
		case stored.Bearer == "":
			// Hand-written basic-auth entries are never part of a link.
		case strings.EqualFold(domain, lookup):
			removed = append(removed, domain)
		}
	}
	if len(removed) > 0 {
		sort.Strings(removed)
		if err := env.Config.DeleteAuthEntries(removed); err != nil {
			return nil, fmt.Errorf("failed to remove credentials: %w", err)
		}
	}

	backupManager.MarkRemoteUnlinked()

	log.Info().Strs("domains", removed).Msg("settings.auth.unlink completed")
	return models.SettingsAuthUnlinkResponse{Domains: removed}, nil
}

// HandleSettingsAuthClaim redeems a claim token against a remote auth server and stores
// the resulting credentials in auth.toml. It uses .well-known/zaparoo trust
// discovery to extend the credential to additional trusted domains.
//
//nolint:gocritic // single-use parameter in API handler
func HandleSettingsAuthClaim(env requests.RequestEnv, fetchWK wellKnownFetcher) (any, error) {
	var params models.SettingsAuthClaimParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	var backupCoordinator *backupsvc.Coordinator
	if env.State != nil {
		backupCoordinator = env.State.BackupCoordinator()
	}
	storedDomains, err := performClaim(
		env.Context, env.Config, env.Database, env.Platform,
		params.ClaimURL, params.Token, fetchWK, backupCoordinator,
	)
	if err != nil {
		return nil, err
	}

	log.Info().Strs("domains", storedDomains).Msg("settings.auth.claim completed")
	return models.SettingsAuthClaimResponse{Domains: storedDomains}, nil
}

// performClaim is the shared claim-redemption pipeline used by the
// App-driven forward flow (settings.auth.claim) and the device-driven
// reverse link flow (settings.auth.link): well-known validation, one-shot
// token redemption, credential persistence, and trusted-domain extension.
func performClaim(
	ctx context.Context,
	cfg *config.Instance,
	db *database.Database,
	pl platforms.Platform,
	rawClaimURL, token string,
	fetchWK wellKnownFetcher,
	backupCoordinator *backupsvc.Coordinator,
) ([]string, error) {
	// Extract root domain (scheme + host) from the claim URL
	claimURL, err := url.Parse(rawClaimURL)
	if err != nil {
		return nil, models.ClientErrf("invalid claim URL: %w", err)
	}
	// HTTPS only, with the same private/localhost HTTP allowance the backup
	// base URL gets — for developing against a locally-run API.
	if claimURL.Scheme != "https" {
		rootURL := claimURL.Scheme + "://" + claimURL.Host
		if validateErr := config.ValidateBackupRemoteBaseURL(rootURL); validateErr != nil {
			return nil, models.ClientErrf("claim URL must use HTTPS")
		}
	}
	rootDomain := claimURL.Scheme + "://" + claimURL.Host

	// Validate the root domain supports auth before redeeming the claim
	// token. This avoids consuming a one-shot token when the domain can't
	// support auth (deterministic failure), at the cost of a small race
	// window between the well-known check and the claim redemption.
	wk, err := fetchWK(rootDomain)
	if err != nil {
		return nil, models.ClientErrf("failed to fetch well-known for %s: %w", rootDomain, err)
	}
	if wk.Auth != 1 {
		return nil, models.ClientErrf("domain %s does not support auth (auth=%d)", rootDomain, wk.Auth)
	}

	// Update ZapLink cache for root domain
	if db != nil {
		if updateErr := db.UserDB.UpdateZapLinkHost(
			rootDomain, wk.ZapScript,
		); updateErr != nil {
			return nil, fmt.Errorf("failed to update zaplink host cache: %w", updateErr)
		}
	}

	// Redeem the claim token now that the domain is validated
	bearer, err := redeemClaimToken(ctx, rawClaimURL, token, pl.ID(), cfg.DeviceID())
	if err != nil {
		return nil, fmt.Errorf("failed to redeem claim token: %w", err)
	}

	// Persist the credential, tagged with the root that created it so
	// unlink can later remove the whole family by provenance.
	entry := config.CredentialEntry{Bearer: bearer, LinkedVia: rootDomain}
	storedDomains := []string{rootDomain}

	saveErr := cfg.SaveAuthEntry(rootDomain, entry)
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
		if confirmRelatedTrust(relatedDomain, rootDomain, db, fetchWK) {
			if saveErr := cfg.SaveAuthEntry(relatedDomain, entry); saveErr != nil {
				log.Warn().Err(saveErr).Str("related", related).
					Msg("failed to save auth entry for related domain")
				continue
			}
			storedDomains = append(storedDomains, relatedDomain)
		}
	}

	// A fresh credential for the backup API supersedes any recorded
	// revocation (a 401-triggered unlinked marker).
	backupLookup := config.BackupAuthLookupURL(cfg.BackupRemoteBaseURL())
	for _, domain := range storedDomains {
		if !strings.EqualFold(domain, backupLookup) {
			continue
		}
		backupManager := backupsvc.NewManager(cfg, pl, db)
		if backupCoordinator != nil {
			backupManager.WithCoordinator(backupCoordinator)
		}
		backupManager.MarkRemoteLinked()
		refreshCtx, cancelRefresh := context.WithTimeout(ctx, 5*time.Second)
		_, refreshErr := backupManager.RefreshRemoteAvailability(refreshCtx)
		cancelRefresh()
		if refreshErr != nil {
			log.Debug().Err(refreshErr).Msg("remote backup availability not refreshed after link")
		}
		break
	}

	return storedDomains, nil
}

// redeemClaimToken sends the claim token to the claim URL and returns the
// bearer token from the response. deviceHint is the persistent device ID
// from config: the server uses it to reuse the same device record when a
// device re-links, instead of creating a duplicate.
func redeemClaimToken(
	ctx context.Context,
	claimURL string,
	token string,
	platform string,
	deviceHint string,
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
	if deviceHint != "" {
		req.Header.Set(headerZaparooDeviceHint, deviceHint)
	}

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
