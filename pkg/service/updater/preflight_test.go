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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testArchiveSize = 4 * bytesPerMB
	testBinarySize  = 6 * bytesPerMB
	testDBSize      = 2 * bytesPerMB
)

// testRequiredSpace is the formula spelled out independently of the
// implementation, so a change to either has to be deliberate.
const testRequiredSpace = 2*testArchiveSize + 2*testBinarySize + testDBSize

type spaceFixture struct {
	err   error
	needs *spaceNeeds
	dirs  []string
	free  uint64
}

// newSpaceFixture lays out a target binary and a user database on one volume,
// with the staging root under the data directory the way a real install has it.
func newSpaceFixture(t *testing.T) *spaceFixture {
	t.Helper()

	dir := t.TempDir()
	f := &spaceFixture{free: testRequiredSpace}
	f.needs = &spaceNeeds{
		archiveSize: testArchiveSize,
		targetPath:  writeSizedFile(t, filepath.Join(dir, "zaparoo"), testBinarySize),
		userDBPath:  writeSizedFile(t, filepath.Join(dir, "user.db"), testDBSize),
		stagingRoot: filepath.Join(dir, "updater", "staging"),
		free: func(path string) (uint64, error) {
			f.dirs = append(f.dirs, path)
			return f.free, f.err
		},
	}
	return f
}

func writeSizedFile(t *testing.T, path string, size int64) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte{'x'}, int(size)), 0o600))
	return path
}

func TestCheckFreeSpace_AcceptsExactlyEnough(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	f.free = testRequiredSpace

	require.NoError(t, checkFreeSpace(f.needs))
}

// One byte short is the interesting boundary: it proves the requirement is the
// sum of every part, not just the download.
func TestCheckFreeSpace_RejectsOneByteShort(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	f.free = testRequiredSpace - 1

	err := checkFreeSpace(f.needs)
	require.ErrorIs(t, err, ErrInsufficientSpace)
	assert.Contains(t, err.Error(), filepath.Dir(f.needs.targetPath),
		"the message has to name the directory that is full")
	assert.Contains(t, err.Error(), "need at least 22 MB")
}

func TestCheckFreeSpace_LeavesMissingFilesOutOfTheRequirement(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	require.NoError(t, os.Remove(f.needs.targetPath))
	require.NoError(t, os.Remove(f.needs.userDBPath))
	f.free = 2 * testArchiveSize

	require.NoError(t, checkFreeSpace(f.needs),
		"a first install has no binary or database to preserve")
}

// The three paths are one filesystem on MiSTer, where the data directory sits
// beside the install target. Charging the requirement to it three times would
// still be correct, but asking the kernel three times would not be.
func TestCheckFreeSpace_ChecksOneVolumeOnce(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)

	require.NoError(t, checkFreeSpace(f.needs))
	assert.Equal(t, []string{filepath.Dir(f.needs.targetPath)}, f.dirs)
}

func TestCheckFreeSpace_ChecksEverySeparateVolume(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	elsewhere := t.TempDir()
	f.needs.stagingRoot = elsewhere

	require.NoError(t, checkFreeSpace(f.needs))
	assert.ElementsMatch(t, []string{elsewhere, filepath.Dir(f.needs.targetPath)}, f.dirs)
}

// The staging root does not exist until Stage creates it, and neither does the
// snapshot directory, so the check has to ask about an ancestor that does.
func TestCheckFreeSpace_WalksUpToAnExistingDirectory(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	require.NoDirExists(t, f.needs.stagingRoot)

	require.NoError(t, checkFreeSpace(f.needs))
	require.Len(t, f.dirs, 1)
	assert.Equal(t, filepath.Dir(f.needs.targetPath), f.dirs[0])
}

// vfat and exFAT already make us fall back on directory fsync. A filesystem that
// will not report its free space must not be the reason a device can never
// update; ENOSPC during the install is still handled safely.
func TestCheckFreeSpace_UnknownFreeSpaceDoesNotBlock(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	f.err = errors.New("statfs is not supported here")
	f.free = 0

	require.NoError(t, checkFreeSpace(f.needs))
	assert.NotEmpty(t, f.dirs)
}

func TestCheckFreeSpace_SkipsWhenTheManifestDeclaresNoSize(t *testing.T) {
	t.Parallel()

	f := newSpaceFixture(t)
	f.needs.archiveSize = 0
	f.free = 0

	require.NoError(t, checkFreeSpace(f.needs),
		"downloadArchive rejects a sizeless asset with a better error")
	assert.Empty(t, f.dirs)
}

func TestNearestExistingDir_StopsAtTheRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.Equal(t, dir, nearestExistingDir(filepath.Join(dir, "a", "b", "c")))
}

// A release with nothing for this device is Stage's error to report, with the
// platform and architecture in it. Reporting it here too would surface the
// wrong one first.
func TestPreflightSpace_LeavesAssetSelectionToStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	release := testRelease("v99.0.0", &otameta.Asset{
		Name: "zaparoo-somethingelse_mips-99.0.0.zip",
		Size: 1,
	})

	err := preflightSpace(
		&Options{PlatformID: testStagePlatform, DataDir: dir},
		release,
		filepath.Join(dir, "zaparoo"),
		filepath.Join(dir, "updater", "staging"),
	)
	require.NoError(t, err)
}
