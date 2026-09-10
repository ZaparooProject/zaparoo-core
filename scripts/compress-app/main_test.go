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

package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stagingRecordingFS struct {
	afero.Fs
	stagingPath string
}

func (fs *stagingRecordingFS) Rename(oldname, newname string) error {
	fs.stagingPath = oldname
	//nolint:wrapcheck // Transparent test filesystem wrapper.
	return fs.Fs.Rename(oldname, newname)
}

func TestCompressApp(t *testing.T) {
	t.Parallel()
	fs := &stagingRecordingFS{Fs: afero.NewMemMapFs()}
	base := t.TempDir()
	source, output := filepath.Join(base, "raw"), filepath.Join(base, "packed", "dist")
	files := map[string][]byte{
		"index.html":                             []byte("<!DOCTYPE html><title>app</title>"),
		filepath.Join("assets", "legacy-app.js"): []byte("console.log('legacy');"),
		filepath.Join("assets", "app.js"):        []byte("console.log('app');"),
		filepath.Join("assets", "data.gz"):       []byte("an asset with a gzip extension"),
		"empty":                                  {},
	}
	for name, content := range files {
		path := filepath.Join(source, name)
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(fs, path, content, 0o644))
	}
	for _, name := range []string{filepath.Join(".hidden", "ignore"), "_private", ".ignore"} {
		path := filepath.Join(source, name)
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(fs, path, []byte("excluded"), 0o644))
	}
	require.NoError(t, compressApp(fs, source, output))
	assert.True(t, strings.HasPrefix(filepath.Base(fs.stagingPath), "."),
		"go:embed must ignore incomplete staging files")
	first := map[string][]byte{}
	walkErr := afero.Walk(fs, output, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		if !info.IsDir() {
			data, readErr := afero.ReadFile(fs, path)
			require.NoError(t, readErr)
			first[path] = data
		}
		return nil
	})
	require.NoError(t, walkErr)
	require.Len(t, first, len(files))
	for name, want := range files {
		compressed := first[filepath.Join(output, name+".gz")]
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		require.NoError(t, err)
		assert.True(t, reader.ModTime.IsZero())
		assert.Empty(t, reader.Name)
		got, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		assert.Equal(t, want, got)
		original, err := afero.ReadFile(fs, filepath.Join(source, name))
		require.NoError(t, err)
		assert.Equal(t, want, original, "source remains untouched")
		require.NoError(t, fs.Chtimes(filepath.Join(source, name), time.Unix(100, 0), time.Unix(100, 0)))
	}
	require.NoError(t, compressApp(fs, source, output))
	for path, want := range first {
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, want, got, "compression is independent of timestamps")
	}
	require.NoError(t, fs.Remove(filepath.Join(source, "empty")))
	require.NoError(t, compressApp(fs, source, output))
	_, err := fs.Stat(filepath.Join(output, "empty.gz"))
	require.ErrorIs(t, err, os.ErrNotExist, "deleted assets do not survive regeneration")
	require.NoError(t, fs.RemoveAll(source))
	require.NoError(t, compressApp(fs, source, output))
	_, err = fs.Stat(output)
	require.ErrorIs(t, err, os.ErrNotExist, "missing source clears stale app output")
}

func TestCompressAppRejectsOverlap(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for _, paths := range [][2]string{
		{base, base},
		{base, filepath.Join(base, "child")},
		{filepath.Join(base, "child"), base},
	} {
		assert.Error(t, compressApp(afero.NewMemMapFs(), paths[0], paths[1]))
	}
}

func TestCompressAppErrors(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	source, output := filepath.Join(base, "raw"), filepath.Join(base, "packed")
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, source, []byte("not a directory"), 0o644))
	require.Error(t, compressApp(fs, source, output))
	require.NoError(t, fs.Remove(source))
	require.NoError(t, fs.MkdirAll(source, 0o755))
	assert.Error(t, compressApp(afero.NewReadOnlyFs(fs), source, output))
}
