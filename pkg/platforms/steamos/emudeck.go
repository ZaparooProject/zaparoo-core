//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/gamescope"
	"github.com/rs/zerolog/log"
)

// EmulatorType represents an EmuDeck emulator integration type.
type EmulatorType string

const (
	// EmulatorRetroArch uses RetroArch with a libretro core.
	EmulatorRetroArch EmulatorType = "retroarch"
	// EmulatorStandalone uses a standalone Flatpak application.
	EmulatorStandalone EmulatorType = "standalone"
)

// EmulatorConfig defines an EmuDeck emulator invocation.
type EmulatorConfig struct {
	Type      EmulatorType
	FlatpakID string
	Core      string
	Args      []string
}

// EmuDeckPaths holds EmuDeck library paths.
type EmuDeckPaths struct {
	RomsPath     string
	GamelistPath string
}

// emulatorMapping combines standalone EmuDeck applications with RetroArch's
// shared core map. Standalone applications intentionally win system overlaps.
//
//nolint:gochecknoglobals // Static launcher configuration.
var emulatorMapping = buildEmulatorMapping()

func buildEmulatorMapping() map[string]EmulatorConfig {
	mapping := map[string]EmulatorConfig{
		"psx":        {Type: EmulatorStandalone, FlatpakID: "org.duckstation.DuckStation"},
		"ps2":        {Type: EmulatorStandalone, FlatpakID: "net.pcsx2.PCSX2"},
		"ps3":        {Type: EmulatorStandalone, FlatpakID: "net.rpcs3.RPCS3"},
		"psp":        {Type: EmulatorStandalone, FlatpakID: "org.ppsspp.PPSSPP"},
		"gamecube":   {Type: EmulatorStandalone, FlatpakID: "org.DolphinEmu.dolphin-emu"},
		"wii":        {Type: EmulatorStandalone, FlatpakID: "org.DolphinEmu.dolphin-emu"},
		"wiiu":       {Type: EmulatorStandalone, FlatpakID: "info.cemu.Cemu"},
		"switch":     {Type: EmulatorStandalone, FlatpakID: "org.ryujinx.Ryujinx"},
		"3ds":        {Type: EmulatorStandalone, FlatpakID: "org.citra_emu.citra"},
		"dreamcast":  {Type: EmulatorStandalone, FlatpakID: "org.flycast.Flycast"},
		"naomi":      {Type: EmulatorStandalone, FlatpakID: "org.flycast.Flycast"},
		"atomiswave": {Type: EmulatorStandalone, FlatpakID: "org.flycast.Flycast"},
		"scummvm":    {Type: EmulatorStandalone, FlatpakID: "org.scummvm.ScummVM"},
	}
	for _, core := range sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop) {
		if len(core.Folders) == 0 {
			continue
		}
		folder := core.Folders[0]
		if _, standalone := mapping[folder]; standalone {
			continue
		}
		mapping[folder] = EmulatorConfig{
			Type:      EmulatorRetroArch,
			FlatpakID: RetroArchFlatpakID,
			Core:      strings.TrimSuffix(core.Core, ".so"),
		}
	}
	return mapping
}

// DefaultEmuDeckPaths returns standard EmuDeck paths.
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

// IsEmuDeckAvailable reports whether EmuDeck's ROM directory exists.
func IsEmuDeckAvailable() bool {
	_, err := os.Stat(DefaultEmuDeckPaths().RomsPath)
	return err == nil
}

func launchStandaloneEmulator(
	ctx context.Context,
	emulator EmulatorConfig,
	romPath, systemFolder string,
) (*os.Process, error) {
	if !launchers.IsFlatpakInstalled(emulator.FlatpakID) {
		return nil, fmt.Errorf("emulator not installed: %s", emulator.FlatpakID)
	}
	args := make([]string, 0, len(emulator.Args)+3)
	args = append(args, "run", emulator.FlatpakID)
	args = append(args, emulator.Args...)
	args = append(args, romPath)

	//nolint:gosec // Flatpak ID and arguments come from built-in mappings.
	cmd := exec.CommandContext(ctx, "flatpak", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch emulator: %w", err)
	}
	log.Info().Str("romPath", romPath).Str("system", systemFolder).
		Int("pid", cmd.Process.Pid).Msg("game launched via EmuDeck")
	return cmd.Process, nil
}

