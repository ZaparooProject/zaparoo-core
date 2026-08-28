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

// Package catalog contains dependency-free MiSTer core and media definitions.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type MGLParams struct {
	Method     string `json:"method"`
	Delay      int    `json:"delay,omitempty"`
	Index      int    `json:"index,omitempty"`
	ResetDelay int    `json:"resetDelay,omitempty"`
	ResetHold  int    `json:"resetHold,omitempty"`
}

type Slot struct {
	Mgl   *MGLParams `json:"mgl,omitempty"`
	Label string     `json:"label,omitempty"`
	Exts  []string   `json:"extensions,omitempty"`
}

type Core struct {
	ID             string   `json:"id"`
	LauncherID     string   `json:"launcherId,omitempty"`
	SetName        string   `json:"setName,omitempty"`
	RBF            string   `json:"rbf,omitempty"`
	Folders        []string `json:"folders,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
	Slots          []Slot   `json:"slots,omitempty"`
	SetNameSameDir bool     `json:"setNameSameDir,omitempty"`
}

type dataFile struct {
	Systems map[string]Core   `json:"systems"`
	Groups  map[string][]Core `json:"groups"`
}

//go:embed catalog.json
var rawCatalog []byte

var canonical = loadCatalog()

func loadCatalog() dataFile {
	var data dataFile
	if err := json.Unmarshal(rawCatalog, &data); err != nil {
		panic(fmt.Errorf("decode embedded MiSTer catalog: %w", err))
	}
	if len(data.Systems) == 0 {
		panic("embedded MiSTer catalog contains no systems")
	}
	return data
}

func cloneMGLParams(params *MGLParams) *MGLParams {
	if params == nil {
		return nil
	}
	cloned := *params
	return &cloned
}

func cloneSlot(slot *Slot) Slot {
	cloned := *slot
	cloned.Exts = append([]string(nil), slot.Exts...)
	cloned.Mgl = cloneMGLParams(slot.Mgl)
	return cloned
}

func cloneCore(core *Core) Core {
	cloned := *core
	cloned.Folders = append([]string(nil), core.Folders...)
	cloned.Extensions = append([]string(nil), core.Extensions...)
	cloned.Slots = make([]Slot, len(core.Slots))
	for i := range core.Slots {
		cloned.Slots[i] = cloneSlot(&core.Slots[i])
	}
	return cloned
}

// All returns every canonical core definition sorted by ID.
func All() []Core {
	ids := make([]string, 0, len(canonical.Systems))
	for id := range canonical.Systems {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	cores := make([]Core, 0, len(ids))
	for _, id := range ids {
		core := canonical.Systems[id]
		cores = append(cores, cloneCore(&core))
	}
	return cores
}

// Get returns an exact core definition by ID.
func Get(id string) (*Core, error) {
	core, ok := canonical.Systems[id]
	if !ok {
		return nil, fmt.Errorf("unknown system: %s", id)
	}
	cloned := cloneCore(&core)
	return &cloned, nil
}

// Groups returns deep copies of all core groups.
func Groups() map[string][]Core {
	groups := make(map[string][]Core, len(canonical.Groups))
	for id, members := range canonical.Groups {
		cloned := make([]Core, len(members))
		for i := range members {
			member := &members[i]
			if system, ok := canonical.Systems[member.ID]; ok {
				member.Folders = system.Folders
				member.Extensions = system.Extensions
			}
			cloned[i] = cloneCore(member)
		}
		groups[id] = cloned
	}
	return groups
}

// GetGroup returns an exact group definition with all member slots merged.
func GetGroup(id string) (Core, error) {
	members, ok := Groups()[id]
	if !ok {
		return Core{}, fmt.Errorf("no system group found for %s", id)
	}
	if len(members) == 0 {
		return Core{}, fmt.Errorf("no systems in %s", id)
	}
	if len(members) == 1 {
		return members[0], nil
	}

	merged := cloneCore(&members[0])
	merged.Slots = nil
	for i := range members {
		for j := range members[i].Slots {
			merged.Slots = append(merged.Slots, cloneSlot(&members[i].Slots[j]))
		}
	}
	return merged, nil
}

// Lookup resolves a group exactly, then a system ID case-insensitively.
func Lookup(id string) (*Core, error) {
	if group, err := GetGroup(id); err == nil {
		return &group, nil
	}
	for systemID := range canonical.Systems {
		if strings.EqualFold(systemID, id) {
			core := canonical.Systems[systemID]
			cloned := cloneCore(&core)
			return &cloned, nil
		}
	}
	return nil, fmt.Errorf("unknown system: %s", id)
}

// PathToMGLDef resolves a media path to the matching MGL slot parameters.
func PathToMGLDef(core *Core, path string) (*MGLParams, error) {
	if core == nil {
		return nil, fmt.Errorf("system has no matching mgl args: <nil>, %s", path)
	}
	lowerPath := strings.ToLower(path)
	for _, slot := range core.Slots {
		for _, ext := range slot.Exts {
			if strings.HasSuffix(lowerPath, ext) {
				return cloneMGLParams(slot.Mgl), nil
			}
		}
	}
	return nil, fmt.Errorf("system has no matching mgl args: %s, %s", core.ID, path)
}
