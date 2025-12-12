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

// NewWebBrowserLauncher creates a launcher for opening URLs in the default browser.
func NewWebBrowserLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:        "WebBrowser",
		Schemes:   []string{"http", "https"},
		Lifecycle: platforms.LifecycleFireAndForget,
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			cmd := exec.CommandContext(context.Background(), "xdg-open", path)
			err := cmd.Start()
			if err != nil {
				return nil, fmt.Errorf("failed to open URL in browser: %w", err)
			}
			return nil, nil
		},
	}
}
