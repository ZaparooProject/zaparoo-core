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

package scummvmnames

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func TestTitle(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		target, hint, want string
	}{
		{"sky", "", "Beneath a Steel Sky"},
		{"TENTACLE", "", "Day of the Tentacle"},
		{"sword2", "Broken Sword 2 The Smoking Mirror (CD Windows)", "Broken Sword II: The Smoking Mirror"},
		// Shared IDs: the engine prefix decides, else the hint.
		{"scumm:atlantis", "", "Indiana Jones and the Fate of Atlantis"},
		{"cryomni3d:atlantis", "", "Atlantis: The Lost Tales"},
		{"atlantis", "Indiana Jones and the Fate of Atlantis (CD DOS)", "Indiana Jones and the Fate of Atlantis"},
		{"atlantis", "Atlantis The Lost Tales", "Atlantis: The Lost Tales"},
	} {
		got, ok := Title(c.target, c.hint)
		require.True(t, ok, c.target)
		require.Equal(t, c.want, got, c.target)
	}
	_, ok := Title("notagame", "")
	require.False(t, ok)
	_, ok = Title("sky:atlantis", "")
	require.False(t, ok, "an engine prefix must match")
	require.Regexp(t, `^\d+\.\d+\.\d+$`, Version())
}

func TestIsTargetFile(t *testing.T) {
	t.Parallel()
	require.True(t, IsTargetFile(systemdefs.SystemScummVM, ".scummvm"))
	require.True(t, IsTargetFile(systemdefs.SystemScummVM, ".ScummVM"))
	require.False(t, IsTargetFile(systemdefs.SystemScummVM, ".zip"))
	require.False(t, IsTargetFile(systemdefs.SystemDOS, ".scummvm"))
}

func TestLooksLikeID(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"sky": true, "lsl6hires": true, "monkey2": true, "ft-demo": true, "qfg1_vga": true,
		"": false, "Monkey Island": false, "Sky": false, "sky (cd)": false,
	} {
		require.Equal(t, want, LooksLikeID(name), name)
	}
}
