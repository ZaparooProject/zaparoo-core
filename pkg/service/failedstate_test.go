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

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	testmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// freePort borrows an OS-assigned port and hands it straight back.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	require.NoError(t, listener.Close())
	return addr.Port
}

// seedMigratedUserDB creates a user database at the current schema.
func seedMigratedUserDB(ctx context.Context, t *testing.T, pl platforms.Platform) {
	t.Helper()
	db, err := userdb.OpenUserDB(ctx, pl)
	require.NoError(t, err)
	require.NoError(t, db.MigrateUp())
	require.NoError(t, db.Close())
}

func readHealthState(t *testing.T, port int) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), http.NoBody,
	)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(body, &payload), "body was %q", body)
	return payload.State
}

func readPage(t *testing.T, port int) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/app/", port), http.NoBody,
	)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

// A user database migrated by a newer build is refused, and that refusal is
// right: the media database is rebuilt in the same situation because a reindex
// reconstructs it, while history, mappings and profiles have no such source.
//
// What used to happen next is the actual problem. The process exited, so on a
// supervised platform it restarted every few seconds burying the one line that
// explained it, and on MiSTer — which supervises nothing — it was simply gone
// by the time anyone looked. The app sat on "Connecting..." either way. Core
// now stays up on the port it already holds and says what happened.
func TestStart_SchemaAheadUserDBReportsInsteadOfExiting(t *testing.T) {
	ctx := context.Background()

	port := freePort(t)
	testRoot := t.TempDir()
	settings := platforms.Settings{
		ConfigDir: testRoot,
		DataDir:   testRoot,
		LogDir:    testRoot,
		TempDir:   testRoot,
	}

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, testRoot, "127.0.0.1", port)
	require.NoError(t, err)
	cfg.SetUpdateCheck(false)

	mockPlatform := testmocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(settings)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{testRoot})
	mockPlatform.On("SupportedReaders", mock.AnythingOfType("*config.Instance")).Return([]readers.Reader{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{})
	mockPlatform.On("ManagedByPackageManager").Return(false)
	mockPlatform.On("StartPre", cfg).Return(nil)
	mockPlatform.On("Stop").Return(nil).Maybe()

	seedMigratedUserDB(ctx, t, mockPlatform)
	markSchemaAhead(ctx, t, testRoot, config.UserDbFile)

	t.Cleanup(crashdump.Stop)
	svcResult, startErr := Start(mockPlatform, cfg)
	require.NoError(t, startErr, "a refused user database must not end the process")
	require.NotNil(t, svcResult)
	t.Cleanup(func() { assert.NoError(t, svcResult.Stop()) })

	assert.Equal(t, "failed", readHealthState(t, port),
		"health has to report the failure, so a client sees something other than a dead port")

	page := readPage(t, port)
	assert.Contains(t, page, "Zaparoo cannot open your saved data")
	assert.Contains(t, page, "upgraded by Zaparoo v",
		"the page has to name the version that wrote the schema, not a goose timestamp")
	assert.Contains(t, page, "core.log", "and where to find the rest of it")

	// Nothing behind the page may be running: the databases are not open.
	assert.False(t, svcResult.RestartRequested())
}

// Stopping from the failed state has to work, or a device that reported a
// problem cannot be shut down cleanly and the port stays held.
func TestStart_FailedStateStopsCleanly(t *testing.T) {
	ctx := context.Background()

	port := freePort(t)
	testRoot := t.TempDir()
	settings := platforms.Settings{
		ConfigDir: testRoot,
		DataDir:   testRoot,
		LogDir:    testRoot,
		TempDir:   testRoot,
	}

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, testRoot, "127.0.0.1", port)
	require.NoError(t, err)
	cfg.SetUpdateCheck(false)

	mockPlatform := testmocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(settings)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{testRoot})
	mockPlatform.On("SupportedReaders", mock.AnythingOfType("*config.Instance")).Return([]readers.Reader{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{})
	mockPlatform.On("ManagedByPackageManager").Return(false)
	mockPlatform.On("StartPre", cfg).Return(nil)
	mockPlatform.On("Stop").Return(nil).Maybe()

	seedMigratedUserDB(ctx, t, mockPlatform)
	markSchemaAhead(ctx, t, testRoot, config.UserDbFile)

	t.Cleanup(crashdump.Stop)
	svcResult, startErr := Start(mockPlatform, cfg)
	require.NoError(t, startErr)
	require.NotNil(t, svcResult)

	require.NoError(t, svcResult.Stop())

	select {
	case <-svcResult.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("the failed state did not finish shutting down")
	}

	// The port has to come back, or the next start cannot bind it.
	listener, err := (&net.ListenConfig{}).Listen(
		context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port),
	)
	require.NoError(t, err, "the failed state must release the port when stopped")
	require.NoError(t, listener.Close())
}
