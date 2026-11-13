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
	"io"
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

// Install writes the service file and enables it. When enabling fails, a
// service this call created is removed again, while one that was already
// installed gets its previous file back rather than being removed.
func (s *ServiceInstaller) Install(ctx context.Context) error {
	if err := s.checkCommands(); err != nil {
		return err
	}
	previous, readErr := afero.ReadFile(s.FS, s.Path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return fmt.Errorf("read existing service file: %w", readErr)
	}

	if err := s.FS.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	if err := afero.WriteFile(s.FS, s.Path, []byte(s.Content), s.mode()); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	for _, args := range s.Stop {
		if err := s.Cmd.Run(ctx, args[0], args[1:]...); err != nil {
			log.Debug().Err(err).Strs("command", args).Msg("stopping service before install failed")
		}
	}
	for _, args := range s.Enable {
		err := s.Cmd.Run(ctx, args[0], args[1:]...)
		if err == nil {
			continue
		}
		if existed {
			// The service was installed before this call, so it is kept
			// with its previous file instead of being taken away.
			if rbErr := afero.WriteFile(s.FS, s.Path, previous, s.mode()); rbErr != nil {
				log.Warn().Err(rbErr).Msg("restoring the previous service file failed")
			}
		} else if rbErr := s.Uninstall(ctx); rbErr != nil {
			// An enabled service that failed to start would still come up
			// at the next boot, so a new install that failed is undone.
			log.Warn().Err(rbErr).Msg("removing service after a failed install failed")
		}
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Uninstall disables the service and removes its file.
func (s *ServiceInstaller) Uninstall(ctx context.Context) error {
	if err := s.checkCommands(); err != nil {
		return err
	}
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

// InstallCommand runs the -install flag: it installs the service when the
// running binary is binaryPath, the one the service starts, and tells the
// user on out.
func (s *ServiceInstaller) InstallCommand(ctx context.Context, binaryPath string, out io.Writer) error {
	if err := CheckRunningInstallPath(binaryPath); err != nil {
		return err
	}
	if err := s.Install(ctx); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "Zaparoo service installed and started.")
	return nil
}

// UninstallCommand runs the -uninstall flag and tells the user on out.
func (s *ServiceInstaller) UninstallCommand(ctx context.Context, out io.Writer) error {
	if err := s.Uninstall(ctx); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "Zaparoo service uninstalled.")
	return nil
}

// errEmptyServiceCommand means a service command list holds an empty
// command, which has no program to run.
var errEmptyServiceCommand = errors.New("empty service command")

func (s *ServiceInstaller) checkCommands() error {
	for _, list := range [][][]string{s.Stop, s.Enable, s.Disable, s.Reload} {
		for _, args := range list {
			if len(args) == 0 || args[0] == "" {
				return errEmptyServiceCommand
			}
		}
	}
	return nil
}

func (s *ServiceInstaller) mode() os.FileMode {
	if s.Mode == 0 {
		return 0o644
	}
	return s.Mode
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
