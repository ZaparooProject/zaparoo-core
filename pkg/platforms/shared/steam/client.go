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
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// Client implements SteamClient for Steam game launching and scanning.
type Client struct {
	cmd  command.Executor
	opts Options
}

// NewClient creates a new Steam client with the given options.
func NewClient(opts Options) *Client {
	return &Client{
		opts: opts,
		cmd:  &command.RealExecutor{},
	}
}

// NewClientWithExecutor creates a new Steam client with a custom command executor.
// This is useful for testing.
func NewClientWithExecutor(opts Options, cmd command.Executor) *Client {
	return &Client{
		opts: opts,
		cmd:  cmd,
	}
}

// Compile-time interface implementation check.
var _ SteamClient = (*Client)(nil)

// IsSteamInstalled checks if Steam is installed by verifying the Steam directory exists.
// Uses FindSteamDir to locate the directory, respecting config overrides.
func (c *Client) IsSteamInstalled(cfg *config.Instance) bool {
	steamDir := c.FindSteamDir(cfg)
	if steamDir == "" {
		return false
	}
	_, err := os.Stat(steamDir)
	return err == nil
}

// NormalizePath normalizes Steam URL formats to the standard virtual path format.
// Converts "steam://rungameid/123" to "steam://123".
func NormalizePath(path string) string {
	if strings.HasPrefix(path, "steam://rungameid/") {
		return strings.Replace(path, "steam://rungameid/", "steam://", 1)
	}
	return path
}

// ExtractAndValidateID extracts and validates the Steam game ID from a virtual path.
// Returns the numeric ID or an error if the path is invalid or ID is non-numeric.
func ExtractAndValidateID(path string) (string, error) {
	// Normalize the path first
	path = NormalizePath(path)

	id, err := virtualpath.ExtractSchemeID(path, shared.SchemeSteam)
	if err != nil {
		return "", fmt.Errorf("failed to extract Steam game ID from path: %w", err)
	}

	// Validate that the ID is numeric (security check)
	if _, parseErr := strconv.ParseUint(id, 10, 64); parseErr != nil {
		return "", fmt.Errorf("invalid Steam game ID: %s", id)
	}

	return id, nil
}

// BuildSteamURL builds a Steam launch URL from a game ID.
func BuildSteamURL(id string) string {
	return "steam://rungameid/" + id
}

// BuildSteamDetailsURL builds a Steam details page URL from a game ID.
// This opens the game's details page in the Steam client library.
func BuildSteamDetailsURL(id string) string {
	return "steam://nav/games/details/" + id
}
