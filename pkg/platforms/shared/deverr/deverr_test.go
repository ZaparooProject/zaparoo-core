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

package deverr

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDevErrSystemLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewDevErrSystemLauncher()

	assert.Equal(t, "DevErrSystem", launcher.ID)
	assert.Equal(t, systemdefs.SystemDevErr, launcher.SystemID)
	assert.Equal(t, []string{".deverr"}, launcher.Extensions)
	assert.Equal(t, []string{"deverr"}, launcher.Folders)
	assert.NotNil(t, launcher.Launch)
}

func TestNewDevErrAnyLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewDevErrAnyLauncher()

	assert.Equal(t, "DevErrAny", launcher.ID)
	assert.Empty(t, launcher.SystemID)
	assert.True(t, launcher.SkipFilesystemScan)
	assert.Equal(t, []string{".deverr"}, launcher.Extensions)
	assert.NotNil(t, launcher.Launch)
	require.NotNil(t, launcher.Scanner)

	results, err := launcher.Scanner(context.Background(), nil, systemdefs.SystemDevErr, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, platforms.ScanResult{
		Path: "deverr://Any System Scanner - DevErr Result (USA) (!).deverr",
		Name: "Any System Scanner - DevErr Result (USA) (!)",
	}, results[0])

	seed := []platforms.ScanResult{{Path: "existing://game", Name: "Existing Game"}}
	unchanged, err := launcher.Scanner(context.Background(), nil, systemdefs.SystemNES, seed)
	require.NoError(t, err)
	assert.Equal(t, seed, unchanged)
}

func TestDevErrLaunchFn(t *testing.T) {
	t.Parallel()

	proc, err := deverrLaunchFn(nil, "deverr://Example.deverr", nil)
	require.Nil(t, proc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deverr://Example.deverr")
}
