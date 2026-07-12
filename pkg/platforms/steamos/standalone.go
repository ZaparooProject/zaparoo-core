//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
)

type standaloneDef struct {
	args       func(string) ([]string, error)
	id         string
	systemID   string
	flatpakID  string
	executable string
	folder     string
	scan       bool
}

//nolint:gochecknoglobals // Static launcher data.
var nativeStandaloneDefs = []standaloneDef{
	{
		id: "XeniaCanary", systemID: systemdefs.SystemXbox360, executable: "XeniaCanary",
		folder: "xbox360", scan: true, args: batchArgs(),
	},
	{
		id: "Ryubing", systemID: systemdefs.SystemSwitch, flatpakID: "io.github.ryubing.Ryujinx",
		folder: "switch", scan: true, args: batchArgs("--fullscreen"),
	},
	{
		id: "ShadPS4", systemID: systemdefs.SystemPS4, flatpakID: "net.shadps4.shadPS4",
		folder: "ps4", scan: true, args: shadPS4Args,
	},
	{
		id: "PCSX2", systemID: systemdefs.SystemPS2, flatpakID: "net.pcsx2.PCSX2",
		folder: "ps2", scan: true, args: batchArgs("-fullscreen", "-batch"),
	},
	{
		id: "Cemu", systemID: systemdefs.SystemWiiU, flatpakID: "info.cemu.Cemu",
		folder: "wiiu", scan: true, args: cemuArgs,
	},
	{
		id: "Azahar", systemID: systemdefs.System3DS, flatpakID: "org.azahar_emu.Azahar",
		folder: "3ds", scan: true, args: batchArgs("-f"),
	},
	{
		id: "Vita3K", systemID: systemdefs.SystemVita, executable: "Vita3K",
		folder: "psvita", scan: true, args: vita3KArgs,
	},
	{
		id: "RPCS3", systemID: systemdefs.SystemPS3, flatpakID: "net.rpcs3.RPCS3",
		folder: "ps3", scan: true, args: rpcs3Args,
	},
	{
		id: "DuckStation", systemID: systemdefs.SystemPSX, flatpakID: "org.duckstation.DuckStation",
		folder: "psx", args: batchArgs("-batch", "-fullscreen"),
	},
	{
		id: "PPSSPP", systemID: systemdefs.SystemPSP, flatpakID: "org.ppsspp.PPSSPP",
		folder: "psp", args: batchArgs("--fullscreen"),
	},
	{
		id: "DolphinGameCube", systemID: systemdefs.SystemGameCube, flatpakID: "org.DolphinEmu.dolphin-emu",
		folder: "gamecube", scan: true, args: batchArgs("-b", "-e"),
	},
	{
		id: "DolphinWii", systemID: systemdefs.SystemWii, flatpakID: "org.DolphinEmu.dolphin-emu",
		folder: "wii", scan: true, args: batchArgs("-b", "-e"),
	},
	{
		id: "MelonDS", systemID: systemdefs.SystemNDS, flatpakID: "net.kuribo64.melonDS",
		folder: "nds", args: batchArgs("--fullscreen"),
	},
	{
		id: "ScummVMStandalone", systemID: systemdefs.SystemScummVM, flatpakID: "org.scummvm.ScummVM",
		folder: "scummvm", args: scummVMArgs,
	},
	{
		id: "Supermodel", systemID: systemdefs.SystemModel3, flatpakID: "com.supermodel3.Supermodel",
		folder: "model3", scan: true, args: batchArgs("-fullscreen"),
	},
	{
		id: "Xemu", systemID: systemdefs.SystemXbox, flatpakID: "app.xemu.xemu",
		folder: "xbox", scan: true, args: batchArgs("-full-screen", "-dvd_path"),
	},
	{
		id: "PrimeHackGameCube", systemID: systemdefs.SystemGameCube, flatpakID: "io.github.shiiion.primehack",
		folder: "gamecube", args: batchArgs("-b", "-e"),
	},
	{
		id: "PrimeHackWii", systemID: systemdefs.SystemWii, flatpakID: "io.github.shiiion.primehack",
		folder: "wii", args: batchArgs("-b", "-e"),
	},
}

