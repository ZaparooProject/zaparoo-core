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

package tui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func healthServingConfig(t *testing.T, body string) *config.Instance {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)

	cfg, err := helpers.NewTestConfigWithPort(helpers.NewMemoryFS(), t.TempDir(), port)
	require.NoError(t, err)
	return cfg
}

func closedPortConfig(t *testing.T) *config.Instance {
	t.Helper()

	// Port 0 cannot have a listening service.
	cfg, err := helpers.NewTestConfigWithPort(helpers.NewMemoryFS(), t.TempDir(), 0)
	require.NoError(t, err)
	return cfg
}

// A PID file only answers whether a process exists, which is why this screen
// used to claim RUNNING through a multi-minute migration. Each condition has
// to come from what the service actually reports.
func TestResolveServiceCondition_SeparatesTheThreeLiveStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want serviceCondition
	}{
		{name: "ready", body: `{"status":"ok","state":"ready"}`, want: serviceRunning},
		{name: "starting", body: `{"status":"starting","state":"starting"}`, want: serviceStarting},
		{name: "failed", body: `{"status":"error","state":"failed"}`, want: serviceFailed},
		{name: "an older build answering OK", body: "OK", want: serviceRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveServiceCondition(healthServingConfig(t, tt.body), func() bool {
				t.Fatal("the process check must not be consulted when health answered")
				return false
			})
			assert.Equal(t, tt.want, got)
		})
	}
}

// Nothing listening still has two answers: a process that has not bound yet,
// and no process at all.
func TestResolveServiceCondition_FallsBackToTheProcessCheck(t *testing.T) {
	t.Parallel()

	assert.Equal(t, serviceStarting,
		resolveServiceCondition(closedPortConfig(t), func() bool { return true }),
		"a process with no listener is one that has not bound yet")

	assert.Equal(t, serviceStopped,
		resolveServiceCondition(closedPortConfig(t), func() bool { return false }))
}

func logPlatform(t *testing.T, lines string) *mocks.MockPlatform {
	t.Helper()

	root := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir: root,
		TempDir: root,
		LogDir:  root,
	})
	if lines != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, config.LogFile), []byte(lines), 0o600))
	}
	return pl
}

// The reason a start failed is already in the log, which is the file support
// asks for. Showing it here saves the user fetching it to answer the first
// question they have.
func TestLastLoggedError_ReportsTheMostRecentError(t *testing.T) {
	t.Parallel()

	pl := logPlatform(t, `{"level":"info","message":"opening databases"}
{"level":"error","message":"startup failed","error":"schema is newer than this binary supports"}
{"level":"info","message":"service entered failed state"}
`)

	assert.Equal(t, "startup failed: schema is newer than this binary supports", lastLoggedError(pl))
}

func TestLastLoggedError_SkipsWhatItCannotUse(t *testing.T) {
	t.Parallel()

	t.Run("no log file", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, lastLoggedError(logPlatform(t, "")))
	})

	t.Run("nothing logged at error level", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, lastLoggedError(logPlatform(t, `{"level":"info","message":"all fine"}`+"\n")))
	})

	t.Run("non-JSON lines are stepped over", func(t *testing.T) {
		t.Parallel()
		pl := logPlatform(t, `{"level":"error","message":"the real one"}
a panic trace that is not JSON
`)
		assert.Equal(t, "the real one", lastLoggedError(pl))
	})
}

// The failed state is the one this screen exists for: it has to say the
// service is not working, give the reason, and point somewhere with more.
func TestServiceStatusText_SaysSomethingDifferentForEachCondition(t *testing.T) {
	t.Parallel()

	cfg := closedPortConfig(t)
	quiet := logPlatform(t, "")

	assert.Contains(t, serviceStatusText(cfg, quiet, serviceRunning), "RUNNING")
	assert.Contains(t, serviceStatusText(cfg, quiet, serviceStarting), "STARTING")
	assert.Contains(t, serviceStatusText(cfg, quiet, serviceStopped), "NOT RUNNING")

	failed := serviceStatusText(cfg, logPlatform(t, `{"level":"error","message":"could not open user.db"}`+"\n"),
		serviceFailed)
	assert.Contains(t, failed, "NOT WORKING")
	assert.Contains(t, failed, "could not open user.db", "the reason is the whole point of this state")
	assert.Contains(t, failed, "/app/", "and somewhere to read the rest of it")
}

// These lines are the ones a MiSTer wrote when its user database was held
// schema-ahead: the cause, then the record built for a person to read, then the
// symptom that arrives half a minute later when the TUI's own client dials a
// websocket the failed service is not serving.
const (
	logCause = `{"level":"error",` +
		`"error":"error migrating userdb: database schema is newer than this binary supports",` +
		`"message":"error opening databases"}`
	logFailedRecord = `{"level":"error","headline":"Zaparoo cannot open your saved data",` +
		`"detail":"Your history, mappings and profiles were upgraded by a newer version.",` +
		`"message":"service entered failed state"}`
	logSymptom = `{"level":"error",` +
		`"error":"failed to dial websocket (HTTP status 503): websocket: bad handshake",` +
		`"message":"error disabling runZapScript"}`
)

// Core keeps logging after it enters the failed state, so the most recent error
// is a symptom and not the cause. Taking it is what put "error disabling
// runZapScript" on a MiSTer whose user database could not be opened.
func TestLastLoggedError_PrefersTheFailedStateHeadline(t *testing.T) {
	t.Parallel()

	pl := logPlatform(t, logCause+"\n"+logFailedRecord+"\n"+logSymptom+"\n")

	reason := lastLoggedError(pl)

	assert.Equal(t, "Zaparoo cannot open your saved data", reason)
	assert.NotContains(t, reason, "runZapScript",
		"the most recent error is a symptom of the failure, not its cause")
}

// The stopped block used to carry a fixed "may not have started" and nothing
// else, which is the whole of what a user saw when an older binary refused a
// schema-ahead database and exited. Both broken states now name the reason and
// the one button that still works.
func TestServiceStatusText_NamesTheReasonAndTheButton(t *testing.T) {
	t.Parallel()

	cfg := closedPortConfig(t)
	pl := logPlatform(t, logCause+"\n"+logFailedRecord+"\n"+logSymptom+"\n")

	failed := serviceStatusText(cfg, pl, serviceFailed)
	assert.Contains(t, failed, "NOT WORKING")
	assert.Contains(t, failed, "Zaparoo cannot open your saved data")
	assert.Contains(t, failed, "press Logs", "the button that still works has to be named")

	stopped := serviceStatusText(cfg, pl, serviceStopped)
	assert.Contains(t, stopped, "NOT RUNNING")
	assert.Contains(t, stopped, "Zaparoo cannot open your saved data",
		"an older binary that refused the database and exited leaves this state, not failed")
	assert.Contains(t, stopped, "Press Logs")
}
