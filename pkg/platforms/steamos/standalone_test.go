//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeStandaloneLaunchers(t *testing.T) {
	t.Parallel()

	launchers := nativeStandaloneLaunchers()
	require.Len(t, launchers, 18)

	byID := make(map[string]platforms.Launcher, len(launchers))
	for i := range launchers {
		launcher := launchers[i]
		byID[launcher.ID] = launcher
		assert.Contains(t, launcher.Groups, platformshared.LauncherGroupNative)
		assert.Equal(t, platforms.LifecycleTracked, launcher.Lifecycle)
		assert.NotNil(t, launcher.Availability)
		assert.NotNil(t, launcher.Launch)
	}

	assert.Equal(t, systemdefs.SystemXbox360, byID["XeniaCanary"].SystemID)
	assert.Equal(t, systemdefs.SystemSwitch, byID["Ryubing"].SystemID)
	assert.Equal(t, systemdefs.SystemPS4, byID["ShadPS4"].SystemID)
	assert.Equal(t, systemdefs.SystemPS2, byID["PCSX2"].SystemID)
	assert.Equal(t, systemdefs.SystemWiiU, byID["Cemu"].SystemID)
	assert.Equal(t, systemdefs.System3DS, byID["Azahar"].SystemID)
	assert.Equal(t, systemdefs.SystemVita, byID["Vita3K"].SystemID)
	assert.Equal(t, systemdefs.SystemPS3, byID["RPCS3"].SystemID)
	assert.Equal(t, systemdefs.SystemPSX, byID["DuckStation"].SystemID)
	assert.Equal(t, systemdefs.SystemPSP, byID["PPSSPP"].SystemID)
	assert.Equal(t, systemdefs.SystemGameCube, byID["DolphinGameCube"].SystemID)
	assert.Equal(t, systemdefs.SystemWii, byID["DolphinWii"].SystemID)
	assert.Equal(t, systemdefs.SystemNDS, byID["MelonDS"].SystemID)
	assert.Equal(t, systemdefs.SystemScummVM, byID["ScummVMStandalone"].SystemID)
	assert.Equal(t, systemdefs.SystemModel3, byID["Supermodel"].SystemID)
	assert.Equal(t, systemdefs.SystemXbox, byID["Xemu"].SystemID)

	for _, id := range []string{
		"XeniaCanary", "Ryubing", "ShadPS4",
		"PCSX2", "Cemu", "Azahar", "Vita3K", "RPCS3",
		"DolphinGameCube", "DolphinWii", "Supermodel", "Xemu",
	} {
		assert.False(t, byID[id].SkipFilesystemScan, id)
		assert.NotEmpty(t, byID[id].Folders, id)
		assert.NotEmpty(t, byID[id].Extensions, id)
	}
	for _, id := range []string{
		"DuckStation", "PPSSPP", "MelonDS", "ScummVMStandalone", "PrimeHackGameCube", "PrimeHackWii",
	} {
		assert.True(t, byID[id].SkipFilesystemScan, id)
	}
}

func TestFlatpakRunArgsExposeMediaDirectoryReadOnly(t *testing.T) {
	t.Parallel()

	mediaPath := filepath.Join(string(filepath.Separator), "run", "media", "deck", "ROMs", "ps2", "game.iso")
	assert.Equal(t, []string{
		"run",
		"--filesystem=" + filepath.Dir(mediaPath) + ":ro",
		"--die-with-parent",
		"net.pcsx2.PCSX2",
		"-batch",
		mediaPath,
	}, flatpakRunArgs("net.pcsx2.PCSX2", mediaPath, []string{"-batch", mediaPath}))
}

func TestStandaloneArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   string
		path string
		want []string
	}{
		{id: "XeniaCanary", path: "game.xex", want: []string{"game.xex"}},
		{id: "Ryubing", path: "game.nro", want: []string{"--fullscreen", "game.nro"}},
		{id: "PCSX2", path: "game.iso", want: []string{"-fullscreen", "-batch", "game.iso"}},
		{id: "Cemu", path: "game.wua", want: []string{"-g", "game.wua", "-f"}},
		{id: "Azahar", path: "game.3dsx", want: []string{"-f", "game.3dsx"}},
		{id: "DuckStation", path: "game.cue", want: []string{"-batch", "-fullscreen", "game.cue"}},
		{id: "PPSSPP", path: "game.iso", want: []string{"--fullscreen", "game.iso"}},
		{id: "DolphinGameCube", path: "game.dol", want: []string{"-b", "-e", "game.dol"}},
		{id: "MelonDS", path: "game.nds", want: []string{"--fullscreen", "game.nds"}},
		{id: "Supermodel", path: "game.zip", want: []string{"-fullscreen", "game.zip"}},
		{id: "Xemu", path: "game.iso", want: []string{"-full-screen", "-dvd_path", "game.iso"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			var def standaloneDef
			for i := range nativeStandaloneDefs {
				if nativeStandaloneDefs[i].id == tt.id {
					def = nativeStandaloneDefs[i]
					break
				}
			}
			require.NotNil(t, def.args)
			got, err := def.args(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstalledTitleArgs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	vitaPath := filepath.Join(dir, "homebrew.psvita")
	require.NoError(t, os.WriteFile(vitaPath, []byte("VITATEST01\n"), 0o600))
	args, err := vita3KArgs(vitaPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"-Fr", "VITATEST01"}, args)

	ps4Target := filepath.Join(dir, "eboot.bin")
	ps4Path := filepath.Join(dir, "homebrew.ps4")
	require.NoError(t, os.WriteFile(ps4Path, []byte(ps4Target+"\n"), 0o600))
	args, err = shadPS4Args(ps4Path)
	require.NoError(t, err)
	assert.Equal(t, []string{"-g", ps4Target, "--fullscreen", "true"}, args)

	ps3Target := filepath.Join(dir, "EBOOT.BIN")
	ps3Path := filepath.Join(dir, "homebrew.ps3")
	require.NoError(t, os.WriteFile(ps3Path, []byte(ps3Target+"\n"), 0o600))
	args, err = rpcs3Args(ps3Path)
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-gui", ps3Target}, args)
}

func TestScummVMArgs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sky.scummvm")
	require.NoError(t, os.WriteFile(path, []byte("sky\n"), 0o600))

	args, err := scummVMArgs(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"--fullscreen", "--path=" + dir, "sky"}, args)

	_, err = scummVMArgs(filepath.Join(dir, "game.dat"))
	require.Error(t, err)
}
