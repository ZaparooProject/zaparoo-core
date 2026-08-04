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

package tty2oled

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPictureByNameOnDisk(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	formatDir := filepath.Join(cacheDir, "GSC")
	require.NoError(t, os.MkdirAll(formatDir, 0o750))
	picturePath := filepath.Join(formatDir, "bubbles.gsc")
	require.NoError(t, os.WriteFile(picturePath, []byte("picture"), 0o600))
	manager := &PictureManager{cacheDir: cacheDir}

	got, found := manager.findPictureByNameOnDisk("bubbles")

	assert.True(t, found)
	assert.Equal(t, picturePath, got)
}

func TestFindArcadeFallbackOnDisk(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	formatDir := filepath.Join(cacheDir, "GSC")
	require.NoError(t, os.MkdirAll(formatDir, 0o750))
	picturePath := filepath.Join(formatDir, "Arcade.gsc")
	require.NoError(t, os.WriteFile(picturePath, []byte("picture"), 0o600))
	manager := &PictureManager{cacheDir: cacheDir}

	got, found := manager.FindPictureOnDisk("Arcade")

	assert.True(t, found)
	assert.Equal(t, picturePath, got)
}

func TestPictureNameRejectsPaths(t *testing.T) {
	t.Parallel()

	manager := &PictureManager{cacheDir: t.TempDir()}

	_, found := manager.findPictureByNameOnDisk(filepath.Join("..", "bubbles"))

	assert.False(t, found)
}
