//go:build linux

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

package startup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rejectedReplacementFS struct{ afero.Fs }

func (rejectedReplacementFS) Rename(string, string) error { return os.ErrPermission }

func TestStartupRoundTripWithoutTrailingNewline(t *testing.T) {
	t.Parallel()
	for _, ending := range []string{"", "\n", "\n\n"} {
		t.Run(ending, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			path := filepath.Join("config", "user-startup.sh")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, afero.WriteFile(fs, path, []byte("#!/bin/sh\n\n# Existing\necho keep"+ending), 0o755))
			var s Startup
			require.NoError(t, s.loadFile(fs, path))
			require.Len(t, s.Entries, 1)
			assert.Equal(t, []string{"echo keep"}, s.Entries[0].Cmds)
			require.NoError(t, s.Add("New", "echo new"))
			require.NoError(t, s.saveFile(fs, path))
			var loaded Startup
			require.NoError(t, loaded.loadFile(fs, path))
			assert.Equal(t, s.Entries, loaded.Entries)
			info, err := fs.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
		})
	}
}

func TestStartupFailedSavePreservesOriginal(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := "startup.sh"
	original := []byte("#!/bin/sh\necho keep\n")
	require.NoError(t, afero.WriteFile(fs, path, original, 0o644))
	s := Startup{Entries: []Entry{{Name: "New", Cmds: []string{"echo new"}, Enabled: true}}}
	require.ErrorIs(t, s.saveFile(rejectedReplacementFS{fs}, path), os.ErrPermission)
	got, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
	files, err := afero.ReadDir(fs, ".")
	require.NoError(t, err)
	require.Len(t, files, 1)
}
