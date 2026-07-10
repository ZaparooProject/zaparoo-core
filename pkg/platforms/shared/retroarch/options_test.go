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

package retroarch

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildCommand_Direct(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "runtime", "retroarch")
	opts := Options{
		Exec:             []string{filepath.Join(root, "retroarch")},
		CoresDir:         filepath.Join(root, "cores"),
		ConfigPath:       filepath.Join(root, "retroarch.cfg"),
		AppendConfigPath: filepath.Join(root, "network.cfg"),
		Home:             filepath.Join(string(filepath.Separator), "userdata", "home"),
		LibDir:           filepath.Join(root, "lib"),
		ExtraEnv:         []string{"EXAMPLE=value"},
		ExtraArgs:        []string{"-v"},
	}
	mediaPath := filepath.Join(string(filepath.Separator), "media", "psx", "game.chd")
	core := CoreLaunch{Core: "pcsx_rearmed_libretro.so"}

	spec := BuildCommand(opts, core, mediaPath)

	assert.Equal(t, opts.Exec[0], spec.Name)
	assert.Equal(t, []string{
		"-v",
		"--config", opts.ConfigPath,
		"--appendconfig", opts.AppendConfigPath,
		"-L", filepath.Join(opts.CoresDir, core.Core),
		mediaPath,
	}, spec.Args)
	assert.Equal(t, []string{
		"EXAMPLE=value",
		"HOME=" + opts.Home,
		"LD_LIBRARY_PATH=" + opts.LibDir,
	}, spec.Env)
}

func TestBuildCommand_FlatpakWithVTWrap(t *testing.T) {
	t.Parallel()

	opts := Options{
		VTWrap:   []string{"openvt", "-c", "2", "-s", "-w", "--"},
		Exec:     []string{"flatpak", "run", "org.libretro.RetroArch"},
		CoresDir: filepath.Join("cores", "retroarch"),
	}
	core := CoreLaunch{Core: "mesen_libretro.so"}
	mediaPath := filepath.Join("roms", "nes", "game.nes")

	spec := BuildCommand(opts, core, mediaPath)

	assert.Equal(t, "openvt", spec.Name)
	assert.Equal(t, []string{
		"-c", "2", "-s", "-w", "--",
		"flatpak", "run", "org.libretro.RetroArch",
		"-L", filepath.Join(opts.CoresDir, core.Core), mediaPath,
	}, spec.Args)
	assert.Empty(t, spec.Env)
}

func TestBuildCommand_OmitsEmptyOptions(t *testing.T) {
	t.Parallel()

	opts := Options{Exec: []string{"retroarch"}, CoresDir: "cores"}
	core := CoreLaunch{Core: "gambatte_libretro.so"}

	spec := BuildCommand(opts, core, "game.gb")

	assert.Equal(t, "retroarch", spec.Name)
	assert.Equal(t, []string{"-L", filepath.Join("cores", core.Core), "game.gb"}, spec.Args)
	assert.Empty(t, spec.Env)
}
