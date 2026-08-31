//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

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

package cores

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/catalog"
)

type (
	MGLParams = catalog.MGLParams
	Slot      = catalog.Slot
	Core      = catalog.Core
)

// ErrLaunchesDirectly is retained here for the same compatibility reason as the
// type aliases above.
var ErrLaunchesDirectly = catalog.ErrLaunchesDirectly

// Systems is retained for compatibility. Canonical definitions live in the
// dependency-light catalog package.
var Systems = func() map[string]Core {
	all := catalog.All()
	systems := make(map[string]Core, len(all))
	for _, core := range all {
		systems[core.ID] = core
	}
	return systems
}()

// CoreGroups is retained for compatibility. Canonical groups live in catalog.
var CoreGroups = catalog.Groups()

func PathToMGLDef(system *Core, path string) (*MGLParams, error) {
	return catalog.PathToMGLDef(system, path) //nolint:wrapcheck // Compatibility wrapper preserves the catalog error.
}

// GetCore looks up an exact system definition by ID.
func GetCore(id string) (*Core, error) {
	if system, ok := Systems[id]; ok {
		return &system, nil
	}
	return nil, fmt.Errorf("unknown system: %s", id)
}

func GetGroup(groupID string) (Core, error) {
	var merged Core
	if _, ok := CoreGroups[groupID]; !ok {
		return merged, fmt.Errorf("no system group found for %s", groupID)
	}

	if len(CoreGroups[groupID]) < 1 {
		return merged, fmt.Errorf("no systems in %s", groupID)
	} else if len(CoreGroups[groupID]) == 1 {
		return CoreGroups[groupID][0], nil
	}

	merged = CoreGroups[groupID][0]
	merged.Slots = make([]Slot, 0)
	for i := range CoreGroups[groupID] {
		merged.Slots = append(merged.Slots, CoreGroups[groupID][i].Slots...)
	}

	return merged, nil
}

// LookupCore case-insensitively looks up system ID definition.
func LookupCore(id string) (*Core, error) {
	if system, err := GetGroup(id); err == nil {
		return &system, nil
	}

	for key := range Systems {
		if strings.EqualFold(key, id) {
			core := Systems[key]
			return &core, nil
		}
	}

	return nil, fmt.Errorf("unknown system: %s", id)
}
