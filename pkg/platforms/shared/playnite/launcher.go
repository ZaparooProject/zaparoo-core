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

package playnite

import (
	"context"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// NewLauncher builds the Playnite launcher around an integration. It has no
// system of its own: Playnite holds games for many systems, so the scanner
// runs for each and returns the games Playnite files under it. The lifecycle
// is external: Playnite starts and owns the game, and the integration
// publishes ActiveMedia when the extension reports it running.
func NewLauncher(i *Integration) platforms.Launcher {
	return platforms.Launcher{
		ID:           LauncherID,
		Schemes:      []string{shared.SchemePlaynite},
		Lifecycle:    platforms.LifecycleExternal,
		Availability: i.Available,
		// A scheme match alone would select this launcher for any playnite://
		// path, and DoLaunch stops the running media before handing the path
		// over. Rejecting an unlaunchable ID here keeps selection from
		// reaching that point, so a bad scan leaves the current game alone.
		Test: func(_ *config.Instance, path string) bool {
			_, err := ParseGamePath(path)
			return err == nil
		},
		Scanner: func(
			ctx context.Context, cfg *config.Instance, systemID string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			games, err := i.Scan(ctx, cfg, systemID)
			if err != nil {
				return results, err
			}
			return append(results, games...), nil
		},
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			if err := i.Launch(cfg, path); err != nil {
				return nil, err
			}
			return nil, nil //nolint:nilnil // Playnite owns the process; the integration adopts it later.
		},
		Kill: func(*config.Instance) error {
			return i.StopGame()
		},
	}
}
