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

package platforms

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/rs/zerolog/log"
)

// PowerStatusProvider is optionally implemented by platforms whose power state
// cannot be read the ordinary way. Handheld hardware with an out-of-tree
// battery driver is the case this exists for; platforms that leave it
// unimplemented are read through the kernel or OS power API instead.
type PowerStatusProvider interface {
	PowerStatus() (power.Status, error)
}

// PowerStatus reports where pl is drawing power from, preferring the
// platform's own reading when it has one.
//
// A reading that fails is reported as unknown rather than as an error the
// caller has to interpret: whether the battery is unreadable or the call
// itself broke, what the caller can do about it is the same.
func PowerStatus(pl Platform) power.Status {
	read := power.Read
	if provider, ok := pl.(PowerStatusProvider); ok {
		read = provider.PowerStatus
	}

	status, err := read()
	if err != nil {
		log.Debug().Err(err).Msg("could not read device power status")
		return power.Status{Source: power.SourceUnknown}
	}
	if status.Source == "" {
		return power.Status{Source: power.SourceUnknown}
	}
	return status
}
