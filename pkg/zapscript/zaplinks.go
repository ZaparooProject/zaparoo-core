// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/installer"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/rs/zerolog/log"
)

const (
	MIMEZaparooZapScript  = "application/vnd.zaparoo.zapscript"
	WellKnownPath         = "/.well-known/zaparoo"
	HeaderZaparooOS       = "Zaparoo-OS"
	HeaderZaparooArch     = "Zaparoo-Arch"
	HeaderZaparooPlatform = "Zaparoo-Platform"

	preWarmMaxConcurrent = 5
	preWarmTimeout       = 2 * time.Second
)

var AcceptedMimeTypes = []string{
	MIMEZaparooZapScript,
}

// setZapLinkHeaders adds Zaparoo identification headers to an HTTP request.
func setZapLinkHeaders(req *http.Request, platform string) {
	req.Header.Set(HeaderZaparooOS, runtime.GOOS)
	req.Header.Set(HeaderZaparooArch, runtime.GOARCH)
	req.Header.Set(HeaderZaparooPlatform, platform)
}

type WellKnown struct {
	ZapScript int `json:"zapscript"`
}

// httpDoer is an interface for making HTTP requests for testing.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var zapFetchTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   1 * time.Second,
		KeepAlive: 10 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   1 * time.Second,
	ResponseHeaderTimeout: 1 * time.Second,
	ExpectContinueTimeout: 500 * time.Millisecond,
}

var zapFetchClient = &http.Client{
	Transport: &installer.AuthTransport{
		Base: zapFetchTransport,
	},
	Timeout: 2 * time.Second,
}

func queryZapLinkSupport(u *url.URL, platform string) (int, error) {
	baseURL := u.Scheme + "://" + u.Host
	wellKnownURL := baseURL + WellKnownPath
	log.Debug().Msgf("querying zap link support at %s", wellKnownURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("failed to create request for '%s': %w", wellKnownURL, err)
	}
	setZapLinkHeaders(req, platform)

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch '%s': %w", wellKnownURL, err)
	}
	if resp == nil {
		return 0, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("invalid status code")
	}

	var wellKnown WellKnown
	err = json.NewDecoder(resp.Body).Decode(&wellKnown)
	if err != nil {
		return 0, fmt.Errorf("failed to decode JSON from '%s': %w", wellKnownURL, err)
	}

	log.Debug().Msgf("zap link well known result for %s: %v", wellKnownURL, wellKnown)
	return wellKnown.ZapScript, nil
}

func isZapLink(link string, db *database.Database, platform string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}

	baseURL := u.Scheme + "://" + u.Host
	supported, ok, err := db.UserDB.GetZapLinkHost(baseURL)
	if err != nil {
		log.Error().Err(err).Msgf("error checking db for zap link support: %s", link)
		return false
	}
	if !ok {
		result, err := queryZapLinkSupport(u, platform)
		if isOfflineError(err) {
			// don't permanently log as not supported if it may be temp internet access
			return false
		}
		if err != nil {
			log.Debug().Err(err).Msgf("error querying zap link support: %s", link)
			if updateErr := db.UserDB.UpdateZapLinkHost(baseURL, result); updateErr != nil {
				log.Error().Err(updateErr).Msgf("error updating zap link support: %s", link)
			}
			return false
		}
		err = db.UserDB.UpdateZapLinkHost(baseURL, result)
		if err != nil {
			log.Error().Err(err).Msgf("error updating zap link support: %s", link)
		}
		supported = result > 0
	}

	if !supported {
		return false
	}

	return true
}

func getRemoteZapScript(urlStr, platform string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for '%s': %w", urlStr, err)
	}
	setZapLinkHeaders(req, platform)
	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch zapscript from '%s': %w", urlStr, err)
	}
	if resp == nil {
		return nil, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return nil, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return nil, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %w", err)
	}

	if content != MIMEZaparooZapScript {
		return nil, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	return body, nil
}

