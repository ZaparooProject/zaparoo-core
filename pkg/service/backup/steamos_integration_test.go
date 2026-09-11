//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteamOSRestorePolicyAcceptsNarrowDynamicPrefixPlan(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.SteamOS)
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = steamos.BackupDefinitions(env.RootDir)
	env.Manager.pl = backupRestoreRootPlatform{
		restoreRoot:    env.RootDir,
		backupPlatform: basePlatform,
	}

	platformFiles := []FileRef{
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join(
			".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "Arcade",
			"bottle.yml",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "Arcade",
			"drive_c", "users", "steamuser", "Documents", "Game", "save.dat",
		))},
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join(
			"Bottles", "custom", "Arcade", "bottle.yml",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			"Bottles", "custom", "Arcade", "drive_c", "users", "steamuser", "Saved Games",
			"Game", "save.dat",
		))},
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join(
			"Faugus", "Game", "pfx", "user.reg",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			"Steam", "nonsteam", "4000000001", "pfx", "drive_c", "users", "steamuser",
			"AppData", "LocalLow", "Studio", "Game", "save.dat",
		))},
	}
	files := make([]FileRef, 0, 1+len(platformFiles))
	files = append(files, FileRef{
		Category: CategoryZaparoo, RestorePath: "user.db", ArchivePath: zaparooArchive("user.db"),
	})
	for _, file := range platformFiles {
		file.ArchivePath = platformArchive(file.RestorePath)
		files = append(files, file)
	}

	err := env.Manager.validateManifestPolicy(&Manifest{Platform: platformids.SteamOS, Files: files})
	require.NoError(t, err)
}

func TestSteamOSRestorePolicyRejectsBroadPrefixPaths(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.SteamOS)
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = steamos.BackupDefinitions(env.RootDir)
	env.Manager.pl = backupRestoreRootPlatform{
		restoreRoot: env.RootDir, backupPlatform: basePlatform,
	}

	invalid := []FileRef{
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "123456", "pfx", "drive_c",
			"users", "steamuser", "Documents", "save.dat",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			"Steam", "nonsteam", "123456", "pfx", "drive_c", "users", "steamuser",
			"Documents", "save.dat",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			"Steam", "nonsteam", "4000000001", "pfx", "drive_c", "users", "steamuser", "Other",
			"Documents", "save.dat",
		))},
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join(
			"Steam", "nonsteam", "4000000001", "pfx", "tracked_files",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "Arcade",
			"drive_c", "users", "steamuser", "Other", "Documents", "save.dat",
		))},
	}
	for _, file := range invalid {
		file.ArchivePath = platformArchive(file.RestorePath)
		manifest := Manifest{Platform: platformids.SteamOS, Files: []FileRef{
			{Category: CategoryZaparoo, RestorePath: "user.db", ArchivePath: zaparooArchive("user.db")},
			file,
		}}
		err := env.Manager.validateManifestPolicy(&manifest)
		require.ErrorContains(t, err, "not collected", file.RestorePath)
	}
}

func TestSteamOSBackupRestoresCustomEmuDeckTarget(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.SteamOS)
	customRoot := t.TempDir()
	customSaves := filepath.Join(customRoot, "saves")
	settingsPath := filepath.Join(env.RootDir, ".config", "EmuDeck", "settings.sh")
	writeTestFile(t, settingsPath, "savesPath=\""+customSaves+"\"\n")
	savePath := filepath.Join(customSaves, "retroarch", "game.srm")
	writeTestFile(t, savePath, "original\n")

	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = steamos.BackupDefinitions(env.RootDir)
	env.Manager.pl = backupRestoreRootPlatform{
		restoreRoot: env.RootDir, backupPlatform: basePlatform,
	}

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	staged := stageTestZip(t, info.Path)
	assert.Contains(t, restorePaths(staged.Manifest.Files), filepath.ToSlash(filepath.Join(
		"Emulation", "saves", "retroarch", "game.srm",
	)))
	writeTestFile(t, savePath, "changed\n")
	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	data, err := os.ReadFile(savePath) // #nosec G304 -- test-owned temp path.
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(data))
	_, err = os.Stat(filepath.Join(env.RootDir, "Emulation", "saves", "retroarch", "game.srm"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSteamOSBackupRestoresThroughExplicitCategorySymlink(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.SteamOS)
	target := t.TempDir()
	link := filepath.Join(env.RootDir, "Emulation", "saves")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o750))
	require.NoError(t, os.Symlink(target, link))
	savePath := filepath.Join(target, "game.srm")
	writeTestFile(t, savePath, "original\n")

	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = steamos.BackupDefinitions(env.RootDir)
	env.Manager.pl = backupRestoreRootPlatform{
		restoreRoot: env.RootDir, backupPlatform: basePlatform,
	}
	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	writeTestFile(t, savePath, "changed\n")
	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	data, err := os.ReadFile(savePath) // #nosec G304 -- test-owned temp path.
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(data))
}

