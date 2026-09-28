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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostManagedDirectoryDoesNotProbeExecutable(t *testing.T) {
	t.Parallel()
	path := resolvePlatformDir(true, "host-directory", func() (string, bool) {
		t.Fatal("host-managed paths must not inspect the executable or portable directory")
		return "", false
	})
	assert.Equal(t, "host-directory", path)
}

func TestPlatformDirectoryPreservesPortableDefaults(t *testing.T) {
	t.Parallel()
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "configured", true: "portable"}[exists], func(t *testing.T) {
			t.Parallel()
			path := resolvePlatformDir(false, "configured", func() (string, bool) {
				return "portable", exists
			})
			want := "configured"
			if exists {
				want = "portable"
			}
			assert.Equal(t, want, path)
		})
	}
}
