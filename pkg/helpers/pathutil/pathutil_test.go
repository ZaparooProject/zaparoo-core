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

package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomicPreservesSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "config-link")
	target := filepath.Join(dir, "real-config")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	require.NoError(t, os.Symlink(filepath.Base(target), link))
	require.NoError(t, WriteFileAtomic(afero.NewOsFs(), link, []byte("new"), 0o600))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	got, err := afero.ReadFile(afero.NewOsFs(), target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	require.NoError(t, os.Remove(target))
	require.NoError(t, WriteFileAtomic(afero.NewOsFs(), link, []byte("recreated"), 0o600))
	got, err = afero.ReadFile(afero.NewOsFs(), target)
	require.NoError(t, err)
	assert.Equal(t, "recreated", string(got))
}

func TestWriteFileAtomicRejectsSymlinkLoop(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	path := filepath.Join(t.TempDir(), "loop")
	require.NoError(t, os.Symlink("loop", path))
	require.ErrorContains(t, WriteFileAtomic(afero.NewOsFs(), path, []byte("new"), 0o600), "too many")
	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
}

func TestCanonicalMediaPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "empty", path: "", expected: ""},
		{name: "native path", path: filepath.Join("roms", "NES", "Game.nes"), expected: "roms/NES/Game.nes"},
		{name: "backslash path", path: `roms\NES\Game.nes`, expected: "roms/NES/Game.nes"},
		{
			name:     "cleans filesystem path",
			path:     filepath.Join("roms", "NES", "..", "SNES", "Game.sfc"),
			expected: "roms/SNES/Game.sfc",
		},
		{name: "uri unchanged", path: "steam://12345", expected: "steam://12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, CanonicalMediaPath(tt.path))
		})
	}
}

func TestResolveRelativePath(t *testing.T) {
	t.Parallel()

	exeDir := ExeDir()
	if exeDir == "" {
		t.Skip("ExeDir() returned empty, cannot test relative path resolution")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty string unchanged",
			path:     "",
			expected: "",
		},
		{
			name:     "absolute path unchanged",
			path:     exeDir,
			expected: exeDir,
		},
		{
			name:     "relative path resolved to ExeDir",
			path:     filepath.Join("roms", "nes"),
			expected: filepath.Join(exeDir, "roms", "nes"),
		},
		{
			name:     "dot relative path resolved",
			path:     "./games",
			expected: filepath.Join(exeDir, "games"),
		},
		{
			name:     "single filename resolved",
			path:     "roms",
			expected: filepath.Join(exeDir, "roms"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveRelativePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExeDir(t *testing.T) {
	t.Parallel()

	dir := ExeDir()
	if dir == "" {
		t.Skip("ExeDir() returned empty")
	}

	assert.True(t, filepath.IsAbs(dir), "ExeDir should return an absolute path")
}
