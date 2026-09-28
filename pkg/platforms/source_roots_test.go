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

package platforms_test

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceLocation(t *testing.T) {
	t.Parallel()
	root := platforms.SourceRootPath("tree")
	id, segments, err := platforms.SourceLocation(root)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimPrefix(root, "source://"), id)
	assert.Empty(t, segments)

	path, err := virtualpath.CreateVirtualPathSegments(platforms.SourceScheme, id,
		[]string{"NES", "Game (USA).nes"})
	require.NoError(t, err)
	gotID, gotSegments, err := platforms.SourceLocation(path)
	require.NoError(t, err)
	assert.Equal(t, id, gotID)
	assert.Equal(t, []string{"NES", "Game (USA).nes"}, gotSegments)

	_, _, err = platforms.SourceLocation("/roms/nes/game.nes")
	require.ErrorIs(t, err, platforms.ErrNotSourcePath)
	_, _, err = platforms.SourceLocation("source://")
	require.ErrorIs(t, err, platforms.ErrNotSourcePath)
	_, _, err = platforms.SourceLocation(root + "/NES/Game (USA).nes")
	require.Error(t, err, "a non-canonical spelling is rejected")
}
