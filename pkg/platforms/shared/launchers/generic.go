//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package launchers

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// NewGenericLauncher creates a launcher for shell scripts (.sh files).
// The launched process is tracked for lifecycle management.
func NewGenericLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:            "Generic",
		Extensions:    []string{".sh"},
		AllowListOnly: true,
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			cmd := exec.CommandContext(context.Background(), path)
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to start command: %w", err)
			}
			// Generic launcher can be tracked - return process for lifecycle management
			return cmd.Process, nil
		},
	}
}
