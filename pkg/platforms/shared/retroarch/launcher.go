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

package retroarch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
)

// NewLauncher creates a blocking RetroArch launcher.
func NewLauncher(opts Options, c CoreLaunch) platforms.Launcher { //nolint:gocritic // Public value API.
	opts = cloneOptions(&opts)
	c = cloneCoreLaunch(&c)
	launcher := platforms.Launcher{
		ID:        c.ID,
		SystemID:  c.SystemID,
		Lifecycle: platforms.LifecycleBlocking,
		Controls:  Controls(opts.NetworkCmdAddr),
	}
	if c.Scan {
		launcher.Folders = c.Folders
		launcher.Extensions = c.Extensions
	}

	launcher.Availability = func(cfg *config.Instance) error {
		core, err := resolveCore(cfg, &c)
		if err != nil {
			return err
		}
		return validateLaunch(&opts, filepath.Join(opts.CoresDir, core))
	}
	launcher.Launch = func(cfg *config.Instance, mediaPath string, _ *platforms.LaunchOptions) (*os.Process, error) {
		core, err := resolveCore(cfg, &c)
		if err != nil {
			return nil, err
		}
		corePath := filepath.Join(opts.CoresDir, core)
		if err := validateLaunch(&opts, corePath); err != nil {
			return nil, err
		}

		spec := buildCommandWithCore(&opts, core, mediaPath)
		if spec.Name == "" {
			return nil, errors.New("retroarch executable is not configured")
		}

		//nolint:gosec // command argv comes from built-in platform configuration
		cmd := exec.CommandContext(context.Background(), spec.Name, spec.Args...)
		cmd.Env = append(os.Environ(), spec.Env...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start retroarch: %w", err)
		}
		return cmd.Process, nil
	}

	if opts.NetworkCmdAddr != "" {
		launcher.Kill = func(_ *config.Instance) error {
			return sendCommand(context.Background(), opts.NetworkCmdAddr, commandQuit)
		}
	}

	return launcher
}

// NewLaunchers creates launchers in the same order as cs.
func NewLaunchers(opts Options, cs []CoreLaunch) []platforms.Launcher { //nolint:gocritic // Public value API.
	launchers := make([]platforms.Launcher, 0, len(cs))
	for i := range cs {
		launchers = append(launchers, NewLauncher(opts, cs[i]))
	}
	return launchers
}

func resolveCore(cfg *config.Instance, c *CoreLaunch) (string, error) {
	core := c.Core
	if cfg != nil {
		if override := cfg.LookupLauncherDefaults(c.ID, nil).LoadPath; override != "" {
			core = override
		}
	}

	resolved, err := normalizeCoreFilename(core)
	if err != nil {
		return "", fmt.Errorf("invalid RetroArch core override for %s: %w", c.ID, err)
	}
	return resolved, nil
}

func normalizeCoreFilename(core string) (string, error) {
	if core == "" {
		return "", errors.New("core name is empty")
	}
	if filepath.Base(core) != core || core == "." || core == ".." {
		return "", fmt.Errorf("core must be a filename, got %q", core)
	}

	switch {
	case strings.HasSuffix(core, "_libretro.so"):
		return core, nil
	case strings.HasSuffix(core, "_libretro"):
		return core + ".so", nil
	case filepath.Ext(core) == "":
		return core + "_libretro.so", nil
	default:
		return "", fmt.Errorf("core must use a _libretro.so filename, got %q", core)
	}
}

func validateLaunch(opts *Options, corePath string) error {
	if len(opts.Exec) == 0 || opts.Exec[0] == "" {
		return errors.New("retroarch executable is not configured")
	}

	fs := opts.FS
	if fs == nil {
		fs = afero.NewOsFs()
	}

	executable := opts.Exec[0]
	if filepath.IsAbs(executable) {
		info, err := fs.Stat(executable)
		if err != nil {
			return fmt.Errorf("retroarch is not installed at %s: %w", executable, err)
		}
		if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("retroarch executable is not executable: %s", executable)
		}
	}

	if opts.Preflight != nil {
		if err := opts.Preflight(corePath); err != nil {
			return fmt.Errorf("retroarch preflight: %w", err)
		}
	}

	info, err := fs.Stat(corePath)
	if err != nil {
		return fmt.Errorf("retroarch core is not installed at %s: %w", corePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("retroarch core path is not a file: %s", corePath)
	}
	return nil
}
