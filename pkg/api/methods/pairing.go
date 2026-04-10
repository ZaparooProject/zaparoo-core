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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

// PairingController is the subset of PairingManager needed by the RPC handlers.
type PairingController interface {
	StartPairing() (pin string, expiresAt time.Time, err error)
	CancelPairing()
}

// HandleClientsPairStart returns a handler that initiates a new pairing flow.
// Localhost-only — the user must have physical access to the device.
func HandleClientsPairStart(mgr PairingController) func(requests.RequestEnv) (any, error) {
	return func(env requests.RequestEnv) (any, error) {
		if !env.IsLocal {
			return nil, models.ClientErrf("%w", ErrLocalhostOnly)
		}

		log.Info().Msg("received clients.pair.start request")

		pin, expiresAt, err := mgr.StartPairing()
		if err != nil {
			return nil, models.ClientErrf("failed to start pairing: %w", err)
		}

		return models.ClientsPairStartResponse{
			PIN:       pin,
			ExpiresAt: expiresAt.Unix(),
		}, nil
	}
}

// HandleClientsPairCancel returns a handler that cancels the active pairing flow.
// Localhost-only.
func HandleClientsPairCancel(mgr PairingController) func(requests.RequestEnv) (any, error) {
	return func(env requests.RequestEnv) (any, error) {
		if !env.IsLocal {
			return nil, models.ClientErrf("%w", ErrLocalhostOnly)
		}

		log.Info().Msg("received clients.pair.cancel request")
		mgr.CancelPairing()
		return NoContent{}, nil
	}
}
