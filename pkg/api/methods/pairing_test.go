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

package methods

import (
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPairingController struct {
	expiresAt time.Time
	err       error
	pin       string
	cancelled bool
}

func (m *mockPairingController) StartPairing() (string, time.Time, error) {
	return m.pin, m.expiresAt, m.err
}

func (m *mockPairingController) CancelPairing() {
	m.cancelled = true
}

func TestHandleClientsPairStart_Success(t *testing.T) {
	t.Parallel()
	expires := time.Now().Add(5 * time.Minute)
	mgr := &mockPairingController{pin: "123456", expiresAt: expires}
	handler := HandleClientsPairStart(mgr)

	result, err := handler(requests.RequestEnv{IsLocal: true})
	require.NoError(t, err)

	resp, ok := result.(models.ClientsPairStartResponse)
	require.True(t, ok)
	assert.Equal(t, "123456", resp.PIN)
	assert.Equal(t, expires.Unix(), resp.ExpiresAt)
}

func TestHandleClientsPairStart_RemoteRejected(t *testing.T) {
	t.Parallel()
	mgr := &mockPairingController{pin: "123456", expiresAt: time.Now()}
	handler := HandleClientsPairStart(mgr)

	_, err := handler(requests.RequestEnv{IsLocal: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "localhost")
}

func TestHandleClientsPairStart_Error(t *testing.T) {
	t.Parallel()
	mgr := &mockPairingController{err: errors.New("pairing in progress")}
	handler := HandleClientsPairStart(mgr)

	_, err := handler(requests.RequestEnv{IsLocal: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pairing in progress")
}

func TestHandleClientsPairCancel_Success(t *testing.T) {
	t.Parallel()
	mgr := &mockPairingController{}
	handler := HandleClientsPairCancel(mgr)

	result, err := handler(requests.RequestEnv{IsLocal: true})
	require.NoError(t, err)
	assert.IsType(t, NoContent{}, result)
	assert.True(t, mgr.cancelled)
}

func TestHandleClientsPairCancel_RemoteRejected(t *testing.T) {
	t.Parallel()
	mgr := &mockPairingController{}
	handler := HandleClientsPairCancel(mgr)

	_, err := handler(requests.RequestEnv{IsLocal: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "localhost")
	assert.False(t, mgr.cancelled, "CancelPairing must not be called for remote requests")
}
