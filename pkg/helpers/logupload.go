/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package helpers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// Uploading a log needs no service, no database and no API — only a log
// directory and a data directory. That is what makes it the thing a user can
// still do when Core has stopped, so it lives here rather than inside any one
// interface.
var (
	ErrUploadReadLog  = errors.New("log unavailable for upload")
	ErrUploadPrepare  = errors.New("failed to prepare upload")
	ErrUploadConnect  = errors.New("failed to connect to upload service")
	ErrUploadResponse = errors.New("failed to read upload response")
	ErrUploadStatus   = errors.New("upload service returned error status")
)

// uploadTimeout bounds a single upload attempt. A log bundle is under a
// megabyte, so this is generous even on a slow connection.
const uploadTimeout = 30 * time.Second

// maxResponseBytes bounds the reply. The service answers with a single URL, so
// anything approaching this is a service that is not the one we think it is —
// and the reply is read on a device with 128MB of RAM and then shown to the
// user, so it is not read unbounded.
const maxResponseBytes = 8 << 10

// UploadLog sends the log bundle to the configured paste service and returns
// the URL it was published at.
func UploadLog(pl platforms.Platform) (string, error) {
	return uploadLogTo(pl, config.LogUploadURL, &http.Client{
		Timeout:   uploadTimeout,
		Transport: useragent.Transport(nil),
	})
}

// uploadLogTo is UploadLog with the destination injected, so the read-and-post
// pairing can be tested without reaching the real paste service.
func uploadLogTo(pl platforms.Platform, uploadURL string, client *http.Client) (string, error) {
	// The bundle, not just core.log: a crash exists only in the captured
	// stderr, and that is usually the part worth reading.
	content, err := ReadLogBundle(pl, config.LogBundleMaxBytes)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadReadLog, err)
	}

	return UploadLogContent(content, uploadURL, client)
}

// UploadLogContent uploads log content to the given URL and returns the URL it
// was published at. The paste service answers with that URL as its whole body.
func UploadLogContent(content []byte, uploadURL string, client *http.Client) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "core.log")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadPrepare, err)
	}
	if _, err = part.Write(content); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadPrepare, err)
	}
	if err = writer.Close(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadPrepare, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadPrepare, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req) //nolint:gosec // G704: URL from hardcoded paste service endpoint
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadConnect, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close response body")
		}
	}()

	response, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadResponse, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %d %s", ErrUploadStatus, resp.StatusCode, string(response))
	}

	return strings.TrimSpace(string(response)), nil
}

// DescribeUploadFailure turns an upload error into a sentence for a user. The
// read and connection cases are separated because they are the two with an
// obvious cause and an obvious remedy, and because the first one means nothing
// was sent at all.
func DescribeUploadFailure(err error) string {
	switch {
	case errors.Is(err, ErrUploadReadLog):
		return "Unable to read log file."
	case errors.Is(err, ErrUploadConnect):
		return "Unable to connect to upload service."
	default:
		return "Unable to upload log file."
	}
}
