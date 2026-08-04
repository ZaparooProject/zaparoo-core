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

package mister

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTY2OLEDPictureNameFromTrackedArcade(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	media := &models.ActiveMedia{
		SystemID: systemdefs.SystemArcade,
		Path:     "bubbles",
		Name:     "Bubbles",
	}

	assert.Equal(t, "bubbles", platform.TTY2OLEDPictureName(media))
}

func TestTTY2OLEDPictureNameFromMRA(t *testing.T) {
	t.Parallel()

	mraPath := filepath.Join(t.TempDir(), "Street Fighter II.mra")
	mra := []byte(`<misterromdescription>
	<setname>sf2</setname>
	<name>Street Fighter II - The World Warrior</name>
</misterromdescription>`)
	require.NoError(t, os.WriteFile(mraPath, mra, 0o600))
	platform := &Platform{}
	media := &models.ActiveMedia{
		SystemID: systemdefs.SystemArcade,
		Path:     mraPath,
		Name:     "Street Fighter II - The World Warrior",
	}

	assert.Equal(t, "sf2", platform.TTY2OLEDPictureName(media))
}

func TestTTY2OLEDPictureNameIgnoresNonArcadeMedia(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	media := &models.ActiveMedia{
		SystemID: systemdefs.SystemNES,
		Path:     "bubbles",
	}

	assert.Empty(t, platform.TTY2OLEDPictureName(media))
}

func TestTTY2OLEDPictureNameReturnsEmptyForMissingMediaPath(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	media := &models.ActiveMedia{SystemID: systemdefs.SystemArcade}

	assert.Empty(t, platform.TTY2OLEDPictureName(media))
}

func TestTTY2OLEDPictureNameReturnsEmptyWhenMRAIsUnreadable(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	media := &models.ActiveMedia{
		SystemID: systemdefs.SystemArcade,
		Path:     filepath.Join(t.TempDir(), "missing.mra"),
	}

	assert.Empty(t, platform.TTY2OLEDPictureName(media))
}

func TestTTY2OLEDPictureNameRejectsArcadeFilePathWithoutSetname(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	media := &models.ActiveMedia{
		SystemID: systemdefs.SystemArcade,
		Path:     filepath.Join("media", "fat", "_Arcade", "Bubbles.mgl"),
	}

	assert.Empty(t, platform.TTY2OLEDPictureName(media))
}