// isOfflineError returns true if the error is some network connectivity
// related error. Explicit error responses from a server will still return
// false.
func isOfflineError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var t *os.SyscallError
		switch {
		case errors.As(opErr.Err, &t):
			if errors.Is(t.Err, syscall.ECONNREFUSED) || errors.Is(t.Err, syscall.ENETUNREACH) ||
				errors.Is(t.Err, syscall.EHOSTUNREACH) || errors.Is(t.Err, syscall.ETIMEDOUT) {
				return true
			}
		default:
			if strings.Contains(opErr.Err.Error(), "connection refused") ||
				strings.Contains(opErr.Err.Error(), "no such host") ||
				strings.Contains(opErr.Err.Error(), "network is unreachable") ||
				strings.Contains(opErr.Err.Error(), "host is down") {
				return true
			}
		}
	}

	lowerErrStr := strings.ToLower(err.Error())
	if strings.Contains(lowerErrStr, "no such host") ||
		strings.Contains(lowerErrStr, "network is unreachable") ||
		strings.Contains(lowerErrStr, "connection refused") ||
		strings.Contains(lowerErrStr, "host is down") ||
		strings.Contains(lowerErrStr, "i/o timeout") ||
		strings.Contains(lowerErrStr, "tls handshake timeout") {
		return true
	}

	return false
}

func checkZapLink(
	_ *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	cmd parser.Command,
) (string, error) {
	if len(cmd.Args) == 0 {
		return "", errors.New("no args")
	}
	value := cmd.Args[0]
	platform := pl.ID()

	if !isZapLink(value, db, platform) {
		return "", nil
	}

	log.Info().Msgf("checking zap link: %s", value)
	body, err := getRemoteZapScript(value, platform)
	if isOfflineError(err) {
		zapscript, cacheErr := db.UserDB.GetZapLinkCache(value)
		if cacheErr != nil {
			return "", fmt.Errorf("failed to get zaplink cache for '%s': %w", value, cacheErr)
		}
		if zapscript != "" {
			return zapscript, nil
		}
	}
	if err != nil {
		return "", err
	}

	err = db.UserDB.UpdateZapLinkCache(value, string(body))
	if err != nil {
		log.Error().Err(err).Msgf("error updating zap link cache")
	}

	if !helpers.MaybeJSON(body) {
		return string(body), nil
	}
	return "", errors.New("zapscript JSON not supported")
}

// PreWarmZapLinkHosts pre-warms the DNS and TLS cache for known zaplink hosts.
// This is called during startup to reduce latency on first zaplink access.
// It makes HEAD requests to /.well-known/zaparoo for each supported base URL.
func PreWarmZapLinkHosts(db *database.Database, platform string, checkInternet func(int) bool) {
	if !checkInternet(3) {
		log.Debug().Msg("no internet connectivity, skipping zaplink pre-warming")
		return
	}

	baseURLs, err := db.UserDB.GetSupportedZapLinkHosts()
	if err != nil {
		log.Error().Err(err).Msg("error getting supported zaplink hosts for pre-warming")
		return
	}

	if len(baseURLs) == 0 {
		log.Debug().Msg("no supported zaplink hosts to pre-warm")
		return
	}

	log.Info().Msgf("pre-warming %d zaplink hosts", len(baseURLs))

	sem := make(chan struct{}, preWarmMaxConcurrent)
	var wg sync.WaitGroup

	for _, baseURL := range baseURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			preWarmHost(u, db, platform, zapFetchClient)
		}(baseURL)
	}

	wg.Wait()
	log.Info().Msg("zaplink pre-warming complete")
}

// preWarmHost makes a HEAD request to a base URL's well-known endpoint to warm caches.
func preWarmHost(baseURL string, db *database.Database, platform string, client httpDoer) {
	wellKnownURL := baseURL + WellKnownPath

	ctx, cancel := context.WithTimeout(context.Background(), preWarmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, wellKnownURL, http.NoBody)
	if err != nil {
		log.Debug().Err(err).Msgf("failed to create pre-warm request for %s", baseURL)
		return
	}
	setZapLinkHeaders(req, platform)

	resp, err := client.Do(req)
	if err != nil {
		log.Debug().Err(err).Msgf("pre-warm request failed for %s", baseURL)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("error closing pre-warm response body")
		}
	}()

	if resp.StatusCode == http.StatusOK {
		u, parseErr := url.Parse(wellKnownURL)
		if parseErr != nil {
			return
		}
		result, queryErr := queryZapLinkSupport(u, platform)
		if queryErr == nil && result > 0 {
			if updateErr := db.UserDB.UpdateZapLinkHost(baseURL, result); updateErr != nil {
				log.Debug().Err(updateErr).Msgf("failed to update zaplink host timestamp for %s", baseURL)
			}
		}
		log.Debug().Msgf("pre-warmed zaplink host: %s", baseURL)
	}
}
