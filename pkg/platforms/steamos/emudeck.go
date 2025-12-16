//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package steamos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/gamescope"
	"github.com/rs/zerolog/log"
)

// EmulatorType represents the type of emulator used to launch games.
type EmulatorType string

const (
	// EmulatorRetroArch uses RetroArch with a specific libretro core
	EmulatorRetroArch EmulatorType = "retroarch"
	// EmulatorStandalone uses a standalone emulator application
	EmulatorStandalone EmulatorType = "standalone"
)

// EmulatorConfig defines how to launch games for a specific system.
type EmulatorConfig struct {
	// Type is the emulator type (retroarch or standalone)
	Type EmulatorType
	// FlatpakID is the Flatpak application ID (if installed via Flatpak)
	FlatpakID string
	// Core is the libretro core name (for RetroArch)
	Core string
	// Args are additional command-line arguments
	Args []string
}

// EmuDeckPaths holds the paths for EmuDeck installation.
type EmuDeckPaths struct {
	// RomsPath is the base path for ROMs (e.g., ~/Emulation/roms/)
	RomsPath string
	// GamelistPath is the base path for ES-DE gamelists (e.g., ~/ES-DE/gamelists/)
	GamelistPath string
}

// DefaultEmuDeckPaths returns the default paths for EmuDeck.
func DefaultEmuDeckPaths() EmuDeckPaths {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
	}

	return EmuDeckPaths{
		RomsPath:     filepath.Join(homeDir, "Emulation", "roms"),
		GamelistPath: filepath.Join(homeDir, "ES-DE", "gamelists"),
	}
}

// emulatorMapping maps ES system folders to emulator configurations.
// These are the default emulators that EmuDeck installs for each system.
//
//nolint:gochecknoglobals // Package-level configuration
var emulatorMapping = map[string]EmulatorConfig{
	// Nintendo Systems - RetroArch cores
	"nes": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "mesen_libretro",
	},
	"snes": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "snes9x_libretro",
	},
	"gb": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "gambatte_libretro",
	},
	"gbc": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "gambatte_libretro",
	},
	"gba": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "mgba_libretro",
	},
	"n64": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "mupen64plus_next_libretro",
	},
	"nds": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "melonds_libretro",
	},
	"fds": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "mesen_libretro",
	},
	"virtualboy": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_vb_libretro",
	},
	"pokemini": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "pokemini_libretro",
	},

	// Sega Systems - RetroArch cores
	"mastersystem": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "genesis_plus_gx_libretro",
	},
	"megadrive": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "genesis_plus_gx_libretro",
	},
	"gamegear": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "genesis_plus_gx_libretro",
	},
	"sg1000": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "genesis_plus_gx_libretro",
	},
	"sega32x": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "picodrive_libretro",
	},
	"megacd": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "genesis_plus_gx_libretro",
	},
	"saturn": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_saturn_libretro",
	},

	// NEC Systems - RetroArch cores
	"pcengine": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_pce_libretro",
	},
	"pcenginecd": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_pce_libretro",
	},
	"supergrafx": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_supergrafx_libretro",
	},
	"pcfx": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_pcfx_libretro",
	},

	// SNK Systems - RetroArch cores
	"neogeo": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "fbneo_libretro",
	},
	"ngp": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_ngp_libretro",
	},
	"ngpc": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_ngp_libretro",
	},

	// Atari Systems - RetroArch cores
	"atari2600": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "stella_libretro",
	},
	"atari5200": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "atari800_libretro",
	},
	"atari7800": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "prosystem_libretro",
	},
	"lynx": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "handy_libretro",
	},
	"atarist": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "hatari_libretro",
	},

	// Arcade - RetroArch cores
	"arcade": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "fbneo_libretro",
	},
	"mame": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "mame_libretro",
	},
	"fbneo": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "fbneo_libretro",
	},

	// Other RetroArch systems
	"colecovision": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "bluemsx_libretro",
	},
	"intellivision": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "freeintv_libretro",
	},
	"vectrex": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "vecx_libretro",
	},
	"wonderswan": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_wswan_libretro",
	},
	"wswan": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_wswan_libretro",
	},
	"wswanc": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "beetle_wswan_libretro",
	},
	"msx1": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "bluemsx_libretro",
	},
	"msx2": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "bluemsx_libretro",
	},
	"amstradcpc": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "cap32_libretro",
	},
	"zxspectrum": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "fuse_libretro",
	},
	"c64": {
		Type:      EmulatorRetroArch,
		FlatpakID: "org.libretro.RetroArch",
		Core:      "vice_x64_libretro",
	},

	// Standalone emulators
	"psx": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.duckstation.DuckStation",
	},
	"ps2": {
		Type:      EmulatorStandalone,
		FlatpakID: "net.pcsx2.PCSX2",
	},
	"ps3": {
		Type:      EmulatorStandalone,
		FlatpakID: "net.rpcs3.RPCS3",
	},
	"psp": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.ppsspp.PPSSPP",
	},
	"gamecube": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.DolphinEmu.dolphin-emu",
	},
	"wii": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.DolphinEmu.dolphin-emu",
	},
	"wiiu": {
		Type:      EmulatorStandalone,
		FlatpakID: "info.cemu.Cemu",
	},
	"switch": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.ryujinx.Ryujinx",
	},
	"3ds": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.citra_emu.citra",
	},
	"dreamcast": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.flycast.Flycast",
	},
	"naomi": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.flycast.Flycast",
	},
	"atomiswave": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.flycast.Flycast",
	},
	"scummvm": {
		Type:      EmulatorStandalone,
		FlatpakID: "org.scummvm.ScummVM",
	},
}

