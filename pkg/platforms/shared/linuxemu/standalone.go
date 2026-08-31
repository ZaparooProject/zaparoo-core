//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

const maxLauncherTargetSize = 4096

type standaloneDef struct {
	args             func(string) ([]string, error)
	id               string
	systemID         string
	flatpakID        string
	flatpakCommand   string
	folder           string
	executables      []string
	appImagePrefixes []string
	scan             bool
}

//nolint:gochecknoglobals // Static launcher data.
var nativeStandaloneDefs = []standaloneDef{
	{
		id: "XeniaCanary", systemID: systemdefs.SystemXbox360,
		executables:      []string{"xenia_canary", "xenia-canary", "XeniaCanary"},
		appImagePrefixes: []string{"xenia_canary", "xenia-canary", "XeniaCanary"},
		folder:           "xbox360", scan: true, args: batchArgs(),
	},
	{
		id: "Ryubing", systemID: systemdefs.SystemSwitch, flatpakID: "io.github.ryubing.Ryujinx",
		executables:      []string{"Ryujinx", "ryujinx", "Ryubing"},
		appImagePrefixes: []string{"ryujinx", "Ryubing"},
		folder:           "switch", scan: true, args: batchArgs("--fullscreen"),
	},
	{
		id: "ShadPS4", systemID: systemdefs.SystemPS4, flatpakID: "net.shadps4.shadPS4",
		flatpakCommand: "shadps4",
		executables:    []string{"shadps4"}, appImagePrefixes: []string{"shadps4"},
		folder: "ps4", scan: true, args: shadPS4Args,
	},
	{
		id: "PCSX2", systemID: systemdefs.SystemPS2, flatpakID: "net.pcsx2.PCSX2",
		executables: []string{"pcsx2-qt", "pcsx2"}, appImagePrefixes: []string{"pcsx2-Qt", "PCSX2"},
		folder: "ps2", scan: true, args: batchArgs("-fullscreen", "-batch"),
	},
	{
		id: "Cemu", systemID: systemdefs.SystemWiiU, flatpakID: "info.cemu.Cemu",
		executables: []string{"cemu", "Cemu"}, appImagePrefixes: []string{"Cemu"},
		folder: "wiiu", scan: true, args: cemuArgs,
	},
	{
		id: "Azahar", systemID: systemdefs.System3DS, flatpakID: "org.azahar_emu.Azahar",
		executables: []string{"azahar"}, appImagePrefixes: []string{"Azahar"},
		folder: "3ds", scan: true, args: batchArgs("-f"),
	},
	{
		id: "Vita3K", systemID: systemdefs.SystemVita, executables: []string{"Vita3K"},
		appImagePrefixes: []string{"Vita3K"}, folder: "psvita", scan: true, args: vita3KArgs,
	},
	{
		id: "RPCS3", systemID: systemdefs.SystemPS3, flatpakID: "net.rpcs3.RPCS3",
		executables: []string{"rpcs3"}, appImagePrefixes: []string{"rpcs3", "RPCS3"},
		folder: "ps3", scan: true, args: rpcs3Args,
	},
	{
		id: "DuckStation", systemID: systemdefs.SystemPSX, flatpakID: "org.duckstation.DuckStation",
		executables: []string{"duckstation-qt", "duckstation"}, appImagePrefixes: []string{"DuckStation"},
		folder: "psx", args: batchArgs("-batch", "-fullscreen"),
	},
	{
		id: "PPSSPP", systemID: systemdefs.SystemPSP, flatpakID: "org.ppsspp.PPSSPP",
		executables:      []string{"PPSSPPSDL", "PPSSPPQt", "ppsspp-qt", "ppsspp"},
		appImagePrefixes: []string{"PPSSPP"},
		folder:           "psp", args: batchArgs("--fullscreen"),
	},
	{
		id: "DolphinGameCube", systemID: systemdefs.SystemGameCube, flatpakID: "org.DolphinEmu.dolphin-emu",
		executables: []string{"dolphin-emu", "dolphin-emu-qt"},
		folder:      "gamecube", scan: true, args: batchArgs("-b", "-f", "-e"),
	},
	{
		id: "DolphinWii", systemID: systemdefs.SystemWii, flatpakID: "org.DolphinEmu.dolphin-emu",
		executables: []string{"dolphin-emu", "dolphin-emu-qt"},
		folder:      "wii", scan: true, args: batchArgs("-b", "-f", "-e"),
	},
	{
		id: "MelonDS", systemID: systemdefs.SystemNDS, flatpakID: "net.kuribo64.melonDS",
		executables: []string{"melonDS"}, appImagePrefixes: []string{"melonDS"},
		folder: "nds", args: batchArgs("--fullscreen"),
	},
	{
		id: "ScummVMStandalone", systemID: systemdefs.SystemScummVM, flatpakID: "org.scummvm.ScummVM",
		executables: []string{"scummvm"}, folder: "scummvm", args: scummVMArgs,
	},
	{
		id: "Supermodel", systemID: systemdefs.SystemModel3, flatpakID: "com.supermodel3.Supermodel",
		executables: []string{"supermodel", "Supermodel"}, appImagePrefixes: []string{"Supermodel"},
		folder: "model3", scan: true, args: batchArgs("-fullscreen"),
	},
	{
		id: "Xemu", systemID: systemdefs.SystemXbox, flatpakID: "app.xemu.xemu",
		executables: []string{"xemu"}, appImagePrefixes: []string{"xemu", "Xemu"},
		folder: "xbox", scan: true, args: batchArgs("-full-screen", "-dvd_path"),
	},
	{
		id: "MAME", systemID: systemdefs.SystemArcade, flatpakID: "org.mamedev.MAME",
		executables: []string{"mame"}, folder: "arcade", scan: true, args: mameArgs,
	},
	{
		id: "FlycastDreamcast", systemID: systemdefs.SystemDreamcast, flatpakID: "org.flycast.Flycast",
		executables: []string{"flycast"}, appImagePrefixes: []string{"flycast"},
		folder: "dreamcast", scan: true, args: batchArgs("-config", "window:fullscreen=yes"),
	},
	{
		id: "FlycastNaomi", systemID: systemdefs.SystemNAOMI, flatpakID: "org.flycast.Flycast",
		executables: []string{"flycast"}, appImagePrefixes: []string{"flycast"},
		folder: "naomi", scan: true, args: batchArgs("-config", "window:fullscreen=yes"),
	},
	{
		id: "FlycastAtomiswave", systemID: systemdefs.SystemAtomiswave, flatpakID: "org.flycast.Flycast",
		executables: []string{"flycast"}, appImagePrefixes: []string{"flycast"},
		folder: "atomiswave", scan: true, args: batchArgs("-config", "window:fullscreen=yes"),
	},
	{
		id: "RMG", systemID: systemdefs.SystemNintendo64, flatpakID: "com.github.Rosalie241.RMG",
		executables: []string{"RMG"}, appImagePrefixes: []string{"RMG"},
		folder: "n64", scan: true, args: batchArgs("--fullscreen", "--quit-after-emulation"),
	},
	{
		id: "mGBAGBA", systemID: systemdefs.SystemGBA, flatpakID: "io.mgba.mGBA",
		executables: []string{"mgba-qt", "mgba"}, folder: "gba", scan: true, args: batchArgs("-f"),
	},
	{
		id: "mGBAGB", systemID: systemdefs.SystemGameboy, flatpakID: "io.mgba.mGBA",
		executables: []string{"mgba-qt", "mgba"}, folder: "gb", scan: true, args: batchArgs("-f"),
	},
	{
		id: "mGBAGBC", systemID: systemdefs.SystemGameboyColor, flatpakID: "io.mgba.mGBA",
		executables: []string{"mgba-qt", "mgba"}, folder: "gbc", scan: true, args: batchArgs("-f"),
	},
	{
		id: "Ruffle", systemID: systemdefs.SystemPC, flatpakID: "rs.ruffle.Ruffle",
		executables: []string{"ruffle", "ruffle_desktop"}, appImagePrefixes: []string{"ruffle"},
		folder: "flash", scan: true, args: batchArgs("--fullscreen"),
	},
	{
		id: "PrimeHackGameCube", systemID: systemdefs.SystemGameCube, flatpakID: "io.github.shiiion.primehack",
		executables: []string{"primehack"}, folder: "gamecube", args: batchArgs("-b", "-f", "-e"),
	},
	{
		id: "PrimeHackWii", systemID: systemdefs.SystemWii, flatpakID: "io.github.shiiion.primehack",
		executables: []string{"primehack"}, folder: "wii", args: batchArgs("-b", "-f", "-e"),
	},
}

