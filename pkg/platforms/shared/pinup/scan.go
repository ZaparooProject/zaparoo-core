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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// ScanResults converts a library into indexable media. Every table becomes a
// popper:// virtual path keyed by GameID; the display name is what users see.
func ScanResults(lib Library) []platforms.ScanResult {
	results := make([]platforms.ScanResult, 0, len(lib.Tables))
	for i := range lib.Tables {
		table := &lib.Tables[i]
		name := table.DisplayName()
		if name == "" || virtualpath.ContainsControlChar(name) {
			continue
		}
		results = append(results, platforms.ScanResult{
			Path:  TablePath(table.ID, name),
			Name:  name,
			NoExt: true,
		})
	}
	return results
}
