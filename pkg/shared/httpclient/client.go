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

package httpclient

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultTimeoutSeconds is the default timeout for HTTP requests
	DefaultTimeoutSeconds = 30
)

// AuthTransport provides automatic authentication for HTTP requests based on auth.toml
type AuthTransport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper interface with automatic authentication
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

	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform HTTP round trip: %w", err)
	}
	return resp, nil
}

// DefaultTransport provides a configured transport with connection pooling and reasonable timeouts
var DefaultTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ResponseHeaderTimeout: 30 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	// Connection pooling settings
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
}

// Client provides an HTTP client with authentication and sensible defaults
type Client struct {
	*http.Client
}

// NewClient creates a new HTTP client with authentication support
func NewClient() *Client {
	return &Client{
		Client: &http.Client{
			Transport: &AuthTransport{
				Base: DefaultTransport,
			},
		},
	}
}

// NewClientWithTimeout creates a new HTTP client with a custom timeout
func NewClientWithTimeout(timeout time.Duration) *Client {
	return &Client{
		Client: &http.Client{
			Transport: &AuthTransport{
				Base: DefaultTransport,
			},
			Timeout: timeout,
		},
	}
}

// NewClientFromConfig creates a new HTTP client using default timeout
func NewClientFromConfig(cfg *config.Instance) *Client {
	timeout := DefaultTimeoutSeconds * time.Second
	return NewClientWithTimeout(timeout)
}

// DownloadFileArgs contains arguments for file download operations
type DownloadFileArgs struct {
	URL        string
	OutputPath string
	TempPath   string
}

// DownloadFile downloads a file from the given URL to the output path
func (c *Client) DownloadFile(ctx context.Context, args DownloadFileArgs) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, http.NoBody)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("error getting url: %w", err)
	}
	if resp == nil {
		return errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}

	// Use temp path if provided, otherwise use output path directly
	outputPath := args.OutputPath
	if args.TempPath != "" {
		outputPath = args.TempPath
	}

	file, err := os.Create(outputPath) // #nosec G304 - outputPath is validated by caller
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msgf("error closing file: %s", outputPath)
		}
		removeErr := os.Remove(outputPath)
		if removeErr != nil {
			log.Warn().Err(removeErr).Msgf("error removing partial download: %s", outputPath)
		}
		return fmt.Errorf("error downloading file: %w", err)
	}

	expected := resp.ContentLength
	if expected > 0 && written != expected {
		closeErr := file.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msgf("error closing file: %s", outputPath)
		}
		removeErr := os.Remove(outputPath)
		if removeErr != nil {
			log.Warn().Err(removeErr).Msgf("error removing partial download: %s", outputPath)
		}
		return fmt.Errorf("download incomplete: expected %d bytes, got %d", expected, written)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	// Move from temp path to final path if using temp file
	if args.TempPath != "" && args.TempPath != args.OutputPath {
		if err := os.Rename(args.TempPath, args.OutputPath); err != nil {
			removeErr := os.Remove(args.TempPath)
			if removeErr != nil {
				log.Warn().Err(removeErr).Msgf("error removing temp file: %s", args.TempPath)
			}
			return fmt.Errorf("error renaming temp file: %w", err)
		}
	}

	return nil
}

// Get performs a GET request and returns the response
func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing GET request: %w", err)
	}

	return resp, nil
}

// Post performs a POST request with the given body and returns the response
func (c *Client) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing POST request: %w", err)
	}

	return resp, nil
}

// DefaultClient provides a shared HTTP client instance
var DefaultClient = NewClient()
