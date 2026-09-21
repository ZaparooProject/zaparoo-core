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

//go:build linux && !android

package chimeraos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestScanChimeraGOGGamesIncludesInstallDirectorySource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gameDir := filepath.Join(root, "12345")
	require.NoError(t, os.MkdirAll(gameDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gameDir, "start.sh"), []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "without-launcher"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "not-a-game"), nil, 0o600))

	results := scanChimeraGOGGames(root)
	require.Len(t, results, 1)
	require.Equal(t, "gog://12345/12345", results[0].Path)
	require.Equal(t, &platforms.MediaSource{
		Path: gameDir, Root: root, Kind: platforms.MediaSourceDirectory,
	}, results[0].Source)
}
