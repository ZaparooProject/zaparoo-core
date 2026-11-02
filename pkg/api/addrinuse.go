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

package api

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// IsAddrInUse reports whether err is a listener bind refused because the
// address is already taken, usually by another running Core.
func IsAddrInUse(err error) bool {
	return addrInUse(err)
}

// LogBindFailure logs a failed API listener bind. A port already in use is
// the user's environment (another Core, or another program on the port), not
// a fault in this build.
func LogBindFailure(err error) {
	if IsAddrInUse(err) {
		log.Warn().Err(err).Msg("failed to bind to port, address already in use")
		return
	}
	log.Error().Err(err).Msg("failed to bind to port")
}

// StartupFailureLevel is the level to log a failed service start at. A port
// already in use and a database newer than this build are both logged at Warn
// where they happen and explained to the user, so neither is a fault to report
// again here.
func StartupFailureLevel(err error) zerolog.Level {
	if IsAddrInUse(err) {
		return zerolog.WarnLevel
	}
	return database.StartupFailureLevel(err)
}
