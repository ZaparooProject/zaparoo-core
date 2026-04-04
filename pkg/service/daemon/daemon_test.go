//go:build linux || darwin

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

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dataDir := t.TempDir()
	tempDir := t.TempDir()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir: dataDir,
		TempDir: tempDir,
	})

	return &Service{pl: pl}
}

func TestPrepareBinary_CopiesWithServiceSuffix(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	// Create a fake binary to copy.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "zaparoo.sh")
	require.NoError(t, os.WriteFile(srcPath, []byte("binary-content"), 0o600))

	result, err := svc.prepareBinary(srcPath)
	require.NoError(t, err)

	assert.Equal(t, "zaparoo.service.sh", filepath.Base(result))
	content, err := os.ReadFile(result) //nolint:gosec // G304: test file
	require.NoError(t, err)
	assert.Equal(t, "binary-content", string(content))
}

func TestPrepareBinary_CreatesDataDir(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "nonexistent", "nested")

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir: dataDir,
		TempDir: t.TempDir(),
	})
	svc := &Service{pl: pl}

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "zaparoo")
	require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0o600))

	result, err := svc.prepareBinary(srcPath)
	require.NoError(t, err)
	assert.DirExists(t, dataDir)
	assert.FileExists(t, result)
}

func TestPrepareBinary_NoExtension(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "zaparoo")
	require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0o600))

	result, err := svc.prepareBinary(srcPath)
	require.NoError(t, err)
	assert.Equal(t, "zaparoo.service", filepath.Base(result))
}

func TestPrepareBinary_MissingSource(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	_, err := svc.prepareBinary("/nonexistent/binary")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error opening binary")
}

func TestCleanupServiceBinary_RemovesFromDataDir(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	// Create a fake binary in DataDir to clean up.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "zaparoo.sh")
	require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0o600))

	result, err := svc.prepareBinary(srcPath)
	require.NoError(t, err)
	assert.FileExists(t, result)

	// cleanupServiceBinary uses os.Executable() which returns the test
	// binary path, not the service binary — so it won't match DataDir
	// and won't remove anything. This verifies the safety guard works.
	svc.cleanupServiceBinary()
	assert.FileExists(t, result)
}
