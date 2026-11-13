//go:build linux && !android

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

package knulli

import (
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
)

const (
	// HomeDir is hardcoded because HOME is not set when the service starts.
	HomeDir   = "/userdata/system"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
	LogDir    = DataDir + "/" + config.LogsDir
	// SystemESConfigDir holds the stock es_systems.cfg.
	SystemESConfigDir = "/usr/share/emulationstation"
	// ESConfigDir holds the user's es_systems_*.cfg overlays.
	ESConfigDir = "/userdata/system/configs/emulationstation"
	// KodiDir is where Batocera-based systems install Kodi. Most Knulli
	// devices ship without it.
	KodiDir = "/usr/lib/kodi"
	// ROMRoot is Knulli's default ROM directory.
	ROMRoot = "/userdata/roms"

	killRetryDelay = 2 * time.Second
)

// Platform is Knulli, a Batocera derivative for handhelds.
type Platform struct {
	*esbase.Platform
}

// NewPlatform returns the Knulli platform.
func NewPlatform() *Platform {
	return &Platform{Platform: esbase.New(Options())}
}

// Options describes Knulli to the shared EmulationStation platform. Knulli
// removes Batocera's hotkeygen and its emulator launcher never sets a
// hotkeygen context, so a stop goes straight to /emukill and games offer
// only the stop control.
func Options() *esbase.Options {
	return &esbase.Options{
		ID:             platformids.Knulli,
		DataDir:        DataDir,
		ConfigDir:      ConfigDir,
		LogDir:         LogDir,
		DefaultROMRoot: ROMRoot,
		ESConfigDirs:   []string{SystemESConfigDir, ESConfigDir},
		ExcludedSystems: map[string]struct{}{
			"doom": {},
		},
		// The system list differs per device, so folders are checked
		// against the systems ES actually has.
		OnlyESSystems: true,
		SystemAliases: map[string]string{
			"segacd": "megacd",
		},
		HasKodi: hasKodi,
		// knulli-es-swissknife --emukill waits on each emulator process,
		// so attempts are spaced to keep them from piling up.
		KillRetryDelay: killRetryDelay,
	}
}

func hasKodi() bool {
	_, err := os.Stat(KodiDir)
	return err == nil
}
