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

package mediascanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatWithContext_Success(t *testing.T) {
	dir := t.TempDir()
	info, err := statWithContext(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestStatWithContext_File(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello"), 0o600))

	info, err := statWithContext(context.Background(), f)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
	assert.Equal(t, "test.txt", info.Name())
}

func TestStatWithContext_NotExist(t *testing.T) {
	_, err := statWithContext(context.Background(), "/nonexistent/path/should/not/exist")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestStatWithContext_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := statWithContext(ctx, t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReadDirWithContext_Success(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600))

	entries, err := readDirWithContext(context.Background(), dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	assert.Contains(t, names, "a.txt")
	assert.Contains(t, names, "b.txt")
}

func TestReadDirWithContext_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := readDirWithContext(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestReadDirWithContext_NotExist(t *testing.T) {
	_, err := readDirWithContext(context.Background(), "/nonexistent/path/should/not/exist")
	require.Error(t, err)
}

func TestReadDirWithContext_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := readDirWithContext(ctx, t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestLstatWithContext_Success(t *testing.T) {
	dir := t.TempDir()
	info, err := lstatWithContext(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLstatWithContext_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(target, 0o750))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))

	info, err := lstatWithContext(context.Background(), link)
	require.NoError(t, err)
	assert.NotEqual(t, 0, info.Mode()&os.ModeSymlink, "should report symlink mode")
}

func TestLstatWithContext_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lstatWithContext(ctx, t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEvalSymlinksWithContext_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(target, 0o750))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))

	resolved, err := evalSymlinksWithContext(context.Background(), link)
	require.NoError(t, err)
	// EvalSymlinks resolves all symlinks in the path, including those in
	// the temp dir itself (e.g. /var -> /private/var on macOS).
	expected, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	assert.Equal(t, expected, resolved)
}

func TestEvalSymlinksWithContext_NoSymlink(t *testing.T) {
	dir := t.TempDir()
	resolved, err := evalSymlinksWithContext(context.Background(), dir)
	require.NoError(t, err)
	// EvalSymlinks resolves all symlinks in the path, including those in
	// the temp dir itself (e.g. /var -> /private/var on macOS).
	expected, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, expected, resolved)
}

func TestEvalSymlinksWithContext_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := evalSymlinksWithContext(ctx, t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
