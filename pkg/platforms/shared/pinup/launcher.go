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
package pinup

import (
	"context"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// NewLauncher builds the PinUP Popper launcher around an integration. The
// lifecycle is external: Popper starts and owns the emulator, and the
// integration publishes ActiveMedia once it has adopted that process.
func NewLauncher(i *Integration) platforms.Launcher {
	return platforms.Launcher{
		ID:                 LauncherID,
		SystemID:           systemdefs.SystemPinball,
		Schemes:            []string{shared.SchemePopper},
		Lifecycle:          platforms.LifecycleExternal,
		SkipFilesystemScan: true,
		Availability:       i.Available,
		// A scheme match alone would select this launcher for any popper://
		// path, and DoLaunch stops the running table before handing the path
		// over. Rejecting an unlaunchable ID here keeps selection from
		// reaching that point, so a bad scan leaves the current table alone.
		Test: func(_ *config.Instance, path string) bool {
			_, err := ParseTablePath(path)
			return err == nil
		},
		Scanner: func(
			ctx context.Context, cfg *config.Instance, systemID string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			if systemID != systemdefs.SystemPinball {
				return results, nil
			}
			tables, err := i.Scan(ctx, cfg)
			if err != nil {
				return results, err
			}
			return append(results, tables...), nil
		},
		Launch: func(cfg *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			if err := i.Launch(cfg, path); err != nil {
				return nil, err
			}
			return nil, nil //nolint:nilnil // Popper owns the process; the integration adopts it later.
		},
		Kill: func(*config.Instance) error {
			return i.StopTable()
		},
	}
}
