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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncMediaWorkPauserWithActiveMedia_ResumesWithoutPrimaryMedia(t *testing.T) {
	pauser := syncutil.NewPauser()
	pauser.Pause()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	syncMediaWorkPauserWithActiveMedia(cfg, nil, pauser)

	assert.False(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_ResumesForBackgroundSlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	pauser.Pause()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia("Audio", "Audio", "song.mp3", "Song", "native-audio")
	media.Slot = mediaslot.Background

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.False(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_PausesForPrimarySlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia(systemdefs.SystemSaturn, systemdefs.SystemSaturn, "game.chd", "Game", "Saturn")

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.True(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_ThrottlesForPrimarySlotByDefault(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.False(t, pauser.IsPaused())
	assert.True(t, pauser.IsThrottled())
}

func TestSyncMediaWorkPauserWithActiveMedia_ThrottlesWithNilConfig(t *testing.T) {
	pauser := syncutil.NewPauser()
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	syncMediaWorkPauserWithActiveMedia(nil, media, pauser)

	assert.False(t, pauser.IsPaused())
	assert.True(t, pauser.IsThrottled())
}

func TestSyncMediaWorkPauserWithActiveMedia_PausesForInvalidSlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia(systemdefs.SystemSaturn, systemdefs.SystemSaturn, "game.chd", "Game", "Saturn")
	media.Slot = "sideways"

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.True(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_HeavyThrottleForStreamingSystem(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia(systemdefs.SystemPSX, systemdefs.SystemPSX, "game.chd", "Game", "PSX")

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.False(t, pauser.IsPaused())
	assert.True(t, pauser.IsThrottled())
	assert.Equal(t, syncutil.ThrottleHeavy, pauser.Level())
}

func TestSyncMediaWorkPauserWithActiveMedia_PausesByDefaultForWorstStreamingSystem(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia(systemdefs.SystemSaturn, systemdefs.SystemSaturn, "game.chd", "Game", "Saturn")

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.True(t, pauser.IsPaused())
	assert.False(t, pauser.IsThrottled())
}

func TestSyncMediaWorkPauserWithActiveMedia_LightThrottleForNonStreamingSystem(t *testing.T) {
	pauser := syncutil.NewPauser()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	syncMediaWorkPauserWithActiveMedia(cfg, media, pauser)

	assert.False(t, pauser.IsPaused())
	assert.True(t, pauser.IsThrottled())
	assert.Equal(t, syncutil.ThrottleLight, pauser.Level())
}
