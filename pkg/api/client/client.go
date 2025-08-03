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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
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
		Host:   "localhost:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	id, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}

	req := models.RequestObject{
		JSONRPC: "2.0",
		ID:      &id,
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

	c, _, err := websocket.DefaultDialer.Dial(localWebsocketURL.String(), nil)
	if err != nil {
		return "", err
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
				log.Error().Err(readErr).Msg("error reading message")
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

			if m.ID != id {
				continue
			}

			resp = &m
			return
		}
	}()

	err = c.WriteJSON(req)
	if err != nil {
		return "", err
	}

	timer := time.NewTimer(config.APIRequestTimeout)
	select {
	case <-done:

	case <-timer.C:
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
		return "", ErrRequestTimeout
	case <-ctx.Done():
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
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
		return "", err
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
		Host:   "localhost:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return "", err
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

			if m.ID != nil {
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
		timer := time.NewTimer(config.APIRequestTimeout)
		timerChan = timer.C
	} else if timeout > 0 {
		timer := time.NewTimer(timeout)
		timerChan = timer.C
	}
	// or else leave chan nil, which will never receive

	select {
	case <-done:

	case <-timerChan:
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
		return "", ErrRequestTimeout
	case <-ctx.Done():
		closeErr := c.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing websocket")
		}
		return "", ErrRequestCancelled
	}

	if resp == nil {
		return "", ErrRequestTimeout
	}

	var b []byte
	b, err = json.Marshal(resp.Params)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
