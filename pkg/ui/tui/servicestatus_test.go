/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// platformWithLog returns a platform whose log directory holds the given file
// contents.
func platformWithLog(t *testing.T, contents string) platforms.Platform {
	t.Helper()

	logDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(logDir, config.LogFile), []byte(contents), 0o600))

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: logDir, DataDir: t.TempDir()})
	return pl
}

// These lines are the ones a MiSTer wrote when its user database was held
// schema-ahead: the cause, then the record built for a person to read, then the
// symptom that arrives half a minute later when the TUI's own client dials a
// websocket the failed service is not serving.
const (
	logCause = `{"level":"error",` +
		`"error":"error migrating userdb: database schema is newer than this binary supports",` +
		`"time":"2026-09-21T07:32:23+08:00","message":"error opening databases"}`
	logFailedRecord = `{"level":"error","headline":"Zaparoo cannot open your saved data",` +
		`"detail":"Your history, mappings and profiles were upgraded by a newer version.",` +
		`"time":"2026-09-21T07:32:23+08:00","message":"service entered failed state"}`
	logSymptom = `{"level":"error","error":"failed to dial websocket (HTTP status 503): websocket: bad handshake",` +
		`"time":"2026-09-21T07:32:53+08:00","message":"error disabling runZapScript"}`
	logRoutine = `{"level":"info","time":"2026-09-21T07:32:20+08:00","message":"starting service"}`
)

func TestLastLoggedError(t *testing.T) {
	t.Parallel()

	t.Run("the failed-state headline beats a later symptom", func(t *testing.T) {
		t.Parallel()

		pl := platformWithLog(t, logRoutine+"\n"+logCause+"\n"+logFailedRecord+"\n"+logSymptom+"\n")

		reason := lastLoggedError(pl)

		assert.Equal(t, "Zaparoo cannot open your saved data", reason)
		assert.NotContains(t, reason, "runZapScript",
			"the most recent error is a symptom of the failure, not its cause")
	})

	t.Run("falls back to the most recent error when nothing failed a start", func(t *testing.T) {
		t.Parallel()

		pl := platformWithLog(t, logRoutine+"\n"+logSymptom+"\n")

		assert.Equal(t,
			"error disabling runZapScript: failed to dial websocket (HTTP status 503): websocket: bad handshake",
			lastLoggedError(pl))
	})

	t.Run("says nothing when the log holds no error", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, lastLoggedError(platformWithLog(t, logRoutine+"\n")))
	})

	t.Run("says nothing when there is no log", func(t *testing.T) {
		t.Parallel()

		pl := mocks.NewMockPlatform()
		pl.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})

		assert.Empty(t, lastLoggedError(pl))
	})
}

// The stopped block used to carry a fixed "may not have started" and nothing
// else, which is the whole of what a user saw when an older binary refused a
// schema-ahead database and exited.
func TestServiceStatusText_NamesTheReasonAndTheButton(t *testing.T) {
	t.Parallel()

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	pl := platformWithLog(t, logCause+"\n"+logFailedRecord+"\n"+logSymptom+"\n")

	failed := serviceStatusText(cfg, pl, serviceFailed)
	assert.Contains(t, failed, "NOT WORKING")
	assert.Contains(t, failed, "Zaparoo cannot open your saved data")
	assert.Contains(t, failed, "press Logs", "the button that still works has to be named")

	stopped := serviceStatusText(cfg, pl, serviceStopped)
	assert.Contains(t, stopped, "NOT RUNNING")
	assert.Contains(t, stopped, "Zaparoo cannot open your saved data")
	assert.Contains(t, stopped, "Press Logs")
}
