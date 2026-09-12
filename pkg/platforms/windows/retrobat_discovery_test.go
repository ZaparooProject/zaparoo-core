//go:build windows

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

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingStatFS struct {
	afero.Fs
	err  error
	path string
}

func (fs failingStatFS) Stat(path string) (os.FileInfo, error) {
	if path == fs.path {
		return nil, fs.err
	}
	return fs.Fs.Stat(path) //nolint:wrapcheck // Preserve the underlying filesystem's error shape.
}

func TestFindRetroBatDirFS(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"absent", "configured missing", "configured directory", "configured file", "configured denied",
		"found", "missing executable", "executable directory", "directory denied", "executable denied",
		"normalize failure", "fallback", "mixed stat error",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mem := afero.NewMemMapFs()
			fs := mem
			root := filepath.Join("games", "RetroBat")
			exe := filepath.Join(root, "retrobat.exe")
			configured := ""
			normalize := func(path string) (string, error) { return path, nil }
			var wantErr error
			wantAbsent, wantSuccess := false, false
			switch name {
			case "absent":
				wantAbsent = true
			case "configured missing":
				configured = filepath.Join("custom", "RetroBat")
				wantErr = os.ErrNotExist
			case "configured directory":
				configured = root
				require.NoError(t, mem.MkdirAll(root, 0o755))
				wantSuccess = true
			case "configured file":
				configured = root
				require.NoError(t, mem.MkdirAll(filepath.Dir(root), 0o755))
				require.NoError(t, afero.WriteFile(mem, root, nil, 0o600))
			case "directory denied", "configured denied":
				if name == "configured denied" {
					configured = root
				}
				fs = failingStatFS{Fs: mem, path: root, err: os.ErrPermission}
				wantErr = os.ErrPermission
			case "mixed stat error":
				fs = failingStatFS{Fs: mem, path: root, err: errors.Join(os.ErrNotExist, os.ErrPermission)}
				wantErr = os.ErrPermission
			default:
				require.NoError(t, mem.MkdirAll(root, 0o755))
				if name != "missing executable" && name != "executable directory" {
					require.NoError(t, afero.WriteFile(mem, exe, nil, 0o600))
				}
				switch name {
				case "found":
					wantSuccess = true
				case "missing executable":
					wantAbsent = true
				case "executable directory":
					require.NoError(t, mem.MkdirAll(exe, 0o755))
					wantAbsent = true
				case "executable denied":
					fs = failingStatFS{Fs: mem, path: exe, err: os.ErrPermission}
					wantErr = os.ErrPermission
				case "normalize failure":
					wantErr = errors.New("directory listing failed")
					normalize = func(string) (string, error) { return "", wantErr }
				case "fallback":
					configured = filepath.Join("missing", "RetroBat")
					wantSuccess = true
				}
			}
			path, err := findRetroBatDirFS(fs, configured, []string{root}, normalize)
			if wantSuccess {
				require.NoError(t, err)
				assert.Equal(t, root, path)
				return
			}
			require.Error(t, err)
			assert.Empty(t, path)
			assert.Equal(t, wantAbsent, errors.Is(err, errRetroBatNotInstalled))
			if wantErr != nil {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}
}
