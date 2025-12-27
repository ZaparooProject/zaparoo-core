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
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

// APIClient abstracts API communication for testability.
type APIClient interface {
	// Call executes a JSON-RPC method and returns the result.
	Call(ctx context.Context, method, params string) (string, error)

	// WaitNotification blocks until a notification of the given type is received.
	WaitNotification(ctx context.Context, timeout time.Duration, notificationType string) (string, error)
}

// LocalAPIClient implements APIClient using the real local WebSocket client.
type LocalAPIClient struct {
	cfg *config.Instance
}

// NewLocalAPIClient creates an APIClient that communicates with the local API.
func NewLocalAPIClient(cfg *config.Instance) *LocalAPIClient {
	return &LocalAPIClient{cfg: cfg}
}

// Call executes a JSON-RPC method via the local WebSocket client.
func (c *LocalAPIClient) Call(ctx context.Context, method, params string) (string, error) {
	resp, err := LocalClient(ctx, c.cfg, method, params)
	if err != nil {
		return "", fmt.Errorf("api call failed: %w", err)
	}
	return resp, nil
}

// WaitNotification waits for a notification via the local WebSocket client.
func (c *LocalAPIClient) WaitNotification(
	ctx context.Context,
	timeout time.Duration,
	notificationType string,
) (string, error) {
	resp, err := WaitNotification(ctx, timeout, c.cfg, notificationType)
	if err != nil {
		return "", fmt.Errorf("wait notification failed: %w", err)
	}
	return resp, nil
}
