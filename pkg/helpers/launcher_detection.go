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

package helpers

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"

type LauncherDetectionResult uint8

const (
	LauncherDetectionUnscanned LauncherDetectionResult = iota
	LauncherDetectionNone
	LauncherDetectionUnique
	LauncherDetectionAmbiguous
)

// LauncherKnownMissing reports a launcher whose platform looked for its player
// and did not find it. An unscanned launcher is not known to be missing.
func LauncherKnownMissing(launcher *platforms.Launcher) bool {
	return launcher.Detected != nil && !*launcher.Detected
}

// SelectDetectedLauncher resolves optional host discovery metadata without
// treating an unscanned launcher as installed or as absent.
func SelectDetectedLauncher(candidates []platforms.Launcher) (platforms.Launcher, LauncherDetectionResult) {
	scanned := false
	matches := make([]platforms.Launcher, 0, len(candidates))
	for i := range candidates {
		if candidates[i].Detected == nil {
			continue
		}
		scanned = true
		if *candidates[i].Detected {
			matches = append(matches, candidates[i])
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], LauncherDetectionUnique
	case len(matches) > 1:
		return platforms.Launcher{}, LauncherDetectionAmbiguous
	case scanned:
		return platforms.Launcher{}, LauncherDetectionNone
	default:
		return platforms.Launcher{}, LauncherDetectionUnscanned
	}
}
