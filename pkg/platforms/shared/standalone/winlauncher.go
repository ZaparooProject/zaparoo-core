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

package standalone

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// WindowsLaunchers returns a launcher for every system of every discovered
// emulator, in table order, leaving out IDs for which skip returns true.
// prepare, if set, adjusts each command before it starts.
func WindowsLaunchers(
	emulators []WindowsEmulator,
	installs map[string]WindowsInstall,
	skip func(id string) bool,
	prepare func(cmd *exec.Cmd),
) []platforms.Launcher {
	result := make([]platforms.Launcher, 0, len(installs)*2)
	for i := range emulators {
		install, ok := installs[emulators[i].Name]
		if !ok {
			continue
		}
		for j := range emulators[i].Systems {
			if skip != nil && skip(emulators[i].Systems[j].ID) {
				continue
			}
			result = append(result, NewWindowsLauncher(&emulators[i], &emulators[i].Systems[j], install, prepare))
		}
	}
	return result
}

// NewWindowsLauncher builds the launcher for one emulator system.
func NewWindowsLauncher(
	emu *WindowsEmulator,
	system *WindowsSystem,
	install WindowsInstall,
	prepare func(cmd *exec.Cmd),
) platforms.Launcher {
	showConfig := func(command *platforms.LaunchCommand) ([]byte, error) {
		return MAMEShowConfig(command, prepare)
	}
	build := func(path string) (*platforms.LaunchCommand, error) {
		return BuildWindowsCommand(emu, system, install, path, showConfig)
	}
	launcher := platforms.Launcher{
		ID:                 system.ID,
		SystemID:           system.SystemID,
		Groups:             []string{platformshared.LauncherGroupNative},
		Lifecycle:          platforms.LifecycleBlocking,
		SkipFilesystemScan: system.NoScan,
		Folders:            LauncherFolders(system),
		Extensions:         LauncherExtensions(system),
		Availability: func(*config.Instance) error {
			info, err := os.Stat(install.Exe)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("%s is no longer at %s", emu.Name, install.Exe)
			}
			return nil
		},
		BuildLaunchCommand: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (
			*platforms.LaunchCommand, error,
		) {
			return build(path)
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			spec, err := build(path)
			if err != nil {
				return nil, err
			}
			//nolint:gosec // Executable comes from discovery and arguments from built-in definitions.
			cmd := exec.CommandContext(context.Background(), spec.Executable, spec.Args...)
			cmd.Dir = spec.Dir
			if prepare != nil {
				prepare(cmd)
			}
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("launch %s: %w", emu.Name, err)
			}
			return cmd.Process, nil
		},
	}
	return launcher
}

// BuildWindowsCommand builds the command line for launching path. For MAME
// builds, showConfig runs the command it is given and returns the output.
func BuildWindowsCommand(
	emu *WindowsEmulator,
	system *WindowsSystem,
	install WindowsInstall,
	path string,
	showConfig func(command *platforms.LaunchCommand) ([]byte, error),
) (*platforms.LaunchCommand, error) {
	if err := ValidateMediaFile(path); err != nil {
		return nil, err
	}
	ctx := &Context{Path: path, InstallDir: install.Dir, Exe: install.Exe}
	if emu.MAMEConfig && showConfig != nil {
		ctx.MAMEConfig = func() ([]byte, error) {
			return showConfig(&platforms.LaunchCommand{
				Executable: install.Exe,
				Args:       []string{"-showconfig"},
				Dir:        install.Dir,
			})
		}
	}
	args, err := system.Args(ctx)
	if err != nil {
		return nil, err
	}
	spec := &platforms.LaunchCommand{Executable: install.Exe, Args: args}
	if emu.WorkDirInstall {
		spec.Dir = install.Dir
	}
	return spec, nil
}

// LauncherFolders returns the system's folders plus its Zaparoo system ID
// folder, without case-insensitive duplicates.
func LauncherFolders(system *WindowsSystem) []string {
	folders := make([]string, 0, len(system.Folders)+1)
	seen := make(map[string]bool, len(system.Folders)+1)
	for _, folder := range append(append([]string(nil), system.Folders...), system.SystemID) {
		key := strings.ToLower(folder)
		if folder == "" || seen[key] {
			continue
		}
		seen[key] = true
		folders = append(folders, folder)
	}
	return folders
}

// LauncherExtensions returns the system's explicit extensions, or the union
// of its folders' ES-DE extensions and its extra extensions.
func LauncherExtensions(system *WindowsSystem) []string {
	if len(system.Extensions) > 0 {
		return append([]string(nil), system.Extensions...)
	}
	var extensions []string
	seen := make(map[string]bool)
	add := func(values []string) {
		for _, ext := range values {
			key := strings.ToLower(ext)
			if !seen[key] {
				seen[key] = true
				extensions = append(extensions, ext)
			}
		}
	}
	for _, folder := range system.Folders {
		if info, ok := esde.LookupByFolderName(folder); ok {
			add(info.Extensions)
		}
	}
	add(system.ExtraExtensions)
	return extensions
}
