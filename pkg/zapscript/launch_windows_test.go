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

//go:build windows

package zapscript

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVirtualStatPath_PreservesUNCRoot(t *testing.T) {
	t.Parallel()

	lookupPath := `\\server\share\games\neogeo\NEOGEO.zip\game.neo`
	parts := strings.Split(lookupPath, string(filepath.Separator))

	statPath := virtualStatPath(lookupPath, parts, len(parts)-1)

	assert.True(t, filepath.IsAbs(statPath))
	assert.Equal(t, `\\server\share\games\neogeo\NEOGEO.zip`, statPath)
}
