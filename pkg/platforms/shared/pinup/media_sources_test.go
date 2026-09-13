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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestPopperMetadataSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	games := filepath.Join(root, "Tables")
	require.NoError(t, os.MkdirAll(games, 0o750))
	path := filepath.Join(games, "table.vpx")
	require.NoError(t, os.WriteFile(path, []byte("table"), 0o600))
	require.Equal(t, &platforms.MediaSource{
		Path: path, Root: games, Kind: platforms.MediaSourceFile,
	}, popperMetadataSource(root, &Emulator{GamesDir: "Tables"}, "table.vpx"))
	require.Nil(t, popperMetadataSource(root, &Emulator{}, "table.vpx"))
}
