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

// SelectDetectedLauncher resolves optional host discovery metadata without
// treating catalog order as installation evidence.
func SelectDetectedLauncher(candidates []platforms.Launcher) (platforms.Launcher, LauncherDetectionResult) {
	return SelectPreferredDetectedLauncher(candidates, nil)
}

// SelectPreferredDetectedLauncher resolves discovery metadata using an
// explicit, reviewed preference list when more than one launcher is detected.
// Candidate or registration order is never a preference signal.
func SelectPreferredDetectedLauncher(
	candidates []platforms.Launcher, preferredIDs []string,
) (platforms.Launcher, LauncherDetectionResult) {
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
	if len(matches) == 1 {
		return matches[0], LauncherDetectionUnique
	}
	if len(matches) > 1 {
		if preferred, ok := SelectPreferredLauncher(matches, preferredIDs); ok {
			return preferred, LauncherDetectionUnique
		}
		return platforms.Launcher{}, LauncherDetectionAmbiguous
	}
	if scanned {
		return platforms.Launcher{}, LauncherDetectionNone
	}
	return platforms.Launcher{}, LauncherDetectionUnscanned
}

// SelectPreferredLauncher returns the first candidate named by an explicit
// preference list. It deliberately has no candidate-order fallback.
func SelectPreferredLauncher(
	candidates []platforms.Launcher, preferredIDs []string,
) (platforms.Launcher, bool) {
	for _, preferredID := range preferredIDs {
		for i := range candidates {
			if candidates[i].ID == preferredID {
				return candidates[i], true
			}
		}
	}
	return platforms.Launcher{}, false
}
