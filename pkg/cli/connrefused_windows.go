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

package cli

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isConnectionRefused reports whether a dial failed because nothing was
// listening, which is what a CLI command sees when Core is not running.
//
// A refused connect on Windows comes back as the Winsock error, not the POSIX
// one: syscall.ECONNREFUSED exists here but is a placeholder in the synthetic
// APPLICATION_ERROR range that no socket call ever returns, and Errno.Is has no
// mapping between the two. Comparing against only the POSIX name therefore
// never matches, which logged "Core is not running" at Error and reported an
// expected situation to Sentry every time a CLI command ran with the service
// stopped. Both names are checked so neither platform relies on the other's.
func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, windows.WSAECONNREFUSED)
}
