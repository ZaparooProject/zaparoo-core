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

import "testing"

// The list is the same on every host: this runs where the archives are
// assembled, which is not the platform being packaged.
func TestPayloadFiles(t *testing.T) {
	t.Parallel()

	assertBatoceraPayloadFiles(t)
	for _, platform := range []string{"linux", "windows", "mac", ""} {
		if got := payloadFiles(platform); got != nil {
			t.Fatalf("%s payload files = %v, want nil", platform, got)
		}
	}
}