func flatpakRunArgs(flatpakID, mediaPath string, emulatorArgs []string) ([]string, error) {
	return flatpakRunArgsWithCommand(flatpakID, "", mediaPath, emulatorArgs)
}

func flatpakRunArgsWithCommand(flatpakID, command, mediaPath string, emulatorArgs []string) ([]string, error) {
	mediaDir := filepath.Dir(mediaPath)
	if !filepath.IsAbs(mediaDir) || mediaDir == string(filepath.Separator) || strings.Contains(mediaDir, ":") {
		return nil, errors.New("media path cannot be exposed safely to Flatpak")
	}
	args := make([]string, 0, 5+len(emulatorArgs))
	args = append(args, "run", "--filesystem="+mediaDir+":ro", "--die-with-parent")
	if command != "" {
		args = append(args, "--command="+command)
	}
	args = append(args, flatpakID)
	return append(args, emulatorArgs...), nil
}

func batchArgs(prefix ...string) func(string) ([]string, error) {
	return func(path string) ([]string, error) {
		args := append([]string(nil), prefix...)
		return append(args, path), nil
	}
}

func shadPS4Args(path string) ([]string, error) {
	target, err := readLauncherTarget(path, ".ps4")
	if err != nil {
		return nil, err
	}
	return []string{"-g", target, "--fullscreen", "true"}, nil
}