func flatpakRunArgs(flatpakID, mediaPath string, emulatorArgs []string) []string {
	args := make([]string, 0, 4+len(emulatorArgs))
	args = append(args,
		"run",
		"--filesystem="+filepath.Dir(mediaPath)+":ro",
		"--die-with-parent",
		flatpakID,
	)
	return append(args, emulatorArgs...)
}

func batchArgs(prefix ...string) func(string) ([]string, error) {
	return func(path string) ([]string, error) {
		return append(append([]string(nil), prefix...), path), nil
	}
}

func shadPS4Args(path string) ([]string, error) {
	target, err := readLauncherTarget(path, ".ps4")
	if err != nil {
		return nil, err
	}
	return []string{"-g", target, "--fullscreen", "true"}, nil
}

func cemuArgs(path string) ([]string, error) {
	return []string{"-g", path, "-f"}, nil
}

func vita3KArgs(path string) ([]string, error) {
	titleID, err := readLauncherTarget(path, ".psvita")
	if err != nil {
		return nil, err
	}
	return []string{"-Fr", titleID}, nil
}

func rpcs3Args(path string) ([]string, error) {
	if strings.EqualFold(filepath.Ext(path), ".ps3") {
		target, err := readLauncherTarget(path, ".ps3")
		if err != nil {
			return nil, err
		}
		path = target
	}
	return []string{"--no-gui", path}, nil
}

func readLauncherTarget(path, extension string) (string, error) {
	if !strings.EqualFold(filepath.Ext(path), extension) {
		return "", fmt.Errorf("launcher target requires a %s file", extension)
	}
	//nolint:gosec // Path was resolved through the media database and launch allow-list.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read launcher target: %w", err)
	}
	target := strings.TrimSpace(string(data))
	if target == "" || strings.ContainsAny(target, "\r\n") {
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

func nativeStandaloneLaunchers() []platforms.Launcher {
	result := make([]platforms.Launcher, 0, len(nativeStandaloneDefs))
	for i := range nativeStandaloneDefs {
		result = append(result, newNativeStandaloneLauncher(&nativeStandaloneDefs[i]))
	}
	return result
}

func resolveStandaloneExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	path := filepath.Join(home, ".local", "bin", name)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return path, nil
	}
	return "", fmt.Errorf("executable not found: %s", name)
}

func newNativeStandaloneLauncher(def *standaloneDef) platforms.Launcher {
	launcher := platforms.Launcher{
		ID:                 def.id,
		SystemID:           def.systemID,
		Groups:             []string{platformshared.LauncherGroupNative},
		Lifecycle:          platforms.LifecycleTracked,
		SkipFilesystemScan: !def.scan,
		Availability: func(*config.Instance) error {
			if def.flatpakID != "" {
				if !launchers.IsFlatpakInstalled(def.flatpakID) {
					return fmt.Errorf("emulator not installed: %s", def.flatpakID)
				}
				return nil
			}
			if _, err := resolveStandaloneExecutable(def.executable); err != nil {
				return fmt.Errorf("emulator not installed: %s: %w", def.executable, err)
			}
			return nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			args, err := def.args(path)
			if err != nil {
				return nil, err
			}
			var command string
			commandArgs := args
			if def.flatpakID != "" {
				command = "flatpak"
				commandArgs = flatpakRunArgs(def.flatpakID, path, args)
			} else {
				command, err = resolveStandaloneExecutable(def.executable)
				if err != nil {
					return nil, fmt.Errorf("resolve %s executable: %w", def.id, err)
				}
			}
			//nolint:gosec // Executable, Flatpak ID, and fixed arguments come from built-in definitions.
			cmd := exec.CommandContext(context.Background(), command, commandArgs...)
			cmd.Env = steamOSLaunchEnv()
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("launch %s: %w", def.id, err)
			}
			go steamOSGameMode.ManageFocus(cmd.Process)
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
			}
		}
	}
	return launcher
}
