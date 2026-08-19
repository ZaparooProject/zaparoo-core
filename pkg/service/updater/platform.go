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

package updater

import (
	"errors"
	"fmt"
)

// ErrPlatformUnsupported is returned when this build cannot finish an install
// no matter how the release turns out.
var ErrPlatformUnsupported = errors.New("this platform cannot install updates in place")

// preflightPlatform refuses an install this build cannot complete.
//
// Windows locks the running image, so MoveFileEx cannot replace the executable
// from inside the process being replaced. Without the exit-time helper the
// install runs all the way to the replacement and fails there, which is after
// the archive has been downloaded and after the user database has been
// snapshotted and quiesced. The failure unwinds safely, but it costs the user
// a download and a restart-shaped scare to learn something knowable up front.
func preflightPlatform(goos string) error {
	if goos == "windows" {
		return fmt.Errorf(
			"%w: replacing a running Windows executable needs a helper Core does not ship yet, "+
				"so install this release with the Windows installer instead",
			ErrPlatformUnsupported,
		)
	}
	return nil
}
