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

package mistermain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCoreNameEmptyIsTemporarilyUnavailable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "CORENAME")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	name, err := parseCoreNameFile(path)

	assert.Empty(t, name)
	require.ErrorIs(t, err, errCoreNameUnavailable)
}

func TestReadCoreNamePreservesReadFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "CORENAME")

	name, err := parseCoreNameFile(path)

	assert.Empty(t, name)
	require.Error(t, err)
	require.NotErrorIs(t, err, errCoreNameUnavailable)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadCoreNameTrimsName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "CORENAME")
	require.NoError(t, os.WriteFile(path, []byte("  SNES  \n"), 0o600))

	name, err := parseCoreNameFile(path)

	require.NoError(t, err)
	assert.Equal(t, "SNES", name)
}
