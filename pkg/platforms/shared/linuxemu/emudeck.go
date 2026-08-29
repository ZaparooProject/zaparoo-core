//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/rs/zerolog/log"
)

type emulatorType string

const (
	emulatorRetroArch  emulatorType = "retroarch"
	emulatorStandalone emulatorType = "standalone"
)

type emulatorConfig struct {
	wrapperArgs func(string) ([]string, error)
	typeName    emulatorType
	flatpakID   string
	core        string
	wrapper     string
}

//nolint:gochecknoglobals // Static launcher configuration.
var emulatorMapping = buildEmulatorMapping()

func buildEmulatorMapping() map[string]emulatorConfig {
	mapping := map[string]emulatorConfig{
		"psx": {
			typeName: emulatorStandalone, flatpakID: "org.duckstation.DuckStation",
			wrapper: "duckstation.sh", wrapperArgs: batchArgs("-batch", "-fullscreen"),
		},
		"ps2": {
			typeName: emulatorStandalone, flatpakID: "net.pcsx2.PCSX2",
			wrapper: "pcsx2-qt.sh", wrapperArgs: batchArgs("-batch", "-fullscreen"),
		},
		"ps3": {
			typeName: emulatorStandalone, flatpakID: "net.rpcs3.RPCS3",
			wrapper: "rpcs3.sh", wrapperArgs: batchArgs("--no-gui", "--fullscreen"),
		},
		"psp": {
			typeName: emulatorStandalone, flatpakID: "org.ppsspp.PPSSPP",
			wrapper: "ppsspp.sh", wrapperArgs: batchArgs("--fullscreen"),
		},
		"gamecube": {
			typeName: emulatorStandalone, flatpakID: "org.DolphinEmu.dolphin-emu",
			wrapper: "dolphin-emu.sh", wrapperArgs: batchArgs("-b", "-f", "-e"),
		},
		"wii": {
			typeName: emulatorStandalone, flatpakID: "org.DolphinEmu.dolphin-emu",
			wrapper: "dolphin-emu.sh", wrapperArgs: batchArgs("-b", "-f", "-e"),
		},
		"wiiu": {
			typeName: emulatorStandalone, flatpakID: "info.cemu.Cemu",
			wrapper: "cemu.sh", wrapperArgs: batchArgs("-f", "-g"),
		},
		"switch": {
			typeName: emulatorStandalone, flatpakID: "io.github.ryubing.Ryujinx",
			wrapper: "ryujinx.sh", wrapperArgs: batchArgs("--fullscreen"),
		},
		"3ds": {
			typeName: emulatorStandalone, flatpakID: "org.azahar_emu.Azahar",
			wrapper: "azahar.sh", wrapperArgs: batchArgs("-f"),
		},
		"scummvm": {
			typeName: emulatorStandalone, flatpakID: "org.scummvm.ScummVM",
			wrapper: "scummvm.sh", wrapperArgs: scummVMArgs,
		},
	}
	for _, core := range sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop) {
		if len(core.Folders) == 0 {
			continue
		}
		folder := core.Folders[0]
		if _, standalone := mapping[folder]; standalone {
			continue
		}
		mapping[folder] = emulatorConfig{
			typeName: emulatorRetroArch, flatpakID: RetroArchFlatpakID,
			core: strings.TrimSuffix(core.Core, ".so"),
		}
	}
	return mapping
}

func emuDeckLaunchers(_ *config.Instance, options *Options) []platforms.Launcher {
	paths := DefaultEmuDeckPaths(options.HomeDir)
	if !directoriesExist(paths.RomsPath) {
		return nil
	}
	folders, err := readProviderSystemFolders(paths.RomsPath)
	if err != nil {
		log.Warn().Err(err).Str("path", paths.RomsPath).Msg("failed to read EmuDeck ROM directory")
		return nil
	}
	result := make([]platforms.Launcher, 0, len(folders))
	for _, folder := range folders {
		mappedFolder := emuDeckSystemFolder(folder)
		emulator, mapped := emulatorMapping[mappedFolder]
		if !mapped || (emulator.typeName == emulatorRetroArch && !retroArchAvailable(options)) ||
			(emulator.typeName == emulatorStandalone && !emuDeckEmulatorAvailable(options, &paths, emulator)) {
			continue
		}
		info, ok := esde.LookupByFolderName(mappedFolder)
		if !ok {
			continue
		}
		launcher, ok := createEmuDeckLauncher(options, folder, mappedFolder, info, &paths, emulator)
		if ok {
			result = append(result, launcher)
		}
	}
	return result
}

