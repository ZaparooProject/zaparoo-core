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

package steam

import (
	"context"
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// FindSteamDir locates the Steam installation directory on Windows using the Registry.
func (c *Client) FindSteamDir(cfg *config.Instance) string {
	// Check for user-configured Steam install directory first
	if def := cfg.LookupLauncherDefaults("Steam", nil); def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// registrySteamPaths is the single place the Steam root is read from the
	// registry, so the scanner and the game tracker cannot disagree about
	// where Steam is. It covers the per-user HKCU install as well as the
	// machine-wide HKLM keys.
	for _, installPath := range registrySteamPaths() {
		if _, statErr := os.Stat(installPath); statErr == nil {
			log.Debug().Msgf("found Steam installation via registry: %s", installPath)
			return installPath
		}
	}

	log.Debug().Msgf("Steam registry detection failed, using fallback: %s", c.opts.FallbackPath)
	return c.opts.FallbackPath
}

// Launch launches a Steam game on Windows using the start command.
func (c *Client) Launch(
	_ *config.Instance, path string, launchOpts *platforms.LaunchOptions,
) (*os.Process, error) {
	id, err := ExtractAndValidateID(path)
	if err != nil {
		return nil, err
	}

	// Get action from opts (already resolved by DoLaunch from advargs or config)
	action := ""
	if launchOpts != nil {
		action = launchOpts.Action
	}

	// Build the appropriate Steam URL based on the action
	var steamURL string
	if platforms.IsActionDetails(action) {
		steamURL = BuildSteamDetailsURL(id)
	} else {
		steamURL = BuildSteamURL(id)
	}

	// On Windows, we use "cmd /c start <url>" to open Steam URLs
	// HideWindow prevents a console window from flashing on screen
	cmdOpts := command.StartOptions{HideWindow: true}
	err = c.cmd.StartWithOptions(context.Background(), cmdOpts, helpers.ComSpec(), "/c", "start", steamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to start Steam: %w", err)
	}
	return nil, nil //nolint:nilnil // Steam launches are fire-and-forget
}
