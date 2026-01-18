//go:build darwin

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
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// FindSteamDir locates the Steam installation directory on macOS.
func (c *Client) FindSteamDir(cfg *config.Instance) string {
	// Check for user-configured Steam install directory first
	if def := cfg.LookupLauncherDefaults("Steam", nil); def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try common macOS Steam installation paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get user home directory")
		return c.opts.FallbackPath
	}

	paths := []string{
		filepath.Join(home, "Library", "Application Support", "Steam"),
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			log.Debug().Msgf("found Steam installation: %s", path)
			return path
		}
	}

	log.Debug().Msgf("Steam detection failed, using fallback: %s", c.opts.FallbackPath)
	return c.opts.FallbackPath
}

// Launch launches a Steam game on macOS using the open command.
func (c *Client) Launch(
	_ *config.Instance, path string, opts *platforms.LaunchOptions,
) (*os.Process, error) {
	id, err := ExtractAndValidateID(path)
	if err != nil {
		return nil, err
	}

	action := ""
	if opts != nil {
		action = opts.Action
	}

	// Build the appropriate Steam URL based on the action
	var steamURL string
	if platforms.IsActionDetails(action) {
		steamURL = BuildSteamDetailsURL(id)
	} else {
		steamURL = BuildSteamURL(id)
	}

	// On macOS, we use "open" to open Steam URLs
	if err := c.cmd.Start(context.Background(), "open", steamURL); err != nil {
		return nil, fmt.Errorf("failed to launch Steam: %w", err)
	}
	return nil, nil //nolint:nilnil // Steam launches are fire-and-forget
}
