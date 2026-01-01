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

package fixtures

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
)

// Common test media fixtures for use in tests

// NewRetroGame creates a sample retro game ActiveMedia entry
func NewRetroGame() *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
		LauncherID: "retroarch",
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		Path:       "/roms/nes/Super Mario Bros.zip",
		Name:       "Super Mario Bros.",
	}
}

// NewModernGame creates a sample modern game ActiveMedia entry
func NewModernGame() *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
		LauncherID: "steam",
		SystemID:   "pc",
		SystemName: "PC Games",
		Path:       "/games/steam/The Legend of Zelda Breath of the Wild.exe",
		Name:       "The Legend of Zelda: Breath of the Wild",
	}
}

// NewArcadeGame creates a sample arcade game ActiveMedia entry
func NewArcadeGame() *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Date(2025, 1, 15, 15, 0, 0, 0, time.UTC),
		LauncherID: "mame",
		SystemID:   "arcade",
		SystemName: "Arcade",
		Path:       "/roms/arcade/pacman.zip",
		Name:       "Pac-Man",
	}
}

// NewConsoleGame creates a sample console game ActiveMedia entry
func NewConsoleGame() *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Date(2025, 1, 15, 15, 30, 0, 0, time.UTC),
		LauncherID: "pcsx2",
		SystemID:   "ps2",
		SystemName: "PlayStation 2",
		Path:       "/roms/ps2/Final Fantasy X.iso",
		Name:       "Final Fantasy X",
	}
}

// NewHandheldGame creates a sample handheld game ActiveMedia entry
func NewHandheldGame() *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Date(2025, 1, 15, 16, 0, 0, 0, time.UTC),
		LauncherID: "visualboyadvance",
		SystemID:   "gba",
		SystemName: "Game Boy Advance",
		Path:       "/roms/gba/Pokemon Emerald.gba",
		Name:       "Pokémon Emerald",
	}
}

// NewCustomMedia creates an ActiveMedia entry with custom values
func NewCustomMedia(launcherID, systemID, systemName, path, name string) *models.ActiveMedia {
	return &models.ActiveMedia{
		Started:    time.Now(),
		LauncherID: launcherID,
		SystemID:   systemID,
		SystemName: systemName,
		Path:       path,
		Name:       name,
	}
}

// MediaCollection represents a set of related media entries for comprehensive testing
type MediaCollection struct {
	RetroGame    *models.ActiveMedia
	ModernGame   *models.ActiveMedia
	ArcadeGame   *models.ActiveMedia
	ConsoleGame  *models.ActiveMedia
	HandheldGame *models.ActiveMedia
}

// NewMediaCollection creates a complete set of test media entries
func NewMediaCollection() *MediaCollection {
	return &MediaCollection{
		RetroGame:    NewRetroGame(),
		ModernGame:   NewModernGame(),
		ArcadeGame:   NewArcadeGame(),
		ConsoleGame:  NewConsoleGame(),
		HandheldGame: NewHandheldGame(),
	}
}

// AllMedia returns all media entries in the collection as a slice
func (mc *MediaCollection) AllMedia() []*models.ActiveMedia {
	return []*models.ActiveMedia{
		mc.RetroGame,
		mc.ModernGame,
		mc.ArcadeGame,
		mc.ConsoleGame,
		mc.HandheldGame,
	}
}

// RetroMedia returns media entries for retro systems
func (mc *MediaCollection) RetroMedia() []*models.ActiveMedia {
	return []*models.ActiveMedia{
		mc.RetroGame,
		mc.ArcadeGame,
		mc.ConsoleGame,
		mc.HandheldGame,
	}
}

// SystemIDs returns a slice of all system IDs in the collection
func (mc *MediaCollection) SystemIDs() []string {
	return []string{
		mc.RetroGame.SystemID,
		mc.ModernGame.SystemID,
		mc.ArcadeGame.SystemID,
		mc.ConsoleGame.SystemID,
		mc.HandheldGame.SystemID,
	}
}

// LauncherIDs returns a slice of all launcher IDs in the collection
func (mc *MediaCollection) LauncherIDs() []string {
	return []string{
		mc.RetroGame.LauncherID,
		mc.ModernGame.LauncherID,
		mc.ArcadeGame.LauncherID,
		mc.ConsoleGame.LauncherID,
		mc.HandheldGame.LauncherID,
	}
}

// MediaBySystemID returns the media entry for a given system ID, or nil if not found
func (mc *MediaCollection) MediaBySystemID(systemID string) *models.ActiveMedia {
	for _, media := range mc.AllMedia() {
		if media.SystemID == systemID {
			return media
		}
	}
	return nil
}

// MediaByLauncherID returns the media entry for a given launcher ID, or nil if not found
func (mc *MediaCollection) MediaByLauncherID(launcherID string) *models.ActiveMedia {
	for _, media := range mc.AllMedia() {
		if media.LauncherID == launcherID {
			return media
		}
	}
	return nil
}

// SampleMedia returns a collection of sample media for testing
func SampleMedia() []*models.ActiveMedia {
	return NewMediaCollection().AllMedia()
}
