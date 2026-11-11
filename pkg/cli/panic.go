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
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/rs/zerolog/log"
)

// ExitOnPanic reports a panic on stderr, in the log and to telemetry, then
// exits with status 1. Defer it directly, since recover only stops a panic
// from the deferred call itself.
func ExitOnPanic() {
	//nolint:revive // ExitOnPanic is the deferred function, so recover works here
	if r := recover(); r != nil {
		reportPanic(os.Stderr, r)
		os.Exit(1)
	}
}

func reportPanic(w io.Writer, r any) {
	stack := debug.Stack()
	_, _ = fmt.Fprintf(w, "Panic: %v\n%s\n", r, stack)
	log.Error().
		Interface("panic", r).
		Bytes("stack", stack).
		Msg("recovered from panic")
	telemetry.Flush()
}
