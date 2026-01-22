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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var (
	ErrRequestTimeout   = errors.New("request timed out")
	ErrInvalidParams    = errors.New("invalid params")
	ErrRequestCancelled = errors.New("request cancelled")
)

const APIPath = "/api/v0.1"

// DisableZapScript disables the service running any processed ZapScript from
// tokens, and returns a function to re-enable it.
// The returned function must be run even if there is an error so the service
// isn't left in an unusable state.
func DisableZapScript(cfg *config.Instance) func() {
	_, err := LocalClient(
		context.Background(),
		cfg,
		models.MethodSettingsUpdate,
		"{\"runZapScript\":false}",
	)
	if err != nil {
		log.Error().Err(err).Msg("error disabling runZapScript")
		return func() {}
	}

	return func() {
		_, err = LocalClient(
			context.Background(),
			cfg,
			models.MethodSettingsUpdate,
			"{\"runZapScript\":true}",
		)
		if err != nil {
			log.Error().Err(err).Msg("error enabling runZapScript")
		}
	}
}

// LocalClient sends a single unauthenticated method with params to the local
// running API service, waits for a response until timeout then disconnects.
func LocalClient(
	ctx context.Context,
	cfg *config.Instance,
	method string,
	params string,
) (string, error) {
	localWebsocketURL := url.URL{
		Scheme: "ws",
		Host:   "127.0.0.1:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	id := models.NewStringID(uuid.New().String())

	req := models.RequestObject{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	switch {
	case params == "":
		req.Params = nil
	case json.Valid([]byte(params)):
		req.Params = []byte(params)
	default:
		return "", ErrInvalidParams
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: config.APIRequestTimeout,
		NetDialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{
				Timeout:   config.APIRequestTimeout,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(dialCtx, network, addr)
		},
	}
	//nolint:bodyclose // gorilla/websocket replaces resp.Body with NopCloser before returning
	c, _, err := dialer.DialContext(ctx, localWebsocketURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to dial websocket: %w", err)
	}
	defer func(c *websocket.Conn) {
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
	}(c)

	done := make(chan struct{})
	var resp *models.ResponseObject

	go func() {
		defer close(done)
		for {
			_, message, readErr := c.ReadMessage()
			if readErr != nil {
				if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
					log.Debug().Err(readErr).Msg("connection closed")
				} else {
					log.Error().Err(readErr).Msg("error reading message")
				}
				return
			}

			var m models.ResponseObject
			unmarshalErr := json.Unmarshal(message, &m)
			if unmarshalErr != nil {
				continue
			}

			if m.JSONRPC != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if !m.ID.Equal(id) {
				continue
			}

			resp = &m
			return
		}
	}()

	err = c.WriteJSON(req)
	if err != nil {
		return "", fmt.Errorf("failed to write json to websocket: %w", err)
	}

	timeout := config.APIRequestTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	timer := time.NewTimer(timeout)
	select {
	case <-done:

	case <-timer.C:
		return "", ErrRequestTimeout
	case <-ctx.Done():
		return "", ErrRequestCancelled
	}

	if resp == nil {
		return "", ErrRequestTimeout
	}

	if resp.Error != nil {
		return "", errors.New(resp.Error.Message)
	}

	var b []byte
	b, err = json.Marshal(resp.Result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response result: %w", err)
	}

	return string(b), nil
}

func WaitNotification(
	ctx context.Context,
	timeout time.Duration,
	cfg *config.Instance,
	id string,
) (string, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "127.0.0.1:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	dialTimeout := timeout
	if dialTimeout <= 0 {
		dialTimeout = config.APIRequestTimeout
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: dialTimeout,
		NetDialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(dialCtx, network, addr)
		},
	}
	//nolint:bodyclose // gorilla/websocket replaces resp.Body with NopCloser before returning
	c, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to dial websocket: %w", err)
	}
	defer func(c *websocket.Conn) {
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
	}(c)

	done := make(chan struct{})
	var resp *models.RequestObject

	go func() {
		defer close(done)
		for {
			_, message, readErr := c.ReadMessage()
			if readErr != nil {
				if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
					log.Debug().Err(readErr).Msg("connection closed")
				} else {
					log.Error().Err(readErr).Msg("error reading message")
				}
				return
			}

			var m models.RequestObject
			unmarshalErr := json.Unmarshal(message, &m)
			if unmarshalErr != nil {
				continue
			}

			if m.JSONRPC != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if !m.ID.IsAbsent() {
				continue
			}

			if m.Method != id {
				continue
			}

			resp = &m

			return
		}
	}()

	var timerChan <-chan time.Time
	if timeout == 0 {
		effectiveTimeout := config.APIRequestTimeout
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < effectiveTimeout {
				effectiveTimeout = remaining
			}
		}
		timer := time.NewTimer(effectiveTimeout)
		timerChan = timer.C
	} else if timeout > 0 {
		timer := time.NewTimer(timeout)
		timerChan = timer.C
	}
	// or else leave chan nil, which will never receive

	select {
	case <-done:

	case <-timerChan:
		return "", ErrRequestTimeout
	case <-ctx.Done():
		return "", ErrRequestCancelled
	}

	if resp == nil {
		return "", ErrRequestTimeout
	}

	var b []byte
	b, err = json.Marshal(resp.Params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response params: %w", err)
	}

	return string(b), nil
}

