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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cannot run in parallel with each other: they swap the
// package-level mountInfoPath var to point at a fixture.

// useMountInfoFixture points mountInfoPath at a fake mount table for the
// duration of the test. The real /proc/self/mountinfo is deliberately not read:
// results would depend on how the host running the tests happens to be
// partitioned, and procfs is a platform boundary.
func useMountInfoFixture(t *testing.T, data string) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	require.NoError(t, os.WriteFile(tmp, []byte(data), 0o600))
	orig := mountInfoPath
	mountInfoPath = tmp
	t.Cleanup(func() { mountInfoPath = orig })
}

const mountInfoFixture = "1 0 0:1 / / rw - ext4 /dev/root rw\n" +
	"2 1 0:2 / /media rw - exfat /dev/sda1 rw\n" +
	"3 2 0:3 / /media/fat rw - vfat /dev/sdb1 rw\n" +
	"4 2 0:4 / /media/fatty rw - btrfs /dev/sdc1 rw\n"

func TestStorageInfoForPath(t *testing.T) {
	useMountInfoFixture(t, mountInfoFixture)

	info, ok := StorageInfoForPath("/media/fat/zaparoo/media.db")
	require.True(t, ok)
	assert.Equal(t, "vfat", info.FSType)
	assert.Equal(t, "/media/fat", info.Mountpoint)
	assert.Equal(t, "/dev/sdb1", info.Source)
	assert.Equal(t, "rw", info.Options)
}

func TestStorageInfoForPath_LongestMountWins(t *testing.T) {
	useMountInfoFixture(t, mountInfoFixture)

	info, ok := StorageInfoForPath("/media/fat/zaparoo/media.db")
	require.True(t, ok)
	assert.Equal(t, "/media/fat", info.Mountpoint)

	info, ok = StorageInfoForPath("/media/other")
	require.True(t, ok)
	assert.Equal(t, "exfat", info.FSType)
	assert.Equal(t, "/media", info.Mountpoint)

	info, ok = StorageInfoForPath("/somewhere/else")
	require.True(t, ok, "the root mount owns everything not under a longer one")
	assert.Equal(t, "/", info.Mountpoint)
}

// TestStorageInfoForPath_MatchesWholePathComponents guards the case a plain
// strings.HasPrefix gets wrong.
//
// The fixture here deliberately does NOT mount /media/fatty: with it mounted,
// the longest-match tiebreak picks it anyway and a broken prefix test still
// passes. The bug only shows when the similarly-named directory is not itself a
// mountpoint, so /media/fatty/... must fall through to /media rather than being
// captured by /media/fat.
func TestStorageInfoForPath_MatchesWholePathComponents(t *testing.T) {
	useMountInfoFixture(t, "1 0 0:1 / / rw - ext4 /dev/root rw\n"+
		"2 1 0:2 / /media rw - exfat /dev/sda1 rw\n"+
		"3 2 0:3 / /media/fat rw - vfat /dev/sdb1 rw\n")

	info, ok := StorageInfoForPath("/media/fatty/zaparoo/media.db")
	require.True(t, ok)
	assert.Equal(t, "/media", info.Mountpoint, "/media/fatty must not be captured by the /media/fat mount")
	assert.Equal(t, "exfat", info.FSType)

	// The mountpoint itself, with no trailing component, still resolves to it.
	info, ok = StorageInfoForPath("/media/fat")
	require.True(t, ok)
	assert.Equal(t, "vfat", info.FSType)
	assert.Equal(t, "/media/fat", info.Mountpoint)
}

func TestStorageInfoForPath_UnreadableMountInfo(t *testing.T) {
	orig := mountInfoPath
	mountInfoPath = filepath.Join(t.TempDir(), "does-not-exist")
	defer func() { mountInfoPath = orig }()

	_, ok := StorageInfoForPath("/anywhere")
	assert.False(t, ok)
}