// IsEmuDeckAvailable checks if EmuDeck is installed by looking for the Emulation directory.
func IsEmuDeckAvailable() bool {
	paths := DefaultEmuDeckPaths()
	if _, err := os.Stat(paths.RomsPath); err != nil {
		return false
	}
	return true
}

// getRetroArchCoresPath returns the path to RetroArch cores.
func getRetroArchCoresPath() string {
	homeDir, _ := os.UserHomeDir()
	// Standard Flatpak RetroArch cores location
	return filepath.Join(homeDir, ".var", "app", "org.libretro.RetroArch", "config", "retroarch", "cores")
}

// LaunchViaEmuDeck launches a game using the appropriate emulator.
// The context allows for cancellation during launch.
func LaunchViaEmuDeck(ctx context.Context, romPath, systemFolder string) (*os.Process, error) {
	emulator, ok := emulatorMapping[systemFolder]
	if !ok {
		return nil, fmt.Errorf("no emulator mapping for system: %s", systemFolder)
	}

	if !launchers.IsFlatpakInstalled(emulator.FlatpakID) {
		return nil, fmt.Errorf("emulator not installed: %s", emulator.FlatpakID)
	}

	var cmd *exec.Cmd

	switch emulator.Type {
	case EmulatorRetroArch:
		// Build RetroArch command with core
		coresPath := getRetroArchCoresPath()
		corePath := filepath.Join(coresPath, emulator.Core+".so")

		args := []string{"run", emulator.FlatpakID, "-L", corePath}
		args = append(args, emulator.Args...)
		args = append(args, romPath)

		log.Debug().
			Str("flatpakID", emulator.FlatpakID).
			Str("core", emulator.Core).
			Str("romPath", romPath).
			Msg("launching via RetroArch")

		cmd = exec.CommandContext(ctx, "flatpak", args...) //nolint:gosec // args from internal mapping

	case EmulatorStandalone:
		// Build standalone emulator command
		args := []string{"run", emulator.FlatpakID}
		args = append(args, emulator.Args...)
		args = append(args, romPath)

		log.Debug().
			Str("flatpakID", emulator.FlatpakID).
			Str("romPath", romPath).
			Msg("launching via standalone emulator")

		cmd = exec.CommandContext(ctx, "flatpak", args...) //nolint:gosec // args from internal mapping
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to launch emulator: %w", err)
	}

	log.Info().
		Str("romPath", romPath).
		Str("system", systemFolder).
		Int("pid", cmd.Process.Pid).
		Msg("game launched via EmuDeck")

	return cmd.Process, nil
}

