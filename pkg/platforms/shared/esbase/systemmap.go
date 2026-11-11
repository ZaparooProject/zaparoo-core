//go:build linux && !android

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

package esbase

import (
	"fmt"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// esdeFolder returns the ES-DE folder for an EmulationStation system or ROM
// folder name, following this system's aliases.
func (p *Platform) esdeFolder(esSystem string) string {
	if folder, ok := p.opts.SystemAliases[esSystem]; ok {
		return folder
	}
	return esSystem
}

// aliasesOf returns the alias names that resolve to an ES-DE folder, sorted.
func (p *Platform) aliasesOf(folder string) []string {
	var names []string
	for alias, target := range p.opts.SystemAliases {
		if target == folder {
			names = append(names, alias)
		}
	}
	slices.Sort(names)
	return names
}

// fromESSystem converts an ES system name to a Zaparoo system ID.
func (p *Platform) fromESSystem(esSystem string) (string, error) {
	systemID, err := esde.GetSystemID(p.esdeFolder(esSystem))
	if err != nil {
		return "", fmt.Errorf("unknown EmulationStation system %s: %w", esSystem, err)
	}
	return systemID, nil
}

// toESSystems returns all ES folder names that map to a given Zaparoo
// system ID, including this system's aliases.
func (p *Platform) toESSystems(zaparooSystem string) []string {
	folders := esde.GetFoldersForSystemID(zaparooSystem)
	names := slices.Clone(folders)
	for _, folder := range folders {
		names = append(names, p.aliasesOf(folder)...)
	}
	return names
}
