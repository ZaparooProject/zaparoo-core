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

// Package mgl preserves the original Core import path for the standalone
// github.com/ZaparooProject/zaparoo-core/mister/v2/mgl module.
package mgl

import (
	"github.com/ZaparooProject/zaparoo-core/mister/v2/catalog"
	standalone "github.com/ZaparooProject/zaparoo-core/mister/v2/mgl"
)

func Generate(core *catalog.Core, rbfPath, mediaPath, override string) (string, error) {
	//nolint:wrapcheck // Compatibility wrapper preserves the standalone error.
	return standalone.Generate(core, rbfPath, mediaPath, override)
}
