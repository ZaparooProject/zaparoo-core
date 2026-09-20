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

package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// volatilePlatform mirrors MiSTer and Mistex: the log lives on a tmpfs, which
// is what LogDir pointing at TempDir means.
func volatilePlatform(t *testing.T, root string) *mocks.MockPlatform {
	t.Helper()

	tmpDir := filepath.Join(root, "tmp")
	dataDir := filepath.Join(root, "data")
	require.NoError(t, os.MkdirAll(tmpDir, 0o750))
	require.NoError(t, os.MkdirAll(dataDir, 0o750))

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir: dataDir,
		TempDir: tmpDir,
		LogDir:  tmpDir,
	})
	return pl
}

// persistentPlatform mirrors every other platform: a log directory of its own
// under the data directory, and a separate temp directory.
func persistentPlatform(t *testing.T, root string) *mocks.MockPlatform {
	t.Helper()

	dataDir := filepath.Join(root, "data")
	logDir := filepath.Join(dataDir, "logs")
	tmpDir := filepath.Join(root, "tmp")
	require.NoError(t, os.MkdirAll(logDir, 0o750))
	require.NoError(t, os.MkdirAll(tmpDir, 0o750))

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir: dataDir,
		TempDir: tmpDir,
		LogDir:  logDir,
	})
	return pl
}

func writeLiveLog(t *testing.T, pl platforms.Platform, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(LogPath(pl), []byte(contents), 0o600))
}

// The reason PersistLog exists: on MiSTer the live log is on a tmpfs, so the
// one file support asks for is gone after a reboot unless it is copied.
func TestPersistLog_CopiesAVolatileLogIntoTheDataDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pl := volatilePlatform(t, root)
	writeLiveLog(t, pl, `{"level":"error","message":"startup failed"}`+"\n")

	got := PersistLog(pl)

	want := filepath.Join(DataDir(pl), config.LogFile)
	assert.Equal(t, want, got, "callers point the user at the copy, not the tmpfs path")

	persisted, err := os.ReadFile(want) //nolint:gosec // test-controlled path
	require.NoError(t, err, "the copy is the whole point")
	assert.Contains(t, string(persisted), "startup failed")
}

// Every platform other than MiSTer and Mistex already logs to persistent
// storage. Copying there anyway writes a second core.log beside the live one,
// on every clean shutdown, which is both a wasted write of the entire log and
// a stale file that reads like the real one.
func TestPersistLog_LeavesAPersistentLogAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pl := persistentPlatform(t, root)
	writeLiveLog(t, pl, `{"level":"info","message":"hello"}`+"\n")

	got := PersistLog(pl)

	assert.Equal(t, LogPath(pl), got, "the live log is already the file to point at")

	_, err := os.Stat(filepath.Join(DataDir(pl), config.LogFile))
	require.ErrorIs(t, err, os.ErrNotExist,
		"nothing should be written into the data directory next to the databases")
}

// A missing live log must not stop a caller: entering the failed state is
// exactly when the log may not have been created yet.
func TestPersistLog_FallsBackToTheLivePathWhenThereIsNoLog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pl := volatilePlatform(t, root)

	assert.Equal(t, LogPath(pl), PersistLog(pl))
}
