//go:build windows

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

package steam

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlatformSteamAppsDirsDeduplicates(t *testing.T) {
	// Not parallel: sets process environment.
	t.Setenv("ProgramFiles(x86)", `C:\Program Files (x86)`)
	t.Setenv("ProgramFiles", `C:\Program Files (x86)`)

	dirs := platformSteamAppsDirs("")

	seen := make(map[string]int, len(dirs))
	for _, dir := range dirs {
		seen[strings.ToLower(dir)]++
	}
	for dir, count := range seen {
		assert.Equal(t, 1, count, "duplicate candidate: %s", dir)
	}
}

func TestPlatformSteamAppsDirsAlwaysEndsInSteamapps(t *testing.T) {
	// Not parallel: sets process environment.
	t.Setenv("ProgramFiles(x86)", `C:\Program Files (x86)`)
	t.Setenv("ProgramFiles", `C:\Program Files`)

	dirs := platformSteamAppsDirs("")
	assert.NotEmpty(t, dirs, "the Program Files fallbacks must always be offered")
	for _, dir := range dirs {
		assert.Equal(t, "steamapps", filepath.Base(dir))
	}
}

func TestPlatformSteamAppsDirsSkipsEmptyEnvironment(t *testing.T) {
	// Not parallel: sets process environment.
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("ProgramFiles", "")

	// With no environment and no registry entry the result may be empty, but
	// it must never contain a bare relative path that would resolve against
	// the working directory.
	for _, dir := range platformSteamAppsDirs("") {
		assert.True(t, filepath.IsAbs(dir), "candidate must be absolute: %s", dir)
	}
}
