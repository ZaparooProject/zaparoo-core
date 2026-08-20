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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Windows install under Program Files cannot be swapped by a process that
// refuses to run elevated. The swap is the last step of an install, long after
// the download and the database snapshot, so the guard has to answer before any
// of that is spent.
func TestPreflightPlatform_RefusesAnUnwritableWindowsInstall(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "no-such-dir", "Zaparoo.exe")
	err := preflightPlatform("windows", missing)

	require.ErrorIs(t, err, ErrPlatformUnsupported)
	assert.Contains(t, err.Error(), "Windows installer",
		"the message has to say what to do instead")
	assert.Contains(t, err.Error(), filepath.Dir(missing),
		"the message has to name the directory that could not be written to")
}

func TestPreflightPlatform_AllowsAWritableWindowsInstall(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "Zaparoo.exe")
	assert.NoError(t, preflightPlatform("windows", target))
}

// Apply resolves the binary itself and reports that failure with its own
// message. Refusing here would tell the user their platform is unsupported when
// the real problem is that this build could not find its own executable.
func TestPreflightPlatform_DefersAnUnresolvableBinaryToApply(t *testing.T) {
	t.Parallel()

	assert.NoError(t, preflightPlatform("windows", ""))
}

func TestPreflightPlatform_AllowsPlatformsThatReplaceTheirOwnBinary(t *testing.T) {
	t.Parallel()

	// The path is never looked at off Windows: a single rename over the running
	// binary needs nothing the rest of the install did not already need.
	missing := filepath.Join(t.TempDir(), "no-such-dir", "zaparoo")
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		assert.NoError(t, preflightPlatform(goos, missing), goos)
	}
}

func TestCheckInstallDirWritable_LeavesNothingBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, checkInstallDirWritable(dir))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the probe file has to be cleaned up")
}
