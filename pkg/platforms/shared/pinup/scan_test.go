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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanResults(t *testing.T) {
	t.Parallel()

	lib := Library{
		Emulators: map[int]Emulator{1: {ID: 1}},
		Tables: []Table{
			{ID: 10, EmulatorID: 1, Name: "afm", Display: "Attack from Mars"},
			{ID: 12, EmulatorID: 1, Name: "fp_table"},
			{ID: 15, EmulatorID: 1},
			{ID: 16, EmulatorID: 1, Display: "Bad\x01Name"},
		},
	}

	results := ScanResults(lib)
	assert.Len(t, results, 2)

	assert.Equal(t, "popper://10/Attack%20from%20Mars", results[0].Path)
	assert.Equal(t, "Attack from Mars", results[0].Name)
	assert.True(t, results[0].NoExt)

	assert.Equal(t, "popper://12/fp_table", results[1].Path)
	assert.Equal(t, "fp_table", results[1].Name, "display name falls back to the file stem")
	assert.True(t, results[1].NoExt)
}
