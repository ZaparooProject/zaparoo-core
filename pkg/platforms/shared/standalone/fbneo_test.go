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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fbneoInstall(t *testing.T, config string) (exe, configPath string) {
	t.Helper()
	install := t.TempDir()
	exe = writeFile(t, filepath.Join(install, "fbneo64.exe"), "MZ")
	configPath = filepath.Join(install, "config", "fbneo64.ini")
	if config != "" {
		writeFile(t, configPath, config)
	}
	return exe, configPath
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test file
	require.NoError(t, err)
	return string(data)
}

func TestFBNeoArgsAddTheSetFolderToAnUnusedSlot(t *testing.T) {
	t.Parallel()

	exe, configPath := fbneoInstall(t, "")
	install := filepath.Dir(exe)
	require.NoError(t, os.MkdirAll(filepath.Join(install, "roms"), 0o750))
	userDir := t.TempDir()
	writeFile(t, configPath, strings.Join([]string{
		"// FinalBurn Neo v1.0.0.03 --- Main Config File",
		"szAppRomPaths[0] roms/",
		"szAppRomPaths[1] " + userDir + `\`,
		"szAppRomPaths[2] roms/arcade/",
		"szAppRomPaths[3] roms/megadrive/",
		"bSkipStartupCheck 0",
		"",
	}, "\r\n"))
	game := writeFile(t, filepath.Join(t.TempDir(), "Arcade", "1941.zip"), "zip")

	args, err := FBNeoArgs(&Context{Path: game, Exe: exe})
	require.NoError(t, err)
	assert.Equal(t, []string{"1941"}, args)
	want := strings.Join([]string{
		"// FinalBurn Neo v1.0.0.03 --- Main Config File",
		"szAppRomPaths[0] roms/",
		"szAppRomPaths[1] " + userDir + `\`,
		"szAppRomPaths[2] " + filepath.Dir(game) + `\`,
		"szAppRomPaths[3] roms/megadrive/",
		"bSkipStartupCheck 0",
		"",
	}, "\r\n")
	assert.Equal(t, want, readConfig(t, configPath), "only the first missing folder's slot changes")

	_, err = FBNeoArgs(&Context{Path: game, Exe: exe})
	require.NoError(t, err)
	assert.Equal(t, want, readConfig(t, configPath), "a folder already listed is not added again")
}

func TestFBNeoArgsPreferAnEmptySlot(t *testing.T) {
	t.Parallel()

	exe, configPath := fbneoInstall(t, "szAppRomPaths[0] missing/\nszAppRomPaths[1] \nx 1\n")
	dir := t.TempDir()
	require.NoError(t, ensureFBNeoROMPath(exe, dir))
	assert.Equal(t, "szAppRomPaths[0] missing/\nszAppRomPaths[1] "+dir+`\`+"\nx 1\n", readConfig(t, configPath))
}

func TestFBNeoArgsKeepAFolderThatCannotBeInspected(t *testing.T) {
	t.Parallel()

	// A NUL byte makes Stat fail with an error other than "does not exist".
	config := "szAppRomPaths[0] bad\x00folder/\nx 1\n"
	exe, configPath := fbneoInstall(t, config)
	err := ensureFBNeoROMPath(exe, t.TempDir())
	require.ErrorContains(t, err, "no unused ROM folder slot")
	assert.Equal(t, config, readConfig(t, configPath))
}

func TestFBNeoArgsWithoutConfigCreateOne(t *testing.T) {
	t.Parallel()

	exe, configPath := fbneoInstall(t, "")
	dir := t.TempDir()
	require.NoError(t, ensureFBNeoROMPath(exe, dir))
	assert.Equal(t, "szAppRomPaths[0] roms/\r\nszAppRomPaths[1] "+dir+`\`+"\r\n", readConfig(t, configPath))
}

func TestFBNeoArgsNeverReplaceUserFolders(t *testing.T) {
	t.Parallel()

	userDir := t.TempDir()
	exe, configPath := fbneoInstall(t, "szAppRomPaths[0] "+userDir+"\n")
	err := ensureFBNeoROMPath(exe, t.TempDir())
	require.ErrorContains(t, err, "no unused ROM folder slot")
	assert.Equal(t, "szAppRomPaths[0] "+userDir+"\n", readConfig(t, configPath))

	err = ensureFBNeoROMPath(exe, filepath.Join(t.TempDir(), "jeux é"))
	require.ErrorContains(t, err, "to FinalBurn Neo's ROM folders")

	args, err := FBNeoArgs(&Context{Path: filepath.Join(t.TempDir(), "1941.zip")})
	require.NoError(t, err, "without a known executable the config is left alone")
	assert.Equal(t, []string{"1941"}, args)
}

func TestFBNeoArgsReuseTheLastEmptyShippedFolder(t *testing.T) {
	t.Parallel()

	exe, configPath := fbneoInstall(t, "")
	install := filepath.Dir(exe)
	for _, name := range []string{"arcade", "nes", "astrocade", "romdata"} {
		require.NoError(t, os.MkdirAll(filepath.Join(install, "roms", name), 0o750))
	}
	writeFile(t, filepath.Join(install, "roms", "nes", "smb.zip"), "zip")
	writeFile(t, configPath, "szAppRomPaths[0] roms/\nszAppRomPaths[1] roms/arcade/\n"+
		"szAppRomPaths[2] roms/nes/\nszAppRomPaths[3] roms/astrocade/\nszAppRomPaths[4] roms/romdata/\n")
	dir := t.TempDir()
	require.NoError(t, ensureFBNeoROMPath(exe, dir))
	assert.Equal(t, "szAppRomPaths[0] roms/\nszAppRomPaths[1] roms/arcade/\n"+
		"szAppRomPaths[2] roms/nes/\nszAppRomPaths[3] "+dir+`\`+"\nszAppRomPaths[4] roms/romdata/\n",
		readConfig(t, configPath), "folders holding sets, roms/ itself and romdata are kept")
}
