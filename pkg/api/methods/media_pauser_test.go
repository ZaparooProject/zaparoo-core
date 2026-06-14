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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
)

func TestSyncMediaWorkPauserWithActiveMedia_ResumesWithoutPrimaryMedia(t *testing.T) {
	pauser := syncutil.NewPauser()
	pauser.Pause()

	syncMediaWorkPauserWithActiveMedia(nil, pauser)

	assert.False(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_ResumesForBackgroundSlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	pauser.Pause()
	media := models.NewActiveMedia("Audio", "Audio", "song.mp3", "Song", "native-audio")
	media.Slot = mediaslot.Background

	syncMediaWorkPauserWithActiveMedia(media, pauser)

	assert.False(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_PausesForPrimarySlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	syncMediaWorkPauserWithActiveMedia(media, pauser)

	assert.True(t, pauser.IsPaused())
}

func TestSyncMediaWorkPauserWithActiveMedia_PausesForInvalidSlot(t *testing.T) {
	pauser := syncutil.NewPauser()
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")
	media.Slot = "sideways"

	syncMediaWorkPauserWithActiveMedia(media, pauser)

	assert.True(t, pauser.IsPaused())
}
