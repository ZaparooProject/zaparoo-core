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

package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func driveToken(root string) tokens.Token {
	return tokens.Token{
		Type:     "externaldrive",
		Text:     "game.sfc",
		ScanTime: time.Now(),
		PathRoot: root,
		Metadata: &tokens.Metadata{
			Name:        "Drive Game",
			Description: "A game on a stick.",
			ImagePath:   filepath.Join(root, "cover.png"),
		},
	}
}

func newMetadataState(t *testing.T) *State {
	t.Helper()
	st, ns := NewState(nil, "test-boot")
	drainState(t, st, ns)
	return st
}

func TestTokenMetadataLabelsMediaOnTheDrive(t *testing.T) {
	t.Parallel()
	st := newMetadataState(t)
	root := filepath.Join(t.TempDir(), "usb")
	st.SetActiveCard(driveToken(root))

	st.SetActiveMedia(models.NewActiveMedia(
		"SNES", "Super Nintendo", filepath.Join(root, "games", "game.sfc"), "game", "SNES"))

	media := st.ActiveMedia()
	require.NotNil(t, media)
	assert.Equal(t, "Drive Game", media.Name)
	assert.Equal(t, "A game on a stick.", media.Description)
	assert.Equal(t, filepath.Join(root, "cover.png"), media.ImagePath)
}

func TestTokenMetadataKeepsLaunchedNameWhenNoneDeclared(t *testing.T) {
	t.Parallel()
	st := newMetadataState(t)
	root := filepath.Join(t.TempDir(), "usb")
	token := driveToken(root)
	token.Metadata = &tokens.Metadata{Description: "Only a description."}
	st.SetActiveCard(token)

	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", filepath.Join(root, "game.sfc"), "game", "SNES"))

	media := st.ActiveMedia()
	require.NotNil(t, media)
	assert.Equal(t, "game", media.Name)
	assert.Equal(t, "Only a description.", media.Description)
	assert.Empty(t, media.ImagePath)
}

func TestTokenMetadataIgnoredOffTheDrive(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := filepath.Join(base, "usb")
	tests := []struct {
		name string
		path string
	}{
		{name: "library game", path: filepath.Join(base, "library", "snes", "game.sfc")},
		{name: "sibling with the root as a name prefix", path: filepath.Join(base, "usb2", "game.sfc")},
		{name: "non-file path", path: "kodi-movie://42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := newMetadataState(t)
			st.SetActiveCard(driveToken(root))

			st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", tt.path, "game", "SNES"))

			media := st.ActiveMedia()
			require.NotNil(t, media)
			assert.Equal(t, "game", media.Name)
			assert.Empty(t, media.Description)
			assert.Empty(t, media.ImagePath)
		})
	}
}

func TestTokenMetadataNeedsAPathRoot(t *testing.T) {
	t.Parallel()
	st := newMetadataState(t)
	token := driveToken("")
	st.SetActiveCard(token)

	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", "/media/usb/game.sfc", "game", "SNES"))

	media := st.ActiveMedia()
	require.NotNil(t, media)
	assert.Equal(t, "game", media.Name)
	assert.Empty(t, media.Description)
}

func TestTokenMetadataClearedWhenMediaChanges(t *testing.T) {
	t.Parallel()
	st := newMetadataState(t)
	base := t.TempDir()
	root := filepath.Join(base, "usb")
	st.SetActiveCard(driveToken(root))
	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", filepath.Join(root, "game.sfc"), "game", "SNES"))
	require.Equal(t, "Drive Game", st.ActiveMedia().Name)

	st.SetActiveMedia(models.NewActiveMedia(
		"SNES", "Super Nintendo", filepath.Join(base, "library", "other.sfc"), "other", "SNES"))

	media := st.ActiveMedia()
	require.NotNil(t, media)
	assert.Equal(t, "other", media.Name)
	assert.Empty(t, media.Description)
	assert.Empty(t, media.ImagePath)
}

func TestTokenMetadataSurvivesRepublishAfterRemoval(t *testing.T) {
	t.Parallel()
	st := newMetadataState(t)
	root := filepath.Join(t.TempDir(), "usb")
	game := filepath.Join(root, "game.sfc")
	st.SetActiveCard(driveToken(root))
	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", game, "game", "SNES"))

	// In tap mode the drive can be pulled while the game runs, and a
	// tracker may report the same game again afterwards.
	st.SetActiveCard(tokens.Token{})
	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", game, "game", "SNES"))

	media := st.ActiveMedia()
	require.NotNil(t, media)
	assert.Equal(t, "Drive Game", media.Name)
	assert.Equal(t, "A game on a stick.", media.Description)
}