func cemuArgs(path string) ([]string, error) { return []string{"-g", path, "-f"}, nil }

func mameArgs(path string) ([]string, error) {
	extension := filepath.Ext(path)
	if !strings.EqualFold(extension, ".zip") && !strings.EqualFold(extension, ".7z") {
		return nil, errors.New("MAME launch requires a .zip or .7z ROM set")
	}
	machine := strings.TrimSuffix(filepath.Base(path), extension)
	if machine == "" || strings.HasPrefix(machine, "-") || strings.ContainsAny(machine, "\x00\r\n") {
		return nil, errors.New("invalid MAME machine name")
	}
	return []string{"-nowindow", "-rompath", filepath.Dir(path), machine}, nil
}

func vita3KArgs(path string) ([]string, error) {
	titleID, err := readLauncherTarget(path, ".psvita")
	if err != nil {
		return nil, err
	}
	return []string{"-F", "-r", titleID}, nil
}

func rpcs3Args(path string) ([]string, error) {
	if strings.EqualFold(filepath.Ext(path), ".ps3") {
		target, err := readLauncherTarget(path, ".ps3")
		if err != nil {
			return nil, err
		}
		path = target
	}
	return []string{"--no-gui", "--fullscreen", path}, nil
}

func readLauncherTarget(path, extension string) (string, error) {
	if !strings.EqualFold(filepath.Ext(path), extension) {
		return "", fmt.Errorf("launcher target requires a %s file", extension)
	}
	//nolint:gosec // Path was resolved through the media database and launch allow-list.
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open launcher target: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file; close errors do not affect parsed data.
	data, err := io.ReadAll(io.LimitReader(file, maxLauncherTargetSize+1))
	if err != nil {
		return "", fmt.Errorf("read launcher target: %w", err)
	}
	target := strings.TrimSpace(string(data))
	if target == "" || len(data) > maxLauncherTargetSize || strings.HasPrefix(target, "-") ||
		strings.ContainsAny(target, "\r\n\x00") {
		return "", errors.New("invalid launcher target")
	}
	return target, nil
}

func scummVMArgs(path string) ([]string, error) {
	if !strings.EqualFold(filepath.Ext(path), ".scummvm") {
		return nil, errors.New("ScummVM launch requires a .scummvm target file")
	}
	target, err := readLauncherTarget(path, ".scummvm")
	if err != nil {
		return nil, fmt.Errorf("read ScummVM target: %w", err)
	}
	return []string{"--fullscreen", "--path=" + filepath.Dir(path), target}, nil
}

const maxStandaloneAppImages = 1024

type standaloneInstallation struct {
	executable string
	flatpakID  string
}

func nativeStandaloneLaunchers(options *Options) []platforms.Launcher {
	appImages := discoverStandaloneAppImages(options.HomeDir)
	result := make([]platforms.Launcher, 0, len(nativeStandaloneDefs))
	for i := range nativeStandaloneDefs {
		installation, err := resolveStandaloneInstallation(options, &nativeStandaloneDefs[i], appImages)
		if err != nil {
			continue
		}
		result = append(result, newNativeStandaloneLauncher(options, &nativeStandaloneDefs[i], installation))
	}
	return result
}

func discoverStandaloneAppImages(homeDir string) []string {
	if homeDir == "" {
		return nil
	}
	//nolint:gosec // Fixed per-user EmuDeck AppImage directory.
	directory, err := os.Open(filepath.Join(homeDir, "Applications"))
	if err != nil {
		return nil
	}
	defer directory.Close() //nolint:errcheck // Read-only directory handle.
	result := make([]string, 0, 32)
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if len(result) >= maxStandaloneAppImages {
				return result
			}
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() ||
				!strings.EqualFold(filepath.Ext(entry.Name()), ".AppImage") {
				continue
			}
			path := filepath.Join(homeDir, "Applications", entry.Name())
			info, statErr := entry.Info()
			if statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				result = append(result, path)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil
		}
	}
	sort.Strings(result)
	return result
}

