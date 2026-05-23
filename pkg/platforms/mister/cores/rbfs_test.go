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

package cores

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShallowScanRBF_IncludesRetroAchievementsCores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	require.NoError(t, os.MkdirAll(raCoreDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(raCoreDir, "NES.rbf"), []byte{}, 0o600))

	rbfs, err := shallowScanRBFAt(root)
	require.NoError(t, err)

	expectedMglName := filepath.Join("_RA_Cores", "Cores", "NES")
	var found *RBFInfo
	for i := range rbfs {
		if rbfs[i].MglName == expectedMglName {
			found = &rbfs[i]
			break
		}
	}

	require.NotNil(t, found, "RA core should be included in shallow RBF scan")
	assert.Equal(t, "NES", found.ShortName)
	assert.Equal(t, "NES.rbf", found.Filename)
}
