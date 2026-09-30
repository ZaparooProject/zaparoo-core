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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	jsonRPCMethodNotFound  = -32601
	zapScriptHoldRedialGap = time.Second
)

var errZapScriptHoldUnsupported = errors.New("service does not support holding ZapScript disabled")

// zapScriptHold is an open connection that keeps ZapScript disabled for as long
// as it stays open.
type zapScriptHold struct {
	// lost is closed when the connection ends for any reason.
	lost  <-chan struct{}
	close func()
}

type zapScriptHoldFunc func(context.Context, *config.Instance) (*zapScriptHold, error)

// DisableZapScript disables the service running any processed ZapScript from
// tokens, and returns a function to re-enable it.
//
// The service holds ZapScript disabled for as long as this process's connection
// is open, so a process that is killed rather than exited cannot leave the
// service unusable. The returned function must still be run on a normal exit,
// even if there was an error, so ZapScript is re-enabled promptly.
func DisableZapScript(cfg *config.Instance) func() {
	return disableZapScriptWithHold(cfg, openZapScriptHold, LocalClient, zapScriptHoldRedialGap)
}

func disableZapScriptWithHold(
	cfg *config.Instance,
	open zapScriptHoldFunc,
	request func(context.Context, *config.Instance, string, string) (string, error),
	redialGap time.Duration,
) func() {
	first, err := open(context.Background(), cfg)
	if errors.Is(err, errZapScriptHoldUnsupported) {
		// A service that predates the hold only understands the setting.
		return disableZapScriptWithRequest(cfg, request)
	}
	if err != nil {
		logZapScriptToggleError(err, "error disabling runZapScript")
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		maintainZapScriptHold(ctx, cfg, open, first, redialGap)
	}()

	return func() {
		cancel()
		<-done
	}
}

// maintainZapScriptHold keeps a hold open until ctx ends, reconnecting if the
// service restarts or the connection drops while the caller is still running.
func maintainZapScriptHold(
	ctx context.Context,
	cfg *config.Instance,
	open zapScriptHoldFunc,
	current *zapScriptHold,
	redialGap time.Duration,
) {
	for {
		select {
		case <-ctx.Done():
			current.close()
			return
		case <-current.lost:
		}
		current.close()

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(redialGap):
			}
			next, err := open(ctx, cfg)
			if err != nil {
				log.Debug().Err(err).Msg("retrying ZapScript hold")
				continue
			}
			current = next
			break
		}
	}
}

// openZapScriptHold connects to the local service and asks it to hold ZapScript
// disabled until the connection closes.
func openZapScriptHold(ctx context.Context, cfg *config.Instance) (*zapScriptHold, error) {
	c, err := dialLocalWebsocket(ctx, cfg)
	if err != nil {
		return nil, err
	}

	id := models.NewStringID(uuid.New().String())
	err = c.WriteJSON(models.RequestObject{
		JSONRPC: "2.0",
		ID:      id,
		Method:  models.MethodSettingsZapScriptHold,
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("failed to write json to websocket: %w", err)
	}

	lost := make(chan struct{})
	answered := make(chan error, 1)
	go func() {
		defer close(lost)
		reported := false
		for {
			_, message, readErr := c.ReadMessage()
			if readErr != nil {
				if !reported {
					answered <- readErr
				}
				return
			}
			if reported {
				continue
			}
			var m models.ResponseObject
			if json.Unmarshal(message, &m) != nil || m.JSONRPC != "2.0" || !m.ID.Equal(id) {
				continue
			}
			reported = true
			switch {
			case m.Error == nil:
				answered <- nil
			case m.Error.Code == jsonRPCMethodNotFound:
				answered <- errZapScriptHoldUnsupported
			default:
				answered <- errors.New(m.Error.Message)
			}
		}
	}()

	timer := time.NewTimer(config.APIRequestTimeout)
	defer timer.Stop()
	select {
	case err = <-answered:
	case <-timer.C:
		err = ErrRequestTimeout
	case <-ctx.Done():
		err = ErrRequestCancelled
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	return &zapScriptHold{
		lost: lost,
		close: func() {
			closeErr := c.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			)
			if closeErr != nil {
				log.Debug().Err(closeErr).Msg("error sending websocket close")
			}
			if closeErr = c.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Msg("error closing websocket")
			}
		},
	}, nil
}
