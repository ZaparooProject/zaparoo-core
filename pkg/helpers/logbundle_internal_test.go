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

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
	"github.com/stretchr/testify/assert"
)

// The smallest bundles give the current crash file a budget too small to reach
// the version header's newline. The fragment records no crash, but counting it
// spent the budget the previous crash file needed, so a bundle reported the
// header of a clean run instead of the crash before it. A fragment shorter than
// the prefix does not start with it either, which is the case that slipped past
// the original check.
func TestCrashCaptureHeaderOnly(t *testing.T) {
	t.Parallel()

	full := crashdump.VersionPrefix + "1.2.3\n"
	for _, tt := range []struct {
		name string
		body string
		want bool
	}{
		{"header with trailing newline", full, true},
		{"header with blank remainder", full + "\n  \n", true},
		{"whole prefix, no newline", crashdump.VersionPrefix, true},
		{"prefix cut short", crashdump.VersionPrefix[:11], true},
		{"version line cut short", crashdump.VersionPrefix + "1.2", true},
		{"header then a panic", full + "panic: boom", false},
		{"a panic with no header", "panic: boom", false},
		{"empty", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, crashCaptureHeaderOnly([]byte(tt.body)))
		})
	}
}
