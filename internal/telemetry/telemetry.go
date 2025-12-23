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

// Package telemetry provides opt-in error reporting via Sentry.
// All PII is stripped before transmission.
package telemetry

import (
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	flushTimeout = 2 * time.Second
	// sentryDSN contains the public key needed for Sentry to authenticate the envelope.
	sentryDSN = "https://abc4626558a1ae75a72c45f28b8d8144@o4510577054842880.ingest.de.sentry.io/4510577058381904"
	// tunnelHost is where all error reports are sent.
	tunnelHost = "errors.zaparoo.org"
)

var (
	enabled      bool
	sentryWriter *sentryzerolog.Writer
	closeOnce    sync.Once

	// Patterns to strip usernames from file paths
	homePathRe    = regexp.MustCompile(`(?i)/home/[^/]+/`)
	usersPathRe   = regexp.MustCompile(`(?i)/Users/[^/]+/`)
	windowsUserRe = regexp.MustCompile(`(?i)[a-zA-Z]:\\Users\\[^\\]+\\`)
)

// tunnelTransport rewrites Sentry API requests to go through the tunnel.
type tunnelTransport struct {
	inner http.RoundTripper
}

func (t *tunnelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to use the tunnel
	req.URL.Scheme = "https"
	req.URL.Host = tunnelHost
	req.URL.Path = "/"
	req.Host = tunnelHost

	//nolint:wrapcheck // RoundTripper interface requires unwrapped error
	return t.inner.RoundTrip(req)
}

// Init initializes Sentry error reporting with zerolog integration.
// If reportingEnabled is false, telemetry remains disabled.
func Init(reportingEnabled bool, deviceID, appVersion, platformID string) error {
	if !reportingEnabled {
		log.Debug().Msg("error reporting disabled")
		return nil
	}

	// Create HTTP client that routes through our tunnel
	httpClient := &http.Client{
		Transport: &tunnelTransport{inner: http.DefaultTransport},
		Timeout:   30 * time.Second,
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              sentryDSN,
		Release:          "zaparoo-core@" + appVersion,
		Environment:      platformID,
		AttachStacktrace: true,
		// Privacy: explicitly disable PII collection
		SendDefaultPII: false,
		ServerName:     "",
		MaxBreadcrumbs: 0,
		HTTPClient:     httpClient,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return sanitizeEvent(event)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize sentry: %w", err)
	}

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{ID: deviceID})
		scope.SetTag("platform", platformID)
		scope.SetTag("os", runtime.GOOS)
		scope.SetTag("arch", runtime.GOARCH)
	})

	// Use existing hub from sentry.Init above
	sentryWriter, err = sentryzerolog.NewWithHub(sentry.CurrentHub(), sentryzerolog.Options{
		Levels:          []zerolog.Level{zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel},
		FlushTimeout:    flushTimeout,
		WithBreadcrumbs: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create sentry zerolog writer: %w", err)
	}

	// Add Sentry writer alongside the existing log writer
	log.Logger = log.Output(zerolog.MultiLevelWriter(
		helpers.LogWriter(),
		sentryWriter,
	)).With().Caller().Logger()

	enabled = true
	log.Info().Msg("error reporting enabled")
	return nil
}

// Close flushes pending events and shuts down Sentry.
// Safe to call multiple times.
func Close() {
	if !enabled {
		return
	}
	closeOnce.Do(func() {
		_ = sentryWriter.Close()
		sentry.Flush(flushTimeout)
	})
}

// Flush ensures all pending events are sent to Sentry.
// Call this before os.Exit to ensure error events are transmitted.
func Flush() {
	if !enabled {
		return
	}
	sentry.Flush(flushTimeout)
}

// Enabled returns whether telemetry is enabled.
func Enabled() bool {
	return enabled
}

// sanitizeEvent removes PII from Sentry events before sending.
func sanitizeEvent(event *sentry.Event) *sentry.Event {
	// Clear server name (hostname) - SDK may populate despite ServerName: ""
	event.ServerName = ""

	for i := range event.Exception {
		if event.Exception[i].Stacktrace != nil {
			for j := range event.Exception[i].Stacktrace.Frames {
				frame := &event.Exception[i].Stacktrace.Frames[j]
				frame.AbsPath = sanitizePath(frame.AbsPath)
				frame.Filename = sanitizePath(frame.Filename)
			}
		}
	}

	event.Message = sanitizePath(event.Message)

	for k, v := range event.Extra {
		if s, ok := v.(string); ok {
			event.Extra[k] = sanitizePath(s)
		}
	}

	return event
}

// sanitizePath removes usernames from file paths.
func sanitizePath(path string) string {
	if path == "" {
		return path
	}

	result := homePathRe.ReplaceAllString(path, "/home/<user>/")
	result = usersPathRe.ReplaceAllString(result, "/Users/<user>/")
	result = windowsUserRe.ReplaceAllString(result, "C:\\Users\\<user>\\")

	return result
}
