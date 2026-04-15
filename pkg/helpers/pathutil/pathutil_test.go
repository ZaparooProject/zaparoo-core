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

package pathutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveRelativePath(t *testing.T) {
	t.Parallel()

	exeDir := ExeDir()
	if exeDir == "" {
		t.Skip("ExeDir() returned empty, cannot test relative path resolution")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty string unchanged",
			path:     "",
			expected: "",
		},
		{
			name:     "absolute path unchanged",
			path:     exeDir,
			expected: exeDir,
		},
		{
			name:     "relative path resolved to ExeDir",
			path:     filepath.Join("roms", "nes"),
			expected: filepath.Join(exeDir, "roms", "nes"),
		},
		{
			name:     "dot relative path resolved",
			path:     "./games",
			expected: filepath.Join(exeDir, "games"),
		},
		{
			name:     "single filename resolved",
			path:     "roms",
			expected: filepath.Join(exeDir, "roms"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveRelativePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExeDir(t *testing.T) {
	t.Parallel()

	dir := ExeDir()
	if dir == "" {
		t.Skip("ExeDir() returned empty")
	}

	assert.True(t, filepath.IsAbs(dir), "ExeDir should return an absolute path")
}
