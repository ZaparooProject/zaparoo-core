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

package remote

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
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

//nolint:govet // Fields grouped by response semantics.
type httpError struct {
	status     int
	code       string
	retryAfter time.Duration
}

type errorBody struct {
	Code string `json:"code"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func (e *httpError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("remote operations HTTP %d (%s)", e.status, e.code)
	}
	return fmt.Sprintf("remote operations HTTP %d", e.status)
}

func (m *manager) doJSON(
	ctx context.Context, method, requestPath string, body, out any,
) error {
	bearer := m.deviceBearer()
	if bearer == "" {
		return errUnauthorized
	}
	endpoint, err := buildEndpoint(m.deps.Config.RemoteControlBaseURL(), requestPath)
	if err != nil {
		return err
	}
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("encode remote operation request: %w", marshalErr)
		}
		reader = bytes.NewReader(encoded)
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("build remote operation request: %w", err)
	}
	m.setRequestHeaders(req, bearer, body != nil)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send remote operation request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close remote operation response")
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	if err := decodeBoundedJSON(resp.Body, 64<<10, out); err != nil {
		return fmt.Errorf("decode remote operation response: %w", err)
	}
	return nil
}

func (m *manager) setRequestHeaders(req *http.Request, bearer string, hasBody bool) {
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(zapscript.HeaderZaparooOS, runtime.GOOS)
	req.Header.Set(zapscript.HeaderZaparooArch, runtime.GOARCH)
	req.Header.Set(zapscript.HeaderZaparooPlatform, m.deps.Platform.ID())
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

func decodeHTTPError(resp *http.Response) error {
	var payload errorResponse
	_ = decodeBoundedJSON(resp.Body, 4<<10, &payload)
	return &httpError{
		status:     resp.StatusCode,
		code:       payload.Error.Code,
		retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func decodeBoundedJSON(reader io.Reader, limit int64, out any) error {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read bounded JSON: %w", err)
	}
	if int64(len(data)) > limit {
		return errors.New("response exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode bounded JSON: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("response contains trailing JSON")
	}
	return nil
}

func buildEndpoint(baseURL, requestPath string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid remote operations base URL: %w", err)
	}
	request, err := url.Parse(requestPath)
	if err != nil {
		return "", fmt.Errorf("invalid remote operations request path: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + request.Path
	base.RawQuery = request.RawQuery
	return base.String(), nil
}
