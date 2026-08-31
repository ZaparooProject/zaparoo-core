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

// Package catalog preserves the original Core import path for the standalone
// github.com/ZaparooProject/zaparoo-core/mister/catalog module.
package catalog

import standalone "github.com/ZaparooProject/zaparoo-core/mister/catalog"

type (
	MGLParams = standalone.MGLParams
	Slot      = standalone.Slot
	Core      = standalone.Core
)

func All() []Core {
	return standalone.All()
}

func Get(id string) (*Core, error) {
	return standalone.Get(id) //nolint:wrapcheck // Compatibility wrapper preserves the standalone error.
}

func Groups() map[string][]Core {
	return standalone.Groups()
}

func GetGroup(id string) (Core, error) {
	return standalone.GetGroup(id) //nolint:wrapcheck // Compatibility wrapper preserves the standalone error.
}

func Lookup(id string) (*Core, error) {
	return standalone.Lookup(id) //nolint:wrapcheck // Compatibility wrapper preserves the standalone error.
}

func PathToMGLDef(core *Core, path string) (*MGLParams, error) {
	//nolint:wrapcheck // Compatibility wrapper preserves the standalone error.
	return standalone.PathToMGLDef(core, path)
}