// createEmuDeckLauncher creates a launcher for a specific EmuDeck system.
func createEmuDeckLauncher(systemFolder string, systemInfo esde.SystemInfo, paths EmuDeckPaths) platforms.Launcher {
	return platforms.Launcher{
		ID:                 fmt.Sprintf("EmuDeck%s", systemInfo.GetLauncherID()),
		SystemID:           systemInfo.SystemID,
		Lifecycle:          platforms.LifecycleTracked,
		SkipFilesystemScan: true, // Use gamelist.xml via Scanner

		Test: func(_ *config.Instance, path string) bool {
			systemDir := filepath.Join(paths.RomsPath, systemFolder)

			// Check if path is within this system's ROM directory
			relPath, err := filepath.Rel(systemDir, path)
			if err != nil {
				return false
			}

			// Ensure the path is actually within the system dir (not ../other)
			if filepath.IsAbs(relPath) || len(relPath) >= 2 && relPath[:2] == ".." {
				return false
			}

			// Skip directories and .txt files
			ext := filepath.Ext(path)
			if ext == "" || ext == ".txt" {
				return false
			}

			return true
		},

		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			proc, err := LaunchViaEmuDeck(context.Background(), path, systemFolder)
			if err != nil {
				return nil, err
			}
			// Set up gamescope focus management in Gaming Mode
			if proc != nil {
				go gamescope.ManageFocus(proc)
			}
			return proc, nil
		},

		Kill: func(_ *config.Instance) error {
			// Revert gamescope focus properties
			gamescope.RevertFocus()
			log.Debug().Msg("kill requested for EmuDeck launcher")
			return nil
		},

		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return esde.ScanGamelist(esde.ScannerConfig{
				RomsBasePath:     paths.RomsPath,
				GamelistBasePath: paths.GamelistPath,
				SystemFolder:     systemFolder,
			})
		},
	}
}

// GetEmuDeckLaunchers returns all available EmuDeck launchers.
// It scans the EmuDeck roms directory and creates a launcher for each
// system that has a configured emulator mapping.
func GetEmuDeckLaunchers(_ *config.Instance) []platforms.Launcher {
	if !IsEmuDeckAvailable() {
		log.Debug().Msg("EmuDeck not available, skipping launcher registration")
		return nil
	}

	paths := DefaultEmuDeckPaths()
	log.Info().
		Str("romsPath", paths.RomsPath).
		Str("gamelistPath", paths.GamelistPath).
		Msg("EmuDeck found, initializing launchers")

	entries, err := os.ReadDir(paths.RomsPath)
	if err != nil {
		log.Warn().
			Err(err).
			Str("path", paths.RomsPath).
			Msg("failed to read EmuDeck roms directory")
		return nil
	}

	result := make([]platforms.Launcher, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		systemFolder := entry.Name()

		// Check if we have an emulator mapping for this system
		_, hasEmulator := emulatorMapping[systemFolder]
		if !hasEmulator {
			log.Debug().
				Str("folder", systemFolder).
				Msg("no emulator mapping for EmuDeck system folder")
			continue
		}

		// Get system info from esde module
		systemInfo, ok := esde.LookupByFolderName(systemFolder)
		if !ok {
			log.Debug().
				Str("folder", systemFolder).
				Msg("unmapped EmuDeck system folder")
			continue
		}

		log.Debug().
			Str("folder", systemFolder).
			Str("systemID", systemInfo.SystemID).
			Str("launcherID", systemInfo.GetLauncherID()).
			Msg("registering EmuDeck launcher")

		launcher := createEmuDeckLauncher(systemFolder, systemInfo, paths)
		result = append(result, launcher)
	}

	log.Info().
		Int("count", len(result)).
		Msg("EmuDeck launchers registered")

	return result
}
