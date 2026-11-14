//go:build windows

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

package windows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/standalone"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	retroArchName          = "RetroArch"
	retroArchSteamAppID    = 1118310
	retroArchNetworkAddr   = "127.0.0.1:55355"
	retroArchNetworkConfig = "retroarch-network.cfg"
)

// retroArchEmulator is RetroArch's discovery entry. Its launchers come from
// the shared RetroArch core map rather than the standalone table.
func retroArchEmulator() standalone.WindowsEmulator {
	return standalone.WindowsEmulator{
		Name:  retroArchName,
		Exes:  []string{"retroarch.exe"},
		Scoop: []string{"retroarch"},
	}
}

// standaloneLaunchers returns launchers for every emulator installed on this
// PC, leaving out IDs a custom launcher already uses.
func (p *Platform) standaloneLaunchers(cfg *config.Instance, taken func(id string) bool) []platforms.Launcher {
	emulators := standalone.WindowsEmulators()
	all := append(append(make([]standalone.WindowsEmulator, 0, len(emulators)+1), emulators...),
		retroArchEmulator())
	installs := standalone.DiscoverWindows(windowsStandaloneEnv(cfg), all)

	launchers := standalone.WindowsLaunchers(emulators, installs, taken, hideConsoleWindow)
	retroArch, ok := installs[retroArchName]
	if !ok {
		if dir, found := steam.FindInstallDirByAppID(retroArchSteamAppID); found {
			exe := filepath.Join(dir, "retroarch.exe")
			if info, err := os.Stat(exe); err == nil && info.Mode().IsRegular() {
				retroArch, ok = standalone.WindowsInstall{Exe: exe, Dir: dir, Source: "steam"}, true
			}
		}
	}
	if ok {
		launchers = append(launchers, retroArchLaunchers(helpers.DataDir(p), retroArch, taken)...)
	}
	return launchers
}

func retroArchLaunchers(
	dataDir string,
	install standalone.WindowsInstall,
	taken func(id string) bool,
) []platforms.Launcher {
	overlay := filepath.Join(dataDir, retroArchNetworkConfig)
	opts := sharedretroarch.Options{
		Exec:             []string{install.Exe},
		ExtraArgs:        []string{"--fullscreen"},
		CoresDir:         filepath.Join(install.Dir, "cores"),
		AppendConfigPath: overlay,
		NetworkCmdAddr:   retroArchNetworkAddr,
		Windows:          true,
		Preflight: sharedretroarch.MemoizePreflight(func(string) error {
			return sharedretroarch.EnsureNetworkCommandConfig(nil, overlay)
		}),
	}
	cores := sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop)
	launchers := make([]platforms.Launcher, 0, len(cores))
	for i := range cores {
		if taken != nil && taken(cores[i].ID) {
			continue
		}
		launcher := sharedretroarch.NewLauncher(opts, cores[i])
		launcher.Groups = append(launcher.Groups, platformshared.LauncherGroupNative)
		launchers = append(launchers, launcher)
	}
	return launchers
}

// hideConsoleWindow stops console emulators from opening a console window
// next to their game window. Windowed programs ignore the flag.
func hideConsoleWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

func windowsStandaloneEnv(cfg *config.Instance) *standalone.WindowsEnv {
	var roots []string
	if cfg != nil {
		roots = cfg.IndexRoots()
	}
	return &standalone.WindowsEnv{
		Getenv: os.Getenv,
		Configured: func(id string) string {
			if cfg == nil {
				return ""
			}
			return cfg.LookupLauncherDefaults(id, nil).InstallDir
		},
		AppPath:          appPath,
		UninstallEntries: uninstallEntries(),
		FixedDrives:      fixedDrives(),
		IndexRoots:       roots,
	}
}

const (
	appPathsKey  = `Software\Microsoft\Windows\CurrentVersion\App Paths\`
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	// wow64UninstallKey holds 32-bit programs on 64-bit Windows.
	wow64UninstallKey = `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
)

// appPath reads the App Paths registration of an executable.
func appPath(exe string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, appPathsKey+exe, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue("")
		_ = key.Close()
		if err == nil && value != "" {
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

// uninstallEntries lists installed programs from the per-user and
// machine-wide Uninstall keys.
func uninstallEntries() []standalone.UninstallEntry {
	locations := []struct {
		path string
		root registry.Key
	}{
		{root: registry.CURRENT_USER, path: uninstallKey},
		{root: registry.LOCAL_MACHINE, path: uninstallKey},
		{root: registry.LOCAL_MACHINE, path: wow64UninstallKey},
	}
	var entries []standalone.UninstallEntry
	for _, location := range locations {
		key, err := registry.OpenKey(location.root, location.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, err := key.ReadSubKeyNames(-1)
		_ = key.Close()
		if err != nil {
			log.Debug().Err(err).Str("key", location.path).Msg("failed to list uninstall entries")
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(location.root, location.path+`\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			var entry standalone.UninstallEntry
			entry.DisplayName, _, _ = sub.GetStringValue("DisplayName")
			entry.InstallLocation, _, _ = sub.GetStringValue("InstallLocation")
			entry.DisplayIcon, _, _ = sub.GetStringValue("DisplayIcon")
			_ = sub.Close()
			if entry.InstallLocation != "" || entry.DisplayIcon != "" {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

// fixedDrives returns the roots of local fixed drives. Network, removable
// and optical drives are left out so discovery never waits on them.
func fixedDrives() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var drives []string
	for i := range 26 {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		rootPtr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(rootPtr) == windows.DRIVE_FIXED {
			drives = append(drives, root)
		}
	}
	return drives
}
