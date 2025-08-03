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

package installer

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog/log"
)

type AuthTransport struct {
	Base http.RoundTripper
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Base == nil {
		t.Base = http.DefaultTransport
	}

	creds := config.LookupAuth(config.GetAuthCfg(), req.URL.String())
	if creds != nil {
		if creds.Bearer != "" {
			req.Header.Set("Authorization", "Bearer "+creds.Bearer)
		} else if creds.Username != "" {
			user := creds.Username
			pass := creds.Password
			auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			req.Header.Set("Authorization", "Basic "+auth)
		}
	}

	return t.Base.RoundTrip(req)
}

var timeoutTr = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ResponseHeaderTimeout: 30 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
}

var httpClient = &http.Client{
	Transport: &AuthTransport{
		Base: timeoutTr,
	},
}

func DownloadHTTPFile(opts DownloaderArgs) error {
	// TODO: Add progress feedback for large file downloads
	// Extended timeout for potentially large game files (700MB+)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error getting url: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing response body")
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	file, err := os.Create(opts.tempPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msgf("error closing file: %s", opts.tempPath)
		}
		removeErr := os.Remove(opts.tempPath)
		if removeErr != nil {
			log.Warn().Err(removeErr).Msgf("error removing partial download: %s", opts.tempPath)
		}
		return fmt.Errorf("error downloading file: %w", err)
	}

	expected := resp.ContentLength
	if expected > 0 && written != expected {
		closeErr := file.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msgf("error closing file: %s", opts.tempPath)
		}
		removeErr := os.Remove(opts.tempPath)
		if removeErr != nil {
			log.Warn().Err(removeErr).Msgf("error removing partial download: %s", opts.tempPath)
		}
		return fmt.Errorf("download incomplete: expected %d bytes, got %d", expected, written)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	if err := os.Rename(opts.tempPath, opts.finalPath); err != nil {
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing temp file: %s", opts.tempPath)
		}
		return fmt.Errorf("error renaming temp file: %w", err)
	}

	return nil
}