func createEmuDeckLauncher(
	options *Options,
	folder, mappedFolder string,
	info esde.SystemInfo,
	paths *EmuDeckPaths,
	emulator emulatorConfig,
) (platforms.Launcher, bool) {
	var launcher platforms.Launcher
	if emulator.typeName == emulatorRetroArch {
		core, ok := sharedretroarch.CoreLaunchForFolder(sharedretroarch.ProfileDesktop, mappedFolder)
		if !ok {
			return platforms.Launcher{}, false
		}
		launcher = sharedretroarch.NewLauncher(*options.RetroArch, core)
	} else {
		launcher = standaloneEmuDeckLauncher(options, paths, emulator)
	}
	launcher.ID = "EmuDeck" + info.GetLauncherID()
	launcher.SystemID = info.SystemID
	launcher.Groups = append(launcher.Groups, platformshared.LauncherGroupEmuDeck)
	launcher.Test = providerPathTest(paths.RomsPath, folder)
	gamelistPath := filepath.Join(paths.GamelistPath, folder, "gamelist.xml")
	if statInfo, err := os.Stat(gamelistPath); err == nil && statInfo.Mode().IsRegular() {
		launcher.SkipFilesystemScan = true
		launcher.Scanner = func(
			_ context.Context,
			_ *config.Instance,
			_ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return esde.ScanGamelist(esde.ScannerConfig{
				RomsBasePath: paths.RomsPath, GamelistBasePath: paths.GamelistPath, SystemFolder: folder,
			})
		}
	} else {
		launcher.SkipFilesystemScan = false
		launcher.Scanner = nil
		launcher.Folders = []string{filepath.Join(paths.RomsPath, folder)}
		launcher.Extensions = append([]string(nil), info.Extensions...)
	}
	guardProviderLauncher(&launcher)
	wrapGameMode(options, &launcher)
	return launcher, true
}

func emuDeckSystemFolder(folder string) string {
	switch folder {
	case "gc":
		return "gamecube"
	case "n3ds":
		return "3ds"
	default:
		return folder
	}
}

func emuDeckWrapperPath(paths *EmuDeckPaths, emulator emulatorConfig) string {
	if emulator.wrapper == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(paths.RomsPath), "tools", "launchers", emulator.wrapper)
}

func validEmuDeckWrapper(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func emuDeckEmulatorAvailable(options *Options, paths *EmuDeckPaths, emulator emulatorConfig) bool {
	return validEmuDeckWrapper(emuDeckWrapperPath(paths, emulator)) ||
		(emulator.flatpakID != "" && options.IsFlatpakInstalled(emulator.flatpakID))
}

func standaloneEmuDeckLauncher(
	options *Options,
	paths *EmuDeckPaths,
	emulator emulatorConfig,
) platforms.Launcher {
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		return emuDeckStandaloneCommand(options, paths, emulator, path)
	}
	return platforms.Launcher{
		Lifecycle: platforms.LifecycleBlocking,
		Availability: func(*config.Instance) error {
			if !emuDeckEmulatorAvailable(options, paths, emulator) {
				return fmt.Errorf("EmuDeck emulator is not installed: %s", emulator.wrapper)
			}
			return nil
		},
		BuildLaunchCommand: func(
			_ *config.Instance,
			path string,
			_ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return buildCommand(path)
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			//nolint:gosec // Executable and arguments come from fixed EmuDeck wrapper or Flatpak definitions.
			cmd := exec.CommandContext(context.Background(), command.Executable, command.Args...)
			cmd.Env = helpers.MergeEnviron(os.Environ(), command.Env)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("launch emulator: %w", err)
			}
			return cmd.Process, nil
		},
	}
}

func emuDeckStandaloneCommand(
	options *Options,
	paths *EmuDeckPaths,
	emulator emulatorConfig,
	path string,
) (*platforms.LaunchCommand, error) {
	if err := validateMediaFile(path); err != nil {
		return nil, err
	}
	argsBuilder := emulator.wrapperArgs
	if argsBuilder == nil {
		argsBuilder = batchArgs()
	}
	args, err := argsBuilder(path)
	if err != nil {
		return nil, err
	}
	executable := emuDeckWrapperPath(paths, emulator)
	if !validEmuDeckWrapper(executable) {
		if emulator.flatpakID == "" || !options.IsFlatpakInstalled(emulator.flatpakID) {
			return nil, fmt.Errorf("EmuDeck emulator is not installed: %s", emulator.wrapper)
		}
		executable = "flatpak"
		args, err = flatpakRunArgs(emulator.flatpakID, path, args)
		if err != nil {
			return nil, err
		}
	}
	var env []string
	if options.LaunchEnv != nil {
		env = options.LaunchEnv()
	}
	return &platforms.LaunchCommand{Executable: executable, Args: args, Env: env}, nil
}

func guardProviderLauncher(launcher *platforms.Launcher) {
	if launcher == nil || launcher.Test == nil {
		return
	}
	testPath := launcher.Test
	build := launcher.BuildLaunchCommand
	if build != nil {
		launcher.BuildLaunchCommand = func(
			cfg *config.Instance,
			path string,
			opts *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			if !testPath(cfg, path) {
				return nil, errors.New("media path is outside provider ROM directory")
			}
			return build(cfg, path, opts)
		}
	}
	launch := launcher.Launch
	if launch != nil {
		launcher.Launch = func(
			cfg *config.Instance,
			path string,
			opts *platforms.LaunchOptions,
		) (*os.Process, error) {
			if !testPath(cfg, path) {
				return nil, errors.New("media path is outside provider ROM directory")
			}
			return launch(cfg, path, opts)
		}
	}
}

func providerPathTest(romsPath, folder string) func(*config.Instance, string) bool {
	return func(_ *config.Instance, path string) bool {
		systemDir := filepath.Join(romsPath, folder)
		rel, err := filepath.Rel(systemDir, path)
		if err != nil || filepath.IsAbs(rel) || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		realRoot, err := filepath.EvalSymlinks(systemDir)
		if err != nil {
			return false
		}
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false
		}
		realRel, err := filepath.Rel(realRoot, realPath)
		if err != nil || filepath.IsAbs(realRel) || realRel == ".." ||
			strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return false
		}
		ext := filepath.Ext(path)
		return ext != "" && !strings.EqualFold(ext, ".txt")
	}
}

func directoriesExist(paths ...string) bool {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}