// IsServiceRunning checks if a Zaparoo service is running on the configured port.
func IsServiceRunning(cfg *config.Instance) bool {
	_, err := LocalClient(context.Background(), cfg, models.MethodVersion, "")
	if err != nil {
		log.Debug().
			Err(err).
			Int("port", cfg.APIPort()).
			Msg("service not detected on API port")
		return false
	}
	log.Debug().
		Int("port", cfg.APIPort()).
		Msg("detected running service instance")
	return true
}

// WaitForAPI waits for the service API to become available.
// Returns true if API became available, false if timeout reached.
func WaitForAPI(cfg *config.Instance, maxWaitTime, checkInterval time.Duration) bool {
	deadline := time.Now().Add(maxWaitTime)

	for time.Now().Before(deadline) {
		if IsServiceRunning(cfg) {
			return true
		}
		time.Sleep(checkInterval)
	}

	return false
}

// WaitNotifications waits for any of the specified notification types on a single
// WebSocket connection. Returns the notification method that matched and its params.
func WaitNotifications(
	ctx context.Context,
	timeout time.Duration,
	cfg *config.Instance,
	ids ...string,
) (method, params string, err error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	dialTimeout := timeout
	if dialTimeout <= 0 {
		dialTimeout = config.APIRequestTimeout
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: dialTimeout,
		NetDialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(dialCtx, network, addr)
		},
	}
	//nolint:bodyclose // gorilla/websocket replaces resp.Body with NopCloser before returning
	c, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to dial websocket: %w", err)
	}
	defer func(c *websocket.Conn) {
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
	}(c)

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	done := make(chan struct{})
	var resp *models.RequestObject

	go func() {
		defer close(done)
		for {
			_, message, readErr := c.ReadMessage()
			if readErr != nil {
				log.Error().Err(readErr).Msg("error reading message")
				return
			}

			var m models.RequestObject
			unmarshalErr := json.Unmarshal(message, &m)
			if unmarshalErr != nil {
				continue
			}

			if m.JSONRPC != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if !m.ID.IsAbsent() {
				continue
			}

			if _, ok := idSet[m.Method]; !ok {
				continue
			}

			resp = &m
			return
		}
	}()

	var timerChan <-chan time.Time
	if timeout == 0 {
		effectiveTimeout := config.APIRequestTimeout
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < effectiveTimeout {
				effectiveTimeout = remaining
			}
		}
		timer := time.NewTimer(effectiveTimeout)
		timerChan = timer.C
	} else if timeout > 0 {
		timer := time.NewTimer(timeout)
		timerChan = timer.C
	}

	select {
	case <-done:

	case <-timerChan:
		return "", "", ErrRequestTimeout
	case <-ctx.Done():
		return "", "", ErrRequestCancelled
	}

	if resp == nil {
		return "", "", ErrRequestTimeout
	}

	var b []byte
	b, err = json.Marshal(resp.Params)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal response params: %w", err)
	}

	return resp.Method, string(b), nil
}
