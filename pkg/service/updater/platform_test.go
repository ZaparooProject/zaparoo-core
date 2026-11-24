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

package updater

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingRemoveFS struct {
	afero.Fs
	err     error
	removes int
}

func (f *countingRemoveFS) Remove(path string) error {
	f.removes++
	if f.err != nil {
		return f.err
	}
	if err := f.Fs.Remove(path); err != nil {
		return fmt.Errorf("removing %q: %w", path, err)
	}
	return nil
}

// A Windows install under Program Files cannot be swapped by a process that
// refuses to run elevated. The swap is the last step of an install, long after
// the download and the database snapshot, so the guard has to answer before any
// of that is spent.
func TestPreflightPlatform_RefusesAnUnwritableWindowsInstall(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	dir := filepath.Join("Program Files", "Zaparoo")
	require.NoError(t, baseFS.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, "Zaparoo.exe")

	err := preflightPlatform(afero.NewReadOnlyFs(baseFS), "windows", target)

	require.ErrorIs(t, err, ErrPlatformUnsupported)
	assert.Contains(t, err.Error(), "Windows installer",
		"the message has to say what to do instead")
	assert.Contains(t, err.Error(), dir,
		"the message has to name the directory that could not be written to")
}

func TestPreflightPlatform_AllowsAWritableWindowsInstall(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("apps", "zaparoo")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, "Zaparoo.exe")
	assert.NoError(t, preflightPlatform(fs, "windows", target))
}

func TestPreflightPlatform_RefusesWindowsTargetWithoutRenameAccess(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("apps", "zaparoo")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, "Zaparoo.exe")
	renameErr := errors.New("target ACL denies delete")

	err := preflightPlatformWith(fs, "windows", target, func(got string) error {
		assert.Equal(t, target, got)
		return renameErr
	})

	require.ErrorIs(t, err, ErrPlatformUnsupported)
	assert.Contains(t, err.Error(), target)
	assert.Contains(t, err.Error(), "Windows installer")
}

// Apply resolves the binary itself and reports that failure with its own
// message. Refusing here would tell the user their platform is unsupported when
// the real problem is that this build could not find its own executable.
func TestPreflightPlatform_DefersAnUnresolvableBinaryToApply(t *testing.T) {
	t.Parallel()

	assert.NoError(t, preflightPlatform(afero.NewMemMapFs(), "windows", ""))
}

func TestPreflightPlatform_AllowsAWritableInstallOnEveryPlatform(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		fs := afero.NewMemMapFs()
		dir := filepath.Join("opt", "zaparoo")
		require.NoError(t, fs.MkdirAll(dir, 0o755))
		assert.NoError(t, preflightPlatform(fs, goos, filepath.Join(dir, "zaparoo")), goos)
	}
}

// A distribution or Nix package puts Core somewhere read-only. Every install
// writes its candidate and backup binaries beside the target, so without this
// the install would download, snapshot the database and only then fail.
func TestPreflightPlatform_RefusesAReadOnlyInstallOffWindows(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin"} {
		baseFS := afero.NewMemMapFs()
		dir := filepath.Join("nix", "store", "abc-zaparoo", "bin")
		require.NoError(t, baseFS.MkdirAll(dir, 0o755))

		err := preflightPlatform(afero.NewReadOnlyFs(baseFS), goos, filepath.Join(dir, "zaparoo"))

		require.ErrorIs(t, err, ErrPlatformUnsupported, goos)
		assert.Contains(t, err.Error(), dir, goos)
		assert.Contains(t, err.Error(), "the same way it was installed", goos)
		assert.NotContains(t, err.Error(), "Windows", goos)
	}
}

// Off Windows the swap is a plain rename over the target, which only needs the
// directory, so the Windows-only rename probe must not be consulted.
func TestPreflightPlatform_SkipsTheTargetRenameProbeOffWindows(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("opt", "zaparoo")
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	err := preflightPlatformWith(fs, "linux", filepath.Join(dir, "zaparoo"), func(string) error {
		return errors.New("rename probe must not run off Windows")
	})

	assert.NoError(t, err)
}

func TestCheckInstallDirWritable_LeavesNothingBehind(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := "install"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, checkInstallDirWritable(fs, dir))

	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the probe file has to be cleaned up")
}

func TestCheckInstallDirWritable_RejectsAnUndeletableProbe(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	dir := "install"
	require.NoError(t, baseFS.MkdirAll(dir, 0o755))
	removeErr := errors.New("delete denied")

	fs := &countingRemoveFS{Fs: baseFS, err: removeErr}
	err := checkInstallDirWritable(fs, dir)

	require.ErrorIs(t, err, removeErr)
	entries, readErr := afero.ReadDir(baseFS, dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Name(), ".zaparoo-update-probe-")
}

func TestPlatformPreflightCache_ProbesEachInstallPathOnce(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	dir := "install"
	require.NoError(t, baseFS.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, "Zaparoo.exe")
	fs := &countingRemoveFS{Fs: baseFS}
	cache := &platformPreflightCache{}

	require.NoError(t, cache.check(fs, "windows", target))
	require.NoError(t, cache.check(fs, "windows", target))
	assert.Equal(t, 1, fs.removes)

	secondTarget := filepath.Join(dir, "Zaparoo-2.exe")
	require.NoError(t, cache.check(fs, "windows", secondTarget))
	assert.Equal(t, 2, fs.removes, "a different install path needs its own probe")
}

func TestPlatformPreflightCache_CachesFailures(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	dir := "install"
	require.NoError(t, baseFS.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, "Zaparoo.exe")
	removeErr := errors.New("delete denied")
	fs := &countingRemoveFS{Fs: baseFS, err: removeErr}
	cache := &platformPreflightCache{}

	firstErr := cache.check(fs, "windows", target)
	secondErr := cache.check(fs, "windows", target)
	require.ErrorIs(t, firstErr, ErrPlatformUnsupported)
	require.ErrorIs(t, secondErr, ErrPlatformUnsupported)
	assert.Equal(t, firstErr.Error(), secondErr.Error())
	assert.Equal(t, 1, fs.removes)
}
