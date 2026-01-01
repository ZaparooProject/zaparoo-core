//go:build linux

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

package batocera

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// fromBatoceraSystem converts a Batocera/ES system folder name to a Zaparoo system ID.
func fromBatoceraSystem(batoceraSystem string) (string, error) {
	systemID, err := esde.GetSystemID(batoceraSystem)
	if err != nil {
		return "", fmt.Errorf("unknown batocera system %s: %w", batoceraSystem, err)
	}
	return systemID, nil
}

// toBatoceraSystems returns all ES folder names that map to a given Zaparoo system ID.
func toBatoceraSystems(zaparooSystem string) ([]string, error) {
	return esde.GetFoldersForSystemID(zaparooSystem), nil
}
