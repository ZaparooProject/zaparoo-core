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

package shared

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPNGFileComplete_ValidPNG(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.png")
	data := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, pngIENDTail...)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	complete, err := PNGFileComplete(path)
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestPNGFileComplete_Truncated(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x00}
	require.NoError(t, os.WriteFile(path, data, 0o600))

	complete, err := PNGFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestPNGFileComplete_TooSmall(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.png")
	require.NoError(t, os.WriteFile(path, []byte{0x89, 0x50}, 0o600))

	complete, err := PNGFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestPNGFileComplete_EmptyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.png")
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))

	complete, err := PNGFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func writeBMP(t *testing.T, dir string, magic [2]byte, declaredSize uint32, actualSize int) string {
	t.Helper()
	path := filepath.Join(dir, "test.bmp")
	data := make([]byte, actualSize)
	data[0] = magic[0]
	data[1] = magic[1]
	if actualSize >= 6 {
		binary.LittleEndian.PutUint32(data[2:6], declaredSize)
	}
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestBMPFileComplete_ValidBMP(t *testing.T) {
	t.Parallel()
	path := writeBMP(t, t.TempDir(), [2]byte{'B', 'M'}, 100, 100)

	complete, err := BMPFileComplete(path)
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestBMPFileComplete_PartiallyWritten(t *testing.T) {
	t.Parallel()
	path := writeBMP(t, t.TempDir(), [2]byte{'B', 'M'}, 100, 50)

	complete, err := BMPFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestBMPFileComplete_ZeroDeclaredSize(t *testing.T) {
	t.Parallel()
	path := writeBMP(t, t.TempDir(), [2]byte{'B', 'M'}, 0, 10)

	complete, err := BMPFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestBMPFileComplete_InvalidMagic(t *testing.T) {
	t.Parallel()
	path := writeBMP(t, t.TempDir(), [2]byte{0x00, 0x00}, 100, 100)

	complete, err := BMPFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestBMPFileComplete_TooSmall(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.bmp")
	require.NoError(t, os.WriteFile(path, []byte{0x42, 0x4d, 0x00}, 0o600))

	complete, err := BMPFileComplete(path)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestPNGFileComplete_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := PNGFileComplete("/nonexistent/path/missing.png")
	require.Error(t, err)
}

func TestBMPFileComplete_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := BMPFileComplete("/nonexistent/path/missing.bmp")
	require.Error(t, err)
}

func TestScreenshotFileComplete_UnsupportedFormat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.jpg")
	require.NoError(t, os.WriteFile(path, []byte{0xFF, 0xD8}, 0o600))

	_, err := ScreenshotFileComplete(path, ".jpg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported screenshot format")
}
