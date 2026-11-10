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

package esbase

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// ServiceInstaller writes the file that starts Core with the system and
// registers it with the system's service manager.
type ServiceInstaller struct {
	FS  afero.Fs
	Cmd command.Executor
	// Path is where the service file goes, and Content what it holds.
	Path    string
	Content string
	// Stop runs before Enable so a reinstall replaces a running service
	// instead of leaving the old binary up; failures are ignored because the
	// service may not be running.
	Stop [][]string
	// Enable runs in order after the file is written; each must succeed.
	Enable [][]string
	// Disable runs before the file is removed; failures are ignored because
	// the service may not be running or registered.
	Disable [][]string
	// Reload runs after the file is removed.
	Reload [][]string
	Mode   os.FileMode
}

// Install writes the service file and enables it.
func (s *ServiceInstaller) Install(ctx context.Context) error {
	if err := s.FS.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	if err := afero.WriteFile(s.FS, s.Path, []byte(s.Content), s.Mode); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	for _, args := range s.Stop {
		if err := s.Cmd.Run(ctx, args[0], args[1:]...); err != nil {
			log.Debug().Err(err).Strs("command", args).Msg("stopping service before install failed")
		}
	}
	for _, args := range s.Enable {
		if err := s.Cmd.Run(ctx, args[0], args[1:]...); err != nil {
			// An enabled service that failed to start would still come up
			// at the next boot, so a failed install is undone.
			if rbErr := s.Uninstall(ctx); rbErr != nil {
				log.Warn().Err(rbErr).Msg("removing service after a failed install failed")
			}
			return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// Uninstall disables the service and removes its file.
func (s *ServiceInstaller) Uninstall(ctx context.Context) error {
	for _, args := range s.Disable {
		if err := s.Cmd.Run(ctx, args[0], args[1:]...); err != nil {
			log.Debug().Err(err).Strs("command", args).Msg("disabling service failed")
		}
	}
	if err := s.FS.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove service file: %w", err)
	}
	for _, args := range s.Reload {
		if err := s.Cmd.Run(ctx, args[0], args[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// SystemdUnitInstaller installs unit as a system service named name at
// path, enabled and (re)started so a reinstall runs the new binary.
func SystemdUnitInstaller(path, name, unit string) *ServiceInstaller {
	return &ServiceInstaller{
		FS:      afero.NewOsFs(),
		Cmd:     &command.RealExecutor{},
		Path:    path,
		Content: unit,
		Mode:    0o644,
		Enable: [][]string{
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", name},
			{"systemctl", "restart", name},
		},
		Disable: [][]string{
			{"systemctl", "stop", name},
			{"systemctl", "disable", name},
		},
		Reload: [][]string{
			{"systemctl", "daemon-reload"},
		},
	}
}

// CheckInstallPath refuses to install a service that would start a binary
// other than exe. Both paths are compared after resolving symlinks.
func CheckInstallPath(exe, want string) error {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	resolvedWant := want
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		resolvedWant = resolved
	}
	if exe != resolvedWant {
		return fmt.Errorf("the service runs %s, but this binary is %s: move it there first", want, exe)
	}
	return nil
}

// CheckRunningInstallPath is CheckInstallPath for the running binary.
func CheckRunningInstallPath(want string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error finding this binary: %w", err)
	}
	return CheckInstallPath(exe, want)
}
