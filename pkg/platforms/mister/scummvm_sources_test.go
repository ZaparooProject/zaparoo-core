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

//go:build linux

package mister

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestScummVMMetadataSource(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "games", "Monkey.v1")
	got := scummVMMetadataSource(ScummVMGame{TargetID: "monkey", Path: absolute})
	require.Equal(t, &platforms.MediaSource{
		Path: absolute, Root: filepath.Dir(absolute), Kind: platforms.MediaSourceDirectory,
	}, got)

	relative := scummVMMetadataSource(ScummVMGame{TargetID: "monkey", Path: "games/Monkey"})
	require.Equal(t, filepath.Join(scummvmBaseDir, "games", "Monkey"), relative.Path)
	require.Equal(t, filepath.Join(scummvmBaseDir, "games"), relative.Root)

	for _, path := range []string{"", "remote://game", "bad\x00path", string(filepath.Separator)} {
		require.Nil(t, scummVMMetadataSource(ScummVMGame{Path: path}))
	}
}
