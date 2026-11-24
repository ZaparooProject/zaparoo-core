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
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
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

func TestParseSidecar(t *testing.T) {
	t.Parallel()
	fields, err := parseSidecar([]byte(`{
		"name": "  Tetris  ",
		"description": "Line one\r\nLine two\tend",
		"image": "art/cover.png",
		"future": {"nested": true}
	}`))
	require.NoError(t, err)
	assert.Equal(t, "Tetris", fields.Name)
	assert.Equal(t, "Line one\nLine two end", fields.Description)
	assert.Equal(t, "art/cover.png", fields.Image)
}

func TestParseSidecarDropsBadFieldsOnly(t *testing.T) {
	t.Parallel()
	fields, err := parseSidecar([]byte(`{
		"name": 42,
		"description": "` + strings.Repeat("d", maxSidecarDescriptionRunes+1) + `",
		"image": "` + strings.Repeat("i", maxSidecarImagePathBytes+1) + `"
	}`))
	require.NoError(t, err)
	assert.Equal(t, sidecarFields{}, fields)

	fields, err = parseSidecar([]byte(`{"name": "` + strings.Repeat("n", maxSidecarNameRunes) + `", "image": null}`))
	require.NoError(t, err)
	assert.Len(t, fields.Name, maxSidecarNameRunes, "a name at the limit is kept")
	assert.Empty(t, fields.Image)
}

func TestParseSidecarStripsControlCharacters(t *testing.T) {
	t.Parallel()
	fields, err := parseSidecar([]byte(`{"name": "Te\u0000tr\u001bis\u202e\nGame", "description": "a\u0007b\u2066c"}`))
	require.NoError(t, err)
	assert.Equal(t, "Tetris Game", fields.Name, "a name stays on one line")
	assert.Equal(t, "abc", fields.Description)
}

func TestParseSidecarRejectsNonObjects(t *testing.T) {
	t.Parallel()
	for _, data := range []string{``, `not json`, `[]`, `"name"`, `null`, `{"name": "x"`} {
		_, err := parseSidecar([]byte(data))
		assert.Error(t, err, "input %q", data)
	}
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
	writeDriveFile(t, root, sidecarFileName, []byte(`{"unknown": "only"}`))
	assert.Nil(t, loadSidecar(root), "nothing usable in the file")

	root = t.TempDir()
	writeDriveFile(t, root, sidecarFileName, []byte(`{"name": `))
	assert.Nil(t, loadSidecar(root), "malformed JSON")

	root = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, sidecarFileName), 0o750))
	assert.Nil(t, loadSidecar(root), "a directory with the file's name")
}

func TestLoadSidecarOversized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	padding := strings.Repeat(" ", maxSidecarBytes)
	writeDriveFile(t, root, sidecarFileName, []byte(`{"name":"Tetris"}`+padding))
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
			writeDriveFile(t, root, sidecarFileName, []byte(`{"name":"Tetris","image":"`+tt.image+`"}`))

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
	require.NoError(t, os.Symlink(target, filepath.Join(root, sidecarFileName)))
	assert.Nil(t, loadSidecar(root), "a linked zaparoo.json is skipped")

	root = t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(outsideDir, "cover.png"), filepath.Join(root, "cover.png")))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(root, "art")))
	for _, image := range []string{"cover.png", "art/cover.png"} {
		writeDriveFile(t, root, sidecarFileName, []byte(`{"name":"Tetris","image":"`+image+`"}`))
		meta := loadSidecar(root)
		require.NotNil(t, meta)
		assert.Empty(t, meta.ImagePath, "image %q reached through a link", image)
	}
}

func TestHandleMountEvent_AttachesSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDriveFile(t, root, tokenFileName, []byte("game.sfc"))
	writeDriveFile(t, root, sidecarFileName, []byte(`{"name":"Tetris","description":"Falling blocks."}`))

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
	writeDriveFile(t, root, sidecarFileName, []byte(`{not json`))

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

func FuzzParseSidecar(f *testing.F) {
	f.Add([]byte(`{"name":"Tetris","description":"Falling blocks.","image":"cover.png"}`))
	f.Add([]byte(`{"name":42,"image":null,"extra":[1,2,3]}`))
	f.Add([]byte(`{"name":"\u202eevil\u0000","description":"a\r\nb"}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"name":`))
	f.Fuzz(func(t *testing.T, data []byte) {
		fields, err := parseSidecar(data)
		if err != nil {
			assert.Equal(t, sidecarFields{}, fields)
			return
		}
		assert.LessOrEqual(t, utf8.RuneCountInString(fields.Name), maxSidecarNameRunes)
		assert.LessOrEqual(t, utf8.RuneCountInString(fields.Description), maxSidecarDescriptionRunes)
		assert.LessOrEqual(t, len(fields.Image), maxSidecarImagePathBytes)
		for _, r := range fields.Name {
			assert.False(t, unicode.IsControl(r) || isBidiControl(r), "name keeps %U", r)
		}
		for _, r := range fields.Description {
			assert.False(t, (unicode.IsControl(r) && r != '\n') || isBidiControl(r), "description keeps %U", r)
		}
		assert.Equal(t, strings.TrimSpace(fields.Name), fields.Name)
		assert.Equal(t, strings.TrimSpace(fields.Description), fields.Description)
	})
}
