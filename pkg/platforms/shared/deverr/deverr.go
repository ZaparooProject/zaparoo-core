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

package deverr

import (
	"context"
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// DevErr launchers provide predictable failures for indexing and launcher tests.

var deverrLaunchFn = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
	return nil, fmt.Errorf("DevErr Path Launched (Error Expected) %s", path)
}

func NewDevErrSystemLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:         "DevErrSystem",
		SystemID:   systemdefs.SystemDevErr,
		Extensions: []string{".deverr"},
		Launch:     deverrLaunchFn,
		Folders:    []string{"deverr"},
	}
}

func NewDevErrAnyLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:                 "DevErrAny",
		Extensions:         []string{".deverr"},
		Launch:             deverrLaunchFn,
		SkipFilesystemScan: true,
		Scanner: func(
			_ context.Context,
			_ *config.Instance,
			systemdID string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			if systemdID != systemdefs.SystemDevErr {
				return results, nil
			}
			results = append(results, platforms.ScanResult{
				Path: "deverr://Any System Scanner - DevErr Result (USA) (!).deverr",
				Name: "Any System Scanner - DevErr Result (USA) (!)",
			})
			return results, nil
		},
	}
}

func GetDevErrLaunchers() []platforms.Launcher {
	return []platforms.Launcher{
		NewDevErrSystemLauncher(),
		NewDevErrAnyLauncher(),
	}
}
