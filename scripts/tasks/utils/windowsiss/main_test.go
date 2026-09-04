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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The build copies the bundled driver in only when the generated script asks
// for it, so an architecture with no MSI has to produce an empty name rather
// than a plausible-looking one.
func TestViGEmMsiForArch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		arch string
		want string
	}{
		{"amd64", "ViGEmBus.x64.msi"},
		{"386", "ViGEmBus.msi"},
		{"arm64", ""},
		{"riscv64", ""},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, vigemMsiForArch(tc.arch))
		})
	}
}
