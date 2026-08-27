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
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.uber.org/goleak"
)

// testLogRedirector is installed as the global logger's writer for the whole
// test binary, so tests capture output by pointing it somewhere rather than by
// assigning log.Logger. See logRedirector for why that assignment was a race.
var testLogRedirector = &logRedirector{fallback: os.Stderr, level: zerolog.TraceLevel}

func TestMain(m *testing.M) {
	// Install once, before any test runs, and leave it alone from here on.
	// Everything reaches the writer; captureLogs applies the per-test level.
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	log.Logger = zerolog.New(testLogRedirector).With().Timestamp().Logger()

	goleak.VerifyTestMain(m,
		// melody's hub goroutine is managed by the library and Close() is async
		goleak.IgnoreTopFunction("github.com/olahol/melody.(*hub).run"),
	)
}