func restorePaths(files []FileRef) map[string]struct{} {
	result := make(map[string]struct{}, len(files))
	for _, file := range files {
		result[file.RestorePath] = struct{}{}
	}
	return result
}

func TestSteamOSBackupRestoresDurableDataAndExcludesReplaceableContent(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.SteamOS)
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = steamos.BackupDefinitions(env.RootDir)
	env.Manager.pl = backupRestoreRootPlatform{
		restoreRoot:    env.RootDir,
		backupPlatform: basePlatform,
	}

	durable := map[string]string{
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "saves", "game.srm",
		): "emulator-save\n",
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "states", "game.state",
		): "emulator-state\n",
		filepath.Join("Emulation", "bios", "scph5501.bin"):                 "user-bios\n",
		filepath.Join("ES-DE", "collections", "custom.cfg"):                "custom-collection\n",
		filepath.Join(".config", "EmuDeck", "settings.sh"):                 "savesPath=\"/tmp/saves\"\n",
		filepath.Join(".config", "EmuDeck", "custom_scripts", "custom.sh"): "custom-script\n",
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "retroarch.cfg",
		): "input-config\n",
		filepath.Join(
			".var", "app", "app.xemu.xemu", "data", "xemu", "xemu", "xemu.toml",
		): "xemu-config\n",
		filepath.Join(
			".var", "app", "app.xemu.xemu", "data", "xemu", "xemu", "eeprom.bin",
		): "xemu-eeprom\n",
		filepath.Join(
			".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Config", "Dolphin.ini",
		): "dolphin-config\n",
		filepath.Join(
			".var", "app", "org.DolphinEmu.dolphin-emu", "config", "dolphin-emu", "Profiles",
			"GCPad", "custom.ini",
		): "dolphin-profile\n",
		filepath.Join(
			".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Wii", "title",
			"00010000", "52534d45", "data", "save.dat",
		): "dolphin-wii-save\n",
		filepath.Join(
			".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "GBA", "Saves",
			"game.sav",
		): "dolphin-gba-save\n",
		filepath.Join(
			".var", "app", "rs.ruffle.Ruffle", "data", "ruffle", "SharedObjects", "game.sol",
		): "ruffle-save\n",
		filepath.Join(
			".var", "app", "org.mamedev.MAME", ".mame", "nvram", "game", "nvram",
		): "mame-nvram\n",
		filepath.Join(".kodi", "userdata", "Database", "MyVideos131.db"): "kodi-library\n",
		filepath.Join(
			".config", "Moonlight Game Streaming Project", "Moonlight.conf",
		): "moonlight-identity\n",
		filepath.Join(
			".config", "steam-rom-manager", "userData", "userConfigurations.json",
		): "custom-parser\n",
		filepath.Join(
			".var", "app", "org.citra_emu.citra", "data", "citra-emu", "sdmc", "Nintendo 3DS",
			"id0", "title", "00040000", "00112233", "data", "00000001.sav",
		): "citra-save\n",
		filepath.Join(
			".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "nand", "data", "id0",
			"extdata", "00048000", "F000000B", "gamecoin.dat",
		): "azahar-system-save\n",
		filepath.Join(
			".config", "Ryujinx", "bis", "user", "save", "0000000000000001", "save.dat",
		): "ryujinx-save\n",
		filepath.Join(
			".config", "Ryujinx", "bis", "user", "saveMeta", "0000000000000001", "meta.dat",
		): "ryujinx-save-meta\n",
		filepath.Join(
			".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx", "bis", "system", "save",
			"8000000000000010", "save.dat",
		): "ryujinx-system-save\n",
		filepath.Join(".config", "Ryujinx", "system", "prod.keys"): "switch-keys\n",
		filepath.Join(
			".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "savestates", "game.SAVESTAT",
		): "rpcs3-state\n",
		filepath.Join(
			".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_hdd0", "savedata", "vmc",
			"card.VM2",
		): "rpcs3-memory-card\n",
		filepath.Join(
			".var", "app", "net.shadps4.shadPS4", "data", "shadPS4", "home", "1000", "savedata",
			"CUSA00001", "save.dat",
		): "shadps4-save\n",
		filepath.Join(
			".var", "app", "net.shadps4.shadPS4", "data", "shadPS4", "home", "1000", "inputs",
			"controller.json",
		): "shadps4-input\n",
		filepath.Join(
			".local", "share", "Steam", "userdata", "123", "config", "shortcuts.vdf",
		): "shortcuts\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx", "drive_c",
			"users", "steamuser", "Documents", "NonSteamGame", "save.dat",
		): "non-steam-save\n",
		filepath.Join("retrodeck", "saves", "nes", "game.srm"): "retrodeck-save\n",
	}
	excluded := map[string]string{
		filepath.Join("Emulation", "roms", "nes", "game.nes"):                                 "rom\n",
		filepath.Join("Emulation", "bios", "RetroArch_v1.10.1.zip"):                           "runtime-archive\n",
		filepath.Join("Emulation", "bios", "RetroArch_v1.10.1", "retroarch"):                  "runtime\n",
		filepath.Join("Emulation", "bios", "ume", "hash", "software.xml"):                     "runtime-metadata\n",
		filepath.Join("Emulation", "bios", "PPSSPP", "themes", "default.ini"):                 "runtime-theme\n",
		filepath.Join("Emulation", "bios", "dolphin-emu", "Sys", "codehandler.bin"):           "runtime-system-data\n",
		filepath.Join("Emulation", "saves", "retroarch", "saves", "alias.srm"):                "managed-alias\n",
		filepath.Join(".config", "EmuDeck", "backend", ".git", "objects", "pack", "pack.bin"): "repo\n",
		filepath.Join(".config", "EmuDeck", "python_virtual_env", "lib", "site.py"):           "venv\n",
		filepath.Join(
			"Emulation", "tools", "downloaded_media", "cover.png",
		): "scraped-media\n",
		filepath.Join("retrodeck", "roms", "nes", "game.nes"): "retrodeck-rom\n",
		filepath.Join("retrodeck", "logs", "retrodeck.log"):   "log\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "common", "Game", "game.exe",
		): "steam-game\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "shadercache", "123", "cache.bin",
		): "shader-cache\n",
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "cache", "index.bin",
		): "cache\n",
		filepath.Join(
			".var", "app", "org.ppsspp.PPSSPP", "config", "ppsspp", "PSP", "SYSTEM", "DUMP", "log.txt",
		): "emulator-log\n",
		filepath.Join(
			".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "log", "azahar_log.txt",
		): "emulator-log\n",
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "assets", "menu.png",
		): "runtime-asset\n",
		filepath.Join(
			".var", "app", "org.libretro.RetroArch", "config", "retroarch", "overlays", "border.png",
		): "border\n",
		filepath.Join(".kodi", "userdata", "Thumbnails", "a", "cover.jpg"): "kodi-thumbnail\n",
		filepath.Join(".kodi", "userdata", "Database", "Textures13.db"):    "kodi-texture-cache\n",
		filepath.Join(
			".var", "app", "org.citra_emu.citra", "data", "citra-emu", "sdmc", "Nintendo 3DS",
			"id0", "title", "00040000", "00112233", "content", "game.app",
		): "installed-cia\n",
		filepath.Join(
			".config", "Ryujinx", "bis", "user", "Contents", "registered", "installed.nca",
		): "installed-switch-content\n",
		filepath.Join(
			".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Wii", "title",
			"00010000", "52534d45", "content", "game.app",
		): "installed-wii-content\n",
		filepath.Join(
			".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_hdd0", "game", "TEST12345",
			"payload.dat",
		): "installed-ps3-content\n",
		filepath.Join(
			".var", "app", "net.shadps4.shadPS4", "data", "shadPS4", "game_data", "CUSA00001",
			"game.bin",
		): "installed-ps4-content\n",
		filepath.Join(
			".var", "app", "net.retrodeck.retrodeck", "config", "ES-DE", "resources", "splash.svg",
		): "frontend-runtime-resource\n",
		filepath.Join(
			".var", "app", "net.retrodeck.retrodeck", "config", "steam-rom-manager", "userData",
			"artworkCache.json",
		): "artwork-cache\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx", "drive_c",
			"users", "steamuser", "AppData", "Local", "Game", "Cache", "blob.bin",
		): "windows-cache\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx", "drive_c",
			"users", "steamuser", "AppData", "Local", "Battle.net", "CachedData.db",
		): "launcher-cache\n",
		filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx", "drive_c",
			"users", "steamuser", "AppData", "Local", "dxvk", "pipeline.dxvk.bin",
		): "shader-cache\n",
		filepath.Join(".config", "PCSX2", "themes", "custom", "theme.json"): "theme\n",
	}
	for path, contents := range durable {
		writeTestFile(t, filepath.Join(env.RootDir, path), contents)
	}
	for path, contents := range excluded {
		writeTestFile(t, filepath.Join(env.RootDir, path), contents)
	}

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	staged := stageTestZip(t, info.Path)
	byRestorePath := make(map[string]FileRef, len(staged.Manifest.Files))
	for _, file := range staged.Manifest.Files {
		byRestorePath[file.RestorePath] = file
	}
	for path := range durable {
		restorePath := path
		switch path {
		case filepath.Join(".local", "share", "Steam", "userdata", "123", "config", "shortcuts.vdf"):
			restorePath = filepath.Join("Steam", "userdata", "123", "config", "shortcuts.vdf")
		case filepath.Join(
			".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx", "drive_c",
			"users", "steamuser", "Documents", "NonSteamGame", "save.dat",
		):
			restorePath = filepath.Join(
				"Steam", "nonsteam", "4000000001", "pfx", "drive_c", "users", "steamuser",
				"Documents", "NonSteamGame", "save.dat",
			)
		}
		assert.Contains(t, byRestorePath, filepath.ToSlash(restorePath))
	}
	for path := range excluded {
		assert.NotContains(t, byRestorePath, filepath.ToSlash(path))
	}
	assert.Equal(t, CategorySaves, byRestorePath[filepath.ToSlash(filepath.Join(
		".var", "app", "app.xemu.xemu", "data", "xemu", "xemu", "eeprom.bin",
	))].Category)
	assert.Equal(t, CategorySaves, byRestorePath[filepath.ToSlash(filepath.Join(
		".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "nand", "data", "id0",
		"extdata", "00048000", "F000000B", "gamecoin.dat",
	))].Category)

	for path := range durable {
		writeTestFile(t, filepath.Join(env.RootDir, path), "changed\n")
	}
	for path := range excluded {
		writeTestFile(t, filepath.Join(env.RootDir, path), "changed-but-excluded\n")
	}

	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	for path, expected := range durable {
		data, readErr := os.ReadFile(filepath.Join(env.RootDir, path)) // #nosec G304 -- test-owned temp path.
		require.NoError(t, readErr)
		assert.Equal(t, expected, string(data), path)
	}
	for path := range excluded {
		data, readErr := os.ReadFile(filepath.Join(env.RootDir, path)) // #nosec G304 -- test-owned temp path.
		require.NoError(t, readErr)
		assert.Equal(t, "changed-but-excluded\n", string(data), path)
	}
}