func createEmuDeckLauncher(
	systemFolder string,
	systemInfo esde.SystemInfo,
	paths EmuDeckPaths,
	retroArchOpts *sharedretroarch.Options,
) platforms.Launcher {
	emulator := emulatorMapping[systemFolder]
	var launcher platforms.Launcher
	if emulator.Type == EmulatorRetroArch {
		core, ok := sharedretroarch.CoreLaunchForFolder(sharedretroarch.ProfileDesktop, systemFolder)
		if ok {
			launcher = sharedretroarch.NewLauncher(*retroArchOpts, core)
			withGamescopeFocus(&launcher)
		}
	} else {
		launcher = standaloneEmuDeckLauncher(systemFolder, systemInfo, emulator)
	}

	launcher.ID = "EmuDeck" + systemInfo.GetLauncherID()
	launcher.SystemID = systemInfo.SystemID
	launcher.SkipFilesystemScan = true
	launcher.Test = emuDeckPathTest(paths.RomsPath, systemFolder)
	launcher.Scanner = func(
		_ context.Context,
		_ *config.Instance,
		_ string,
		_ []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		return esde.ScanGamelist(esde.ScannerConfig{
			RomsBasePath: paths.RomsPath, GamelistBasePath: paths.GamelistPath,
			SystemFolder: systemFolder,
		})
	}
	return launcher
}

func standaloneEmuDeckLauncher(
	systemFolder string,
	systemInfo esde.SystemInfo,
	emulator EmulatorConfig,
) platforms.Launcher {
	return platforms.Launcher{
		ID:        "EmuDeck" + systemInfo.GetLauncherID(),
		SystemID:  systemInfo.SystemID,
		Lifecycle: platforms.LifecycleTracked,
		Availability: func(*config.Instance) error {
			if !launchers.IsFlatpakInstalled(emulator.FlatpakID) {
				return fmt.Errorf("emulator not installed: %s", emulator.FlatpakID)
			}
			return nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			proc, err := launchStandaloneEmulator(context.Background(), emulator, path, systemFolder)
			if err != nil {
				return nil, err
			}
			if proc != nil {
				go gamescope.ManageFocus(proc)
			}
			return proc, nil
		},
		Kill: func(_ *config.Instance) error {
			gamescope.RevertFocus()
			return nil
		},
	}
}

func emuDeckPathTest(romsPath, systemFolder string) func(*config.Instance, string) bool {
	return func(_ *config.Instance, path string) bool {
		systemDir := filepath.Join(romsPath, systemFolder)
		relPath, err := filepath.Rel(systemDir, path)
		if err != nil || filepath.IsAbs(relPath) || relPath == ".." ||
			strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return false
		}
		ext := filepath.Ext(path)
		return ext != "" && !strings.EqualFold(ext, ".txt")
	}
}

func buildEmuDeckLaunchers(
	_ *config.Instance,
	retroArchOpts *sharedretroarch.Options,
) []platforms.Launcher {
	if !IsEmuDeckAvailable() {
		log.Debug().Msg("EmuDeck not available, skipping launcher registration")
		return nil
	}

	paths := DefaultEmuDeckPaths()
	entries, err := os.ReadDir(paths.RomsPath)
	if err != nil {
		log.Warn().Err(err).Str("path", paths.RomsPath).Msg("failed to read EmuDeck ROM directory")
		return nil
	}

	result := make([]platforms.Launcher, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		systemFolder := entry.Name()
		if _, mapped := emulatorMapping[systemFolder]; !mapped {
			continue
		}
		systemInfo, ok := esde.LookupByFolderName(systemFolder)
		if !ok {
			continue
		}
		result = append(result, createEmuDeckLauncher(systemFolder, systemInfo, paths, retroArchOpts))
	}
	log.Info().Int("count", len(result)).Msg("EmuDeck launchers registered")
	return result
}