func resolveStandaloneInstallation(
	options *Options,
	def *standaloneDef,
	appImages []string,
) (standaloneInstallation, error) {
	for _, name := range def.executables {
		if path, err := options.LookPath(name); err == nil {
			return standaloneInstallation{executable: path}, nil
		}
		if options.HomeDir != "" {
			path := filepath.Join(options.HomeDir, ".local", "bin", name)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				return standaloneInstallation{executable: path}, nil
			}
		}
	}
	for _, prefix := range def.appImagePrefixes {
		for _, path := range appImages {
			if strings.HasPrefix(strings.ToLower(filepath.Base(path)), strings.ToLower(prefix)) {
				return standaloneInstallation{executable: path}, nil
			}
		}
	}
	if def.flatpakID != "" && options.IsFlatpakInstalled(def.flatpakID) {
		return standaloneInstallation{executable: "flatpak", flatpakID: def.flatpakID}, nil
	}
	return standaloneInstallation{}, fmt.Errorf("emulator not installed: %s", def.id)
}

func standaloneInstallationAvailable(options *Options, installation standaloneInstallation) bool {
	if installation.flatpakID != "" {
		return options.IsFlatpakInstalled(installation.flatpakID)
	}
	info, err := os.Stat(installation.executable)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func buildStandaloneLaunchCommand(options *Options, def *standaloneDef, path string) (*platforms.LaunchCommand, error) {
	installation, err := resolveStandaloneInstallation(
		options, def, discoverStandaloneAppImages(options.HomeDir),
	)
	if err != nil {
		return nil, err
	}
	return buildStandaloneLaunchCommandForInstallation(options, def, installation, path)
}

func buildStandaloneLaunchCommandForInstallation(
	options *Options,
	def *standaloneDef,
	installation standaloneInstallation,
	path string,
) (*platforms.LaunchCommand, error) {
	if err := validateMediaFile(path); err != nil {
		return nil, err
	}
	args, err := def.args(path)
	if err != nil {
		return nil, err
	}
	commandArgs := args
	if installation.flatpakID != "" {
		commandArgs, err = flatpakRunArgsWithCommand(
			installation.flatpakID, def.flatpakCommand, path, args,
		)
		if err != nil {
			return nil, err
		}
	}
	var env []string
	if options.LaunchEnv != nil {
		env = options.LaunchEnv()
	}
	return &platforms.LaunchCommand{Executable: installation.executable, Args: commandArgs, Env: env}, nil
}

func validateMediaFile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("media path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat media path: %w", err)
	}
	if info.IsDir() {
		return errors.New("media path must be a file")
	}
	return nil
}

func newNativeStandaloneLauncher(
	options *Options,
	def *standaloneDef,
	installation standaloneInstallation,
) platforms.Launcher {
	launcher := platforms.Launcher{
		ID: def.id, SystemID: def.systemID, Groups: []string{platformshared.LauncherGroupNative},
		Lifecycle: platforms.LifecycleBlocking, SkipFilesystemScan: !def.scan,
		Availability: func(*config.Instance) error {
			if !standaloneInstallationAvailable(options, installation) {
				return fmt.Errorf("emulator not installed: %s", def.id)
			}
			return nil
		},
		BuildLaunchCommand: func(
			_ *config.Instance,
			path string,
			_ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return buildStandaloneLaunchCommandForInstallation(options, def, installation, path)
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			commandSpec, err := buildStandaloneLaunchCommandForInstallation(options, def, installation, path)
			if err != nil {
				return nil, err
			}
			//nolint:gosec // Executable, Flatpak ID, and fixed arguments come from built-in definitions.
			cmd := exec.CommandContext(context.Background(), commandSpec.Executable, commandSpec.Args...)
			cmd.Env = helpers.MergeEnviron(os.Environ(), commandSpec.Env)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("launch %s: %w", def.id, err)
			}
			return cmd.Process, nil
		},
	}
	if def.scan {
		if info, ok := esde.LookupByFolderName(def.folder); ok {
			launcher.Folders = []string{def.folder}
			launcher.Extensions = append([]string(nil), info.Extensions...)
			switch def.id {
			case "Ryubing":
				launcher.Extensions = append(launcher.Extensions, ".nro")
			case "ShadPS4":
				launcher.Extensions = []string{".ps4"}
			case "PCSX2":
				launcher.Extensions = append(launcher.Extensions, ".elf")
			case "Azahar":
				launcher.Extensions = append(launcher.Extensions, ".3dsx")
			case "Vita3K":
				launcher.Extensions = []string{".psvita"}
			case "RPCS3":
				launcher.Extensions = []string{".ps3"}
			case "MAME":
				launcher.Extensions = []string{".zip", ".7z"}
			}
		}
	}
	wrapGameMode(options, &launcher)
	return launcher
}
