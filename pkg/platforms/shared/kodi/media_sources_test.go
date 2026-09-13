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

package kodi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestKodiMetadataSource(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "movie.mkv")
	require.NoError(t, os.WriteFile(path, []byte("movie"), 0o600))
	require.Equal(t, &platforms.MediaSource{
		Path: path, Root: filepath.Dir(path), Kind: platforms.MediaSourceFile,
	}, kodiMetadataSource(path))
	for _, invalid := range []string{"smb://server/movie.mkv", "relative.mkv", ""} {
		require.Nil(t, kodiMetadataSource(invalid))
	}
}
