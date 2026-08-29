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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// ExitUnrecoverable is the exit status for a startup failure that starting
// again cannot fix.
//
// The service units set RestartPreventExitStatus to it. Without that, a
// supervisor retries every few seconds forever: each attempt reopens the
// databases, fails in the same place, and writes the same error, so the one
// line explaining the problem is buried under repetitions of itself and the
// device looks like it is doing something about it. Stopping at a failed unit
// with the reason still visible is the more useful answer.
//
// 78 is EX_CONFIG from sysexits.h, which is close enough to what this is: the
// state on disk is not something this build can work with.
const ExitUnrecoverable = 78

// ExitCodeFor maps a startup error to the process exit status to leave behind.
//
// Only conditions a person has to resolve qualify. Anything transient — a
// missing network, a busy file, a database that can be recovered — stays 1 so
// the supervisor keeps trying, because those do come good on their own.
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	// A schema written by a newer build. Core refuses to start rather than
	// discard user data it cannot reconstruct, and no number of restarts
	// changes what is in the file; recovery is in docs/ota-runbook.md.
	if errors.Is(err, database.ErrSchemaAhead) {
		return ExitUnrecoverable
	}
	return 1
}
