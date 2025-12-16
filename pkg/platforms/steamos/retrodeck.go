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

const (
	// RetroDECKFlatpakID is the Flatpak application ID for RetroDECK
	RetroDECKFlatpakID = "net.retrodeck.retrodeck"
)

// RetroDECKPaths holds the paths for RetroDECK installation.
type RetroDECKPaths struct {
	// RomsPath is the base path for ROMs (e.g., ~/retrodeck/roms/)
	RomsPath string
	// GamelistPath is the base path for ES-DE gamelists (e.g., ~/retrodeck/ES-DE/gamelists/)
	GamelistPath string
}

// DefaultRetroDECKPaths returns the default paths for RetroDECK.
func DefaultRetroDECKPaths() RetroDECKPaths {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
	}

	return RetroDECKPaths{
		RomsPath:     filepath.Join(homeDir, "retrodeck", "roms"),
		GamelistPath: filepath.Join(homeDir, "retrodeck", "ES-DE", "gamelists"),
	}
}

// IsRetroDECKInstalled checks if RetroDECK is installed via Flatpak.
func IsRetroDECKInstalled() bool {
	return launchers.IsFlatpakInstalled(RetroDECKFlatpakID)
}

// IsRetroDECKAvailable checks if RetroDECK is installed and the roms directory exists.
func IsRetroDECKAvailable() bool {
	if !IsRetroDECKInstalled() {
		return false
	}

	paths := DefaultRetroDECKPaths()
	if _, err := os.Stat(paths.RomsPath); err != nil {
		return false
	}

	return true
}

// LaunchViaRetroDECK launches a game using RetroDECK's CLI.
// RetroDECK uses RetroENGINE which accepts a ROM path directly.
func LaunchViaRetroDECK(ctx context.Context, romPath string) (*os.Process, error) {
	log.Debug().
		Str("romPath", romPath).
		Msg("launching game via RetroDECK")

	// Use flatpak run with the RetroDECK app ID
	cmd := exec.CommandContext(ctx, "flatpak", "run", RetroDECKFlatpakID, romPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to launch RetroDECK: %w", err)
	}

	log.Info().
		Str("romPath", romPath).
		Int("pid", cmd.Process.Pid).
		Msg("game launched via RetroDECK")

	return cmd.Process, nil
}

// createRetroDECKLauncher creates a launcher for a specific RetroDECK system.
func createRetroDECKLauncher(systemFolder string, systemInfo esde.SystemInfo, paths RetroDECKPaths) platforms.Launcher {
	return platforms.Launcher{
		ID:                 fmt.Sprintf("RetroDECK%s", systemInfo.GetLauncherID()),
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
			if filepath.IsAbs(relPath) || relPath[:2] == ".." {
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
			proc, err := LaunchViaRetroDECK(context.Background(), path)
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
			log.Debug().Msg("kill requested for RetroDECK launcher")
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

// GetRetroDECKLaunchers returns all available RetroDECK launchers.
// It scans the RetroDECK roms directory and creates a launcher for each
// recognized system folder.
func GetRetroDECKLaunchers(_ *config.Instance) []platforms.Launcher {
	if !IsRetroDECKAvailable() {
		log.Debug().Msg("RetroDECK not available, skipping launcher registration")
		return nil
	}

	paths := DefaultRetroDECKPaths()
	log.Info().
		Str("romsPath", paths.RomsPath).
		Str("gamelistPath", paths.GamelistPath).
		Msg("RetroDECK found, initializing launchers")

	entries, err := os.ReadDir(paths.RomsPath)
	if err != nil {
		log.Warn().
			Err(err).
			Str("path", paths.RomsPath).
			Msg("failed to read RetroDECK roms directory")
		return nil
	}

	result := make([]platforms.Launcher, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		systemFolder := entry.Name()
		systemInfo, ok := esde.LookupByFolderName(systemFolder)
		if !ok {
			log.Debug().
				Str("folder", systemFolder).
				Msg("unmapped RetroDECK system folder")
			continue
		}

		log.Debug().
			Str("folder", systemFolder).
			Str("systemID", systemInfo.SystemID).
			Str("launcherID", systemInfo.GetLauncherID()).
			Msg("registering RetroDECK launcher")

		launcher := createRetroDECKLauncher(systemFolder, systemInfo, paths)
		result = append(result, launcher)
	}

	log.Info().
		Int("count", len(result)).
		Msg("RetroDECK launchers registered")

	return result
}
