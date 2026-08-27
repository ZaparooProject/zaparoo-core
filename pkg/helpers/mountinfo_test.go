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

package helpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three tests below cannot run in parallel with each other: the latter
// two swap the package-level mountInfoPath var to point at a fake table,
// which would race a concurrent StorageInfoForPath call in this first test
// reading the real /proc/self/mountinfo path.
func TestStorageInfoForPath(t *testing.T) {
	// A real system's /proc/self/mountinfo always has at least a root entry,
	// so any absolute path resolves to *some* mount — this exercises the
	// longest-prefix-match logic against the live mount table without faking
	// procfs, and cross-checks it against a stat-based sanity bound (the
	// resolved mountpoint must actually be an ancestor directory of path).
	dir := t.TempDir()
	info, ok := StorageInfoForPath(dir)
	require.True(t, ok, "expected some mount to own %s", dir)
	assert.NotEmpty(t, info.FSType)

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(absDir, info.Mountpoint),
		"mountpoint %q should be a prefix of %q", info.Mountpoint, absDir)
}

func TestStorageInfoForPath_LongestMountWins(t *testing.T) {
	orig := mountInfoPath
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	data := "1 0 0:1 / / rw - ext4 /dev/root rw\n" +
		"2 1 0:2 / /media rw - exfat /dev/sda1 rw\n" +
		"3 2 0:3 / /media/fat rw - vfat /dev/sdb1 rw\n"
	require.NoError(t, os.WriteFile(tmp, []byte(data), 0o600))
	mountInfoPath = tmp
	defer func() { mountInfoPath = orig }()

	info, ok := StorageInfoForPath("/media/fat/zaparoo/media.db")
	require.True(t, ok)
	assert.Equal(t, "vfat", info.FSType)
	assert.Equal(t, "/media/fat", info.Mountpoint)
	assert.Equal(t, "/dev/sdb1", info.Source)

	info, ok = StorageInfoForPath("/media/other")
	require.True(t, ok)
	assert.Equal(t, "exfat", info.FSType)
	assert.Equal(t, "/media", info.Mountpoint)
}

func TestStorageInfoForPath_UnreadableMountInfo(t *testing.T) {
	orig := mountInfoPath
	mountInfoPath = filepath.Join(t.TempDir(), "does-not-exist")
	defer func() { mountInfoPath = orig }()

	_, ok := StorageInfoForPath("/anywhere")
	assert.False(t, ok)
}
