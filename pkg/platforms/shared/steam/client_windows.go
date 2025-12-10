//go:build windows

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

package steam

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

// FindSteamDir locates the Steam installation directory on Windows using the Registry.
func (c *Client) FindSteamDir(cfg *config.Instance) string {
	// Check for user-configured Steam install directory first
	if def, ok := cfg.LookupLauncherDefaults("Steam"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try 64-bit systems first (most common)
	paths := []string{
		`SOFTWARE\Wow6432Node\Valve\Steam`,
		`SOFTWARE\Valve\Steam`,
	}

	for _, path := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		installPath, _, err := key.GetStringValue("InstallPath")
		if closeErr := key.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing registry key")
		}
		if err != nil {
			continue
		}

		// Validate the path exists
		if _, statErr := os.Stat(installPath); statErr == nil {
			log.Debug().Msgf("found Steam installation via registry: %s", installPath)
			return installPath
		}
	}

	log.Debug().Msgf("Steam registry detection failed, using fallback: %s", c.opts.FallbackPath)
	return c.opts.FallbackPath
}

// Launch launches a Steam game on Windows using the start command.
// NOTE: Uses exec.Command directly (not c.cmd) because Windows requires
// the HideWindow syscall attribute, which CommandExecutor doesn't support.
func (c *Client) Launch(_ *config.Instance, path string) (*os.Process, error) {
	id, err := ExtractAndValidateID(path)
	if err != nil {
		return nil, err
	}

	steamURL := BuildSteamURL(id)

	// On Windows, we use "cmd /c start <url>" to open Steam URLs
	// Must use direct exec.Command to set HideWindow attribute
	cmd := exec.CommandContext( //nolint:gosec // Steam ID validated as numeric-only by ExtractAndValidateID
		context.Background(),
		"cmd", "/c", "start", steamURL,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Steam: %w", err)
	}
	return nil, nil //nolint:nilnil // Steam launches are fire-and-forget
}
