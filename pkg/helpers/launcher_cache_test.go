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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLauncherByID_Found(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "launcher-a", SystemID: "NES"},
		{ID: "launcher-b", SystemID: "SNES"},
	})

	launcher := cache.GetLauncherByID("launcher-b")
	require.NotNil(t, launcher)
	assert.Equal(t, "launcher-b", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
}

func TestGetLauncherByID_NotFound(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "launcher-a", SystemID: "NES"},
	})

	launcher := cache.GetLauncherByID("nonexistent")
	assert.Nil(t, launcher)
}

func TestGetLauncherByID_EmptyCache(t *testing.T) {
	t.Parallel()

	cache := &LauncherCache{}
	launcher := cache.GetLauncherByID("anything")
	assert.Nil(t, launcher)
}
