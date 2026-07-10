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
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
)

const NetworkCommandConfig = "network_cmd_enable = \"true\"\nnetwork_cmd_port = \"55355\"\n"

// EnsureNetworkCommandConfig writes Core's minimal RetroArch network-command
// overlay without changing the user's primary RetroArch configuration.
func EnsureNetworkCommandConfig(fs afero.Fs, path string) error {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	if err := fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create RetroArch config directory: %w", err)
	}
	if err := afero.WriteFile(fs, path, []byte(NetworkCommandConfig), 0o600); err != nil {
		return fmt.Errorf("write RetroArch network config: %w", err)
	}
	return nil
}
