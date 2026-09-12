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

package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type atomicFailureFS struct {
	afero.Fs
	stage string
}

//nolint:wrapcheck // Fault injector preserves filesystem errors unchanged.
func (fs atomicFailureFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if fs.stage == "create" {
		return nil, syscall.ENOSPC
	}
	file, err := fs.Fs.OpenFile(path, flag, mode)
	if err != nil {
		return nil, err
	}
	return atomicFailureFile{File: file, stage: fs.stage}, nil
}

//nolint:wrapcheck // Fault injector preserves filesystem errors unchanged.
func (fs atomicFailureFS) Rename(from, to string) error {
	if fs.stage == "rename" {
		return syscall.ENOSPC
	}
	return fs.Fs.Rename(from, to)
}

type atomicFailureFile struct {
	afero.File
	stage string
}

//nolint:wrapcheck // Fault injector preserves filesystem errors unchanged.
func (f atomicFailureFile) Write(data []byte) (int, error) {
	if f.stage == "write" || f.stage == "short" {
		n, err := f.File.Write(data[:min(1, len(data))])
		if f.stage == "short" {
			return n, err
		}
		return n, errors.Join(err, syscall.ENOSPC)
	}
	return f.File.Write(data)
}

//nolint:wrapcheck // Fault injector preserves filesystem errors unchanged.
func (f atomicFailureFile) Sync() error {
	if f.stage == "sync" {
		return syscall.ENOSPC
	}
	return f.File.Sync()
}

//nolint:wrapcheck // Fault injector preserves filesystem errors unchanged.
func (f atomicFailureFile) Close() error {
	err := f.File.Close()
	if f.stage == "close" {
		return errors.Join(err, syscall.ENOSPC)
	}
	return err
}

func TestWriteConfigAtomicallyPreservesOriginal(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"create", "write", "short", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			dir := "settings"
			path := filepath.Join(dir, CfgFile)
			require.NoError(t, fs.MkdirAll(dir, 0o700))
			require.NoError(t, afero.WriteFile(fs, path, []byte("original"), 0o600))
			err := writeConfigAtomically(atomicFailureFS{Fs: fs, stage: stage}, path, []byte("replacement"))
			if stage == "short" {
				require.ErrorIs(t, err, io.ErrShortWrite)
			} else {
				require.ErrorIs(t, err, syscall.ENOSPC)
			}
			contents, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			assert.Equal(t, "original", string(contents))
			files, err := afero.ReadDir(fs, dir)
			require.NoError(t, err)
			require.Len(t, files, 1, "temporary files must be removed")
			assert.Equal(t, CfgFile, files[0].Name())
		})
	}
}

func TestConfigSavePathsPreserveOriginalOnDiskFull(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"config", "auth_save", "auth_delete"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfigInstance(t)
			fs := cfg.getFs()
			path := cfg.authPath
			require.NoError(t, cfg.SaveAuthEntry("https://example.invalid", CredentialEntry{Bearer: "synthetic"}))
			if operation == "config" {
				path = cfg.cfgPath
				require.NoError(t, cfg.Save())
			}
			original, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			cfg.fs = atomicFailureFS{Fs: fs, stage: "write"}
			switch operation {
			case "config":
				err = cfg.Save()
			case "auth_save":
				err = cfg.SaveAuthEntry("https://example.invalid", CredentialEntry{Bearer: "replacement"})
			case "auth_delete":
				err = cfg.DeleteAuthEntries([]string{"https://example.invalid"})
			}
			require.ErrorIs(t, err, syscall.ENOSPC)
			actual, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			assert.Equal(t, original, actual)
		})
	}
}

func TestWriteConfigAtomicallyCreatesAndReplaces(t *testing.T) {
	t.Parallel()
	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), AuthFile)
	for _, contents := range []string{"first", "second"} {
		require.NoError(t, writeConfigAtomically(fs, path, []byte(contents)))
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, contents, string(got))
		info, err := fs.Stat(path)
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
	}
}
