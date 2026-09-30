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

package mister

import (
	"path/filepath"
	"testing"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/stretchr/testify/assert"
)

func TestScreenshotWatchDirs(t *testing.T) {
	t.Parallel()

	dir := func(name string) string {
		return filepath.Join(misterconfig.ScreenshotsDir, name)
	}

	tests := []struct {
		name     string
		coreName string
		rbfName  string
		want     []string
	}{
		{
			name:     "setname override watches both",
			coreName: "RA_SNES",
			rbfName:  "SNES",
			want:     []string{dir("RA_SNES"), dir("SNES")},
		},
		{
			name:     "same names watch one",
			coreName: "SNES",
			rbfName:  "SNES",
			want:     []string{dir("SNES")},
		},
		{
			name:     "missing rbf name watches core name only",
			coreName: "MENU",
			rbfName:  "",
			want:     []string{dir("MENU")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, screenshotWatchDirs(tt.coreName, tt.rbfName))
		})
	}
}
