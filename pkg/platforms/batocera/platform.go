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

package batocera

import (
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
)

const (
	// HomeDir is hardcoded because home in env is not set at the point which
	// the service file is called to start.
	HomeDir   = "/userdata/system"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
	LogDir    = DataDir + "/" + config.LogsDir
	// ESConfigDir is the EmulationStation config directory on Batocera.
	ESConfigDir = "/userdata/system/configs/emulationstation"
	// ROMRoot is Batocera's default ROM directory.
	ROMRoot = "/userdata/roms"
	// packageHookFile is created by the Batocera pacman package's install.
	packageHookFile = "/userdata/system/pacman/batoexec/zaparoo-core_0"
)

// Platform is Batocera.
type Platform struct {
	*esbase.Platform
}

// NewPlatform returns the Batocera platform.
func NewPlatform() *Platform {
	return &Platform{Platform: esbase.New(Options())}
}

// Options describes Batocera to the shared EmulationStation platform.
func Options() *esbase.Options {
	return &esbase.Options{
		ID:             platformids.Batocera,
		DataDir:        DataDir,
		ConfigDir:      ConfigDir,
		LogDir:         LogDir,
		DefaultROMRoot: ROMRoot,
		ESConfigDirs:   []string{ESConfigDir},
		// Batocera's EmulationStation has no doom system.
		ExcludedSystems: map[string]struct{}{
			"doom": {},
		},
		UpdatePayload:           UpdatePayload,
		ManagedByPackageManager: managedByPackageManager,
		ExitGame:                esbase.HotkeygenExit,
		Controls:                esbase.HotkeygenControls,
		ControlNames:            esbase.HotkeygenControlNames,
	}
}

// managedByPackageManager checks if this install was done via the Batocera
// pacman package by looking for the batoexec hook file created during install.
func managedByPackageManager() bool {
	_, err := os.Stat(packageHookFile)
	return err == nil
}
