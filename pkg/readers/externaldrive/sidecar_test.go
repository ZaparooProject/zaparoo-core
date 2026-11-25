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

package externaldrive

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/sidecar"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDriveFile(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestLoadSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cover := writeDriveFile(t, root, filepath.Join("art", "Cover.JPG"), []byte("jpeg"))
	writeDriveFile(t, root, "ZAPAROO.JSON",
		[]byte(`{"name":"Tetris","description":"Falling blocks.","image":"art/Cover.JPG"}`))

	meta := loadSidecar(root)
	require.NotNil(t, meta)
	assert.Equal(t, tokens.Metadata{Name: "Tetris", Description: "Falling blocks.", ImagePath: cover}, *meta)
}

func TestLoadSidecarMissingOrEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, loadSidecar(t.TempDir()), "no file")

	root := t.TempDir()
	writeDriveFile(t, root, sidecar.FileName, []byte(`{"unknown": "only"}`))
	assert.Nil(t, loadSidecar(root), "nothing usable in the file")

	root = t.TempDir()
	writeDriveFile(t, root, sidecar.FileName, []byte(`{"name": `))
	assert.Nil(t, loadSidecar(root), "malformed JSON")

	root = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, sidecar.FileName), 0o750))
	assert.Nil(t, loadSidecar(root), "a directory with the file's name")
}

func TestLoadSidecarOversized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	padding := strings.Repeat(" ", sidecar.MaxBytes)
	writeDriveFile(t, root, sidecar.FileName, []byte(`{"name":"Tetris"}`+padding))
	assert.Nil(t, loadSidecar(root))
}

func TestLoadSidecarRejectsBadImages(t *testing.T) {
	t.Parallel()
	outside := writeDriveFile(t, t.TempDir(), "secret.png", []byte("png"))
	tests := []struct {
		setup func(t *testing.T, root string)
		name  string
		image string
	}{
		{name: "missing", image: "cover.png"},
		{name: "wrong extension", image: "cover.gif", setup: func(t *testing.T, root string) {
			writeDriveFile(t, root, "cover.gif", []byte("gif"))
		}},
		{name: "parent escape", image: "../secret.png"},
		{name: "absolute", image: filepath.ToSlash(outside)},
		{name: "directory", image: "art.png", setup: func(t *testing.T, root string) {
			require.NoError(t, os.Mkdir(filepath.Join(root, "art.png"), 0o750))
		}},
		{name: "too large", image: "cover.png", setup: func(t *testing.T, root string) {
			path := writeDriveFile(t, root, "cover.png", nil)
			require.NoError(t, os.Truncate(path, imagefiles.MaxBytes+1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, root)
			}
			writeDriveFile(t, root, sidecar.FileName, []byte(`{"name":"Tetris","image":"`+tt.image+`"}`))

			meta := loadSidecar(root)
			require.NotNil(t, meta, "a bad image drops only the image")
			assert.Equal(t, "Tetris", meta.Name)
			assert.Empty(t, meta.ImagePath)
		})
	}
}

func TestLoadSidecarRejectsSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	outsideDir := t.TempDir()
	target := writeDriveFile(t, outsideDir, "meta.json", []byte(`{"name":"Elsewhere"}`))
	writeDriveFile(t, outsideDir, "cover.png", []byte("png"))

	root := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(root, sidecar.FileName)))
	assert.Nil(t, loadSidecar(root), "a linked zaparoo.json is skipped")

	root = t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(outsideDir, "cover.png"), filepath.Join(root, "cover.png")))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(root, "art")))
	for _, image := range []string{"cover.png", "art/cover.png"} {
		writeDriveFile(t, root, sidecar.FileName, []byte(`{"name":"Tetris","image":"`+image+`"}`))
		meta := loadSidecar(root)
		require.NotNil(t, meta)
		assert.Empty(t, meta.ImagePath, "image %q reached through a link", image)
	}
}

func TestHandleMountEvent_AttachesSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDriveFile(t, root, tokenFileName, []byte("game.sfc"))
	writeDriveFile(t, root, sidecar.FileName, []byte(`{"name":"Tetris","description":"Falling blocks."}`))

	reader := NewReader(&config.Instance{})
	scanChan := make(chan readers.Scan, 1)
	reader.device = config.ReadersConnect{Driver: DriverID}
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	reader.wg.Add(1)
	reader.handleMountEvent(&MountEvent{DeviceID: "sidecar-device", MountPath: root})

	scan := <-scanChan
	require.NotNil(t, scan.Token)
	assert.Equal(t, "game.sfc", scan.Token.Text, "zaparoo.txt alone decides what runs")
	require.NotNil(t, scan.Token.Metadata)
	assert.Equal(t, "Tetris", scan.Token.Metadata.Name)
	assert.Equal(t, "Falling blocks.", scan.Token.Metadata.Description)
}

func TestHandleMountEvent_BadSidecarStillScans(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDriveFile(t, root, tokenFileName, []byte("game.sfc"))
	writeDriveFile(t, root, sidecar.FileName, []byte(`{not json`))

	reader := NewReader(&config.Instance{})
	scanChan := make(chan readers.Scan, 1)
	reader.device = config.ReadersConnect{Driver: DriverID}
	reader.scanChan = scanChan
	reader.stopChan = make(chan struct{})

	reader.wg.Add(1)
	reader.handleMountEvent(&MountEvent{DeviceID: "bad-sidecar-device", MountPath: root})

	scan := <-scanChan
	require.NotNil(t, scan.Token)
	assert.Equal(t, "game.sfc", scan.Token.Text)
	assert.Nil(t, scan.Token.Metadata)
}
