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

package standalone

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, contents string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestArgumentBuilders(t *testing.T) {
	t.Parallel()

	ctx := &Context{Path: filepath.Join(string(filepath.Separator), "roms", "game.rom")}
	args, err := Batch("-a", "-b")(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"-a", "-b", ctx.Path}, args)

	args, err = Around([]string{"-a"}, "-z")(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"-a", ctx.Path, "-z"}, args)

	args, err = Joined("-image=", "-f")(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"-f", "-image=" + ctx.Path}, args)

	args, err = CemuArgs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"-g", ctx.Path, "-f"}, args)
}

func TestTargetFileArguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	args, err := Vita3KArgs(&Context{Path: writeFile(t, filepath.Join(dir, "game.psvita"), "PCSA00001\n")})
	require.NoError(t, err)
	assert.Equal(t, []string{"-F", "-r", "PCSA00001"}, args)

	args, err = RPCS3Args(&Context{Path: writeFile(t, filepath.Join(dir, "game.ps3"), "/games/PS3_GAME")})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-gui", "--fullscreen", "/games/PS3_GAME"}, args)

	eboot := filepath.Join(dir, "EBOOT.BIN")
	args, err = RPCS3Args(&Context{Path: eboot})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-gui", "--fullscreen", eboot}, args)

	args, err = ShadPS4Args(&Context{Path: writeFile(t, filepath.Join(dir, "game.ps4"), "CUSA00001")})
	require.NoError(t, err)
	assert.Equal(t, []string{"-g", "CUSA00001", "--fullscreen", "true"}, args)

	scumm := writeFile(t, filepath.Join(dir, "monkey", "monkey.scummvm"), "monkey")
	args, err = ScummVMArgs(&Context{Path: scumm})
	require.NoError(t, err)
	assert.Equal(t, []string{"--fullscreen", "--path=" + filepath.Dir(scumm), "monkey"}, args)

	empty := writeFile(t, filepath.Join(dir, "toltecs", "toltecs.scummvm"), "")
	args, err = ScummVMArgs(&Context{Path: empty})
	require.NoError(t, err)
	assert.Equal(t, []string{"--fullscreen", "--path=" + filepath.Dir(empty), "toltecs"}, args,
		"an empty target file names the game by its file name")

	_, err = ScummVMArgs(&Context{Path: writeFile(t, filepath.Join(dir, "-x.scummvm"), "")})
	require.Error(t, err)

	_, err = ScummVMArgs(&Context{Path: filepath.Join(dir, "monkey.txt")})
	require.ErrorContains(t, err, ".scummvm")
}

func TestReadLauncherTargetRejectsBadTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cases := map[string]string{
		"option.scummvm":   "--help",
		"empty.scummvm":    "  \n",
		"multi.scummvm":    "one\ntwo",
		"oversize.scummvm": strings.Repeat("a", maxLauncherTargetSize+1),
	}
	for name, contents := range cases {
		_, err := ReadLauncherTarget(writeFile(t, filepath.Join(dir, name), contents), ".scummvm")
		require.ErrorContains(t, err, "invalid launcher target", name)
	}
	_, err := ReadLauncherTarget(filepath.Join(dir, "game.ps3"), ".scummvm")
	require.ErrorContains(t, err, "requires a .scummvm file")
	_, err = ReadLauncherTarget(filepath.Join(dir, "missing.scummvm"), ".scummvm")
	require.ErrorContains(t, err, "open launcher target")
}

func TestValidateMediaFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, ValidateMediaFile(writeFile(t, filepath.Join(dir, "game.rom"), "rom")))
	require.ErrorContains(t, ValidateMediaFile(filepath.Join("roms", "game.rom")), "absolute")
	require.ErrorContains(t, ValidateMediaFile(filepath.Join(dir, "missing.rom")), "stat media path")
	require.ErrorContains(t, ValidateMediaFile(dir), "must be a file")
	if runtime.GOOS != "windows" {
		require.ErrorContains(t, ValidateMediaFile(os.DevNull), "must be a file")
	}
}

func TestSetName(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "roms", "arcade")
	name, err := SetName(filepath.Join(root, "outrun.ZIP"), ".zip", ".7z")
	require.NoError(t, err)
	assert.Equal(t, "outrun", name)

	for _, bad := range []string{"outrun.chd", "-help.zip", "a;b.zip", "a\"b.zip", ".zip"} {
		_, err := SetName(filepath.Join(root, bad), ".zip", ".7z")
		require.Error(t, err, bad)
	}
}

func TestSetNameArguments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(string(filepath.Separator), "roms", "naomi", "mvsc2.zip")
	args, err := FBNeoArgs(&Context{Path: path})
	require.NoError(t, err)
	assert.Equal(t, []string{"mvsc2"}, args)

	args, err = demulSetArgs("naomi")(&Context{Path: path})
	require.NoError(t, err)
	assert.Equal(t, []string{"-run=naomi", "-rom=mvsc2"}, args)

	_, err = demulSetArgs("naomi")(&Context{Path: filepath.Join(filepath.Dir(path), "mvsc2.chd")})
	require.Error(t, err)
}

func TestAltirraArgsPickMapperFromCartSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cart := filepath.Join(dir, "kaboom.a52")
	require.NoError(t, os.WriteFile(cart, make([]byte, 4096), 0o600))
	args, err := AltirraArgs("5200")(&Context{Path: cart})
	require.NoError(t, err)
	assert.Equal(t, []string{"/f", "/defprofile:5200", "/cartmapper", "20", "/cart", cart}, args)

	odd := filepath.Join(dir, "odd.bin")
	require.NoError(t, os.WriteFile(odd, make([]byte, 100), 0o600))
	args, err = AltirraArgs("5200")(&Context{Path: odd})
	require.NoError(t, err)
	assert.Equal(t, []string{"/f", "/defprofile:5200", odd}, args, "an unknown size leaves the choice to Altirra")

	args, err = AltirraArgs("xegs")(&Context{Path: odd})
	require.NoError(t, err)
	assert.Equal(t, []string{"/f", "/defprofile:xegs", "/cartmapper", "1", "/cart", odd}, args)

	headered := filepath.Join(dir, "game.car")
	args, err = AltirraArgs("xegs")(&Context{Path: headered})
	require.NoError(t, err)
	assert.Equal(t, []string{"/f", "/defprofile:xegs", headered}, args, ".car images name their own mapper")

	_, err = AltirraArgs("5200")(&Context{Path: filepath.Join(dir, "missing.a52")})
	require.Error(t, err)
}

func TestDOSBoxArgsLoadConfFiles(t *testing.T) {
	t.Parallel()

	args, err := DOSBoxArgs("-exit")(&Context{Path: "/dos/game/GAME.CONF"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-exit", "-conf", "/dos/game/GAME.CONF"}, args)
	args, err = DOSBoxArgs("-exit")(&Context{Path: "/dos/game/game.exe"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-exit", "/dos/game/game.exe"}, args)
}
