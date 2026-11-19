//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/batocera"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type batoceraTestPlatform struct {
	restoreRoot string
	mounts      string
	backupPlatform
}

func (p batoceraTestPlatform) BackupRestoreRoot() string {
	return p.restoreRoot
}

func (p batoceraTestPlatform) BackupPlan() platforms.BackupPlan {
	return batocera.BackupPlan(p.restoreRoot, p.mounts)
}

func useBatoceraBackup(t *testing.T, env *backupTestEnv) {
	t.Helper()
	basePlatform, ok := env.Manager.pl.(backupPlatform)
	require.True(t, ok)
	basePlatform.definitions = batocera.BackupDefinitions(env.RootDir)
	env.Manager.pl = batoceraTestPlatform{
		restoreRoot:    env.RootDir,
		mounts:         filepath.Join(env.RootDir, "no-mounts"),
		backupPlatform: basePlatform,
	}
}

func TestBatoceraBackupCategorizesAndRestoresUserdata(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Batocera)
	j := filepath.Join
	configs := j("system", "configs")
	durable := map[string]string{
		// Settings.
		j("system", "batocera.conf"):                                                       CategorySettings,
		j(configs, "emulationstation", "es_settings.cfg"):                                  CategorySettings,
		j(configs, "emulationstation", "collections", "custom-fav.cfg"):                    CategorySettings,
		j("roms", "snes", "gamelist.xml"):                                                  CategorySettings,
		j(configs, "retroarch", "retroarchcustom.cfg"):                                     CategorySettings,
		j(configs, "retroarch", "cores", "retroarch-core-options.cfg"):                     CategorySettings,
		j(configs, "retroarch", "config", "Snes9x", "Snes9x.opt"):                          CategorySettings,
		j(configs, "dosbox", "dosbox.conf"):                                                CategorySettings,
		j(configs, "PCSX2", "inis", "PCSX2.ini"):                                           CategorySettings,
		j("saves", "dolphin-emu", "GameSettings", "GALE01.ini"):                            CategorySettings,
		j("saves", "mame", "cfg", "sf2.cfg"):                                               CategorySettings,
		j("system", "wine-bottles", "windows", "Game.wine", "user.reg"):                    CategorySettings,
		j("system", "wine-bottles", "windows", "wine-proton", "Game.exe.wine", "user.reg"): CategorySettings,
		j(configs, "play", "Play Data Files", "config.xml"):                                CategorySettings,
		j(configs, "redream", "redream.cfg"):                                               CategorySettings,
		j("system", "wine-bottles", "model2", "model2emu", "CFG", "a.input"):               CategorySettings,
		j("roms", "windows", "Port.wine", "system.reg"):                                    CategorySettings,
		// Inputs.
		j(configs, "emulationstation", "es_input.cfg"):                    CategoryInputs,
		j(configs, "retroarch", "config", "remaps", "Snes9x", "game.rmp"): CategoryInputs,
		j(configs, "evmapy", "pad.keys"):                                  CategoryInputs,
		// Saves.
		j("saves", "snes", "game.srm"):                                                            CategorySaves,
		j("saves", "ps2", "pcsx2", "Mcd001.ps2"):                                                  CategorySaves,
		j("saves", "dreamcast", "flycast", "vmu_save_A1.bin"):                                     CategorySaves,
		j("saves", "duckstation", "memcards", "shared_card_1.mcd"):                                CategorySaves,
		j("saves", "dolphin-emu", "GC", "USA", "Card A", "save.gci"):                              CategorySaves,
		j("saves", "dolphin-emu", "Wii", "title", "00010000", "52534d45", "data", "save.dat"):     CategorySaves,
		j("saves", "mame", "nvram", "sf2", "nvram"):                                               CategorySaves,
		j("saves", "psvita", "ux0", "user", "00", "savedata", "PCSE0001", "sce_sys", "param.sfo"): CategorySaves,
		j("saves", "switch", "citron", "nand", "user", "save", "0000000000000000", "save.dat"):    CategorySaves,
		j("saves", "wiiu", "usr", "save", "00050000", "101c9400", "user", "common", "s.dat"):      CategorySaves,
		j("saves", "xbox", "eeprom.bin"):                                                          CategorySaves,
		j("saves", "xbox360", "0000000000000000", "4D5307E6", "00000001", "save"):                 CategorySaves,
		j("saves", "3ds", "azahar-emu", "sdmc", "Nintendo 3DS", "id0", "id1", "title", "00040000",
			"00112233", "data", "00000001.sav"): CategorySaves,
		j(configs, "citron", "nand", "user", "save", "0000000000000000", "1", "save.dat"):   CategorySaves,
		j(configs, "ppsspp", "PSP", "SAVEDATA", "ULUS10000", "DATA.BIN"):                    CategorySaves,
		j(configs, "rpcs3", "dev_hdd0", "home", "00000001", "savedata", "BLUS0", "SYS.BIN"): CategorySaves,
		j(configs, "play", "Play Data Files", "vfs", "mc0", "BASLUS-20001", "save.bin"):     CategorySaves,
		j(configs, "redream", "vmu0.bin"):                                                   CategorySaves,
		j(configs, "redream", "saves", "game.dci"):                                          CategorySaves,
		j("saves", "supermodel", "NVRAM", "scud.nv"):                                        CategorySaves,
		j("system", "wine-bottles", "windows", "wine-proton", "Game.exe.wine", "drive_c", "users",
			"root", "Saved Games", "Game", "slot1.sav"): CategorySaves,
		j("system", "wine-bottles", "demul", "drive_c", "users", "root", "AppData", "Roaming",
			"demul", "nvram.bin"): CategorySaves,
		j(configs, "Ryujinx", "bis", "user", "save", "0000000000000001", "0", "save.dat"): CategorySaves,
		j("system", "wine-bottles", "windows", "Game.wine", "drive_c", "users", "batocera",
			"Documents", "Game", "save.dat"): CategorySaves,
		j("system", "wine-bottles", "model2", "model2emu", "NVDATA", "daytona.DAT"): CategorySaves,
		j("roms", "windows", "Port.wine", "drive_c", "users", "batocera", "AppData", "Roaming",
			"Port", "save.ini"): CategorySaves,
		// Savestates.
		j("saves", "snes", "game.state"):                                       CategorySavestates,
		j("saves", "snes", "game.state1.png"):                                  CategorySavestates,
		j("saves", "snes", "game.state.auto"):                                  CategorySavestates,
		j("saves", "nds", "game.ml1"):                                          CategorySavestates,
		j("saves", "ps2", "pcsx2", "sstates", "SLUS.p2s"):                      CategorySavestates,
		j("saves", "duckstation", "SLUS-00001_1.sav"):                          CategorySavestates,
		j("saves", "dolphin-emu", "StateSaves", "GALE01.s01"):                  CategorySavestates,
		j("saves", "mame", "state", "sf2", "1.sta"):                            CategorySavestates,
		j("saves", "3ds", "azahar-emu", "states", "game.cst"):                  CategorySavestates,
		j(configs, "ppsspp", "PSP", "PPSSPP_STATE", "ULUS10000_1.00_0.ppst"):   CategorySavestates,
		j(configs, "rpcs3", "savestates", "BLUS0", "game.SAVESTAT"):            CategorySavestates,
		j(configs, "play", "Play Data Files", "states", "SLUS_200.01.st0.zip"): CategorySavestates,
		j(configs, "redream", "states", "12345678.0.sav"):                      CategorySavestates,
		j("saves", "supermodel", "Saves", "scud.st0"):                          CategorySavestates,
		j("saves", "dreamcast", "flycast", "Game.state"):                       CategorySavestates,
	}
	excluded := []string{
		j("bios", "scph5501.bin"),
		j("roms", "snes", "game.sfc"),
		j("roms", "snes", "media", "images", "game.png"),
		j("roms", "snes", "sub", "gamelist.xml"),
		j("roms", "windows", "Port.wine", "drive_c", "Program Files", "Port", "port.exe"),
		j("roms", "windows", "Port.wine", "drive_c", "users", "batocera", "AppData", "Local",
			"Temp", "tmp.bin"),
		j("saves", "flatpak", "data", "steam.dat"),
		j("saves", "snes", "cache", "blob.bin"),
		j("saves", "3ds", "azahar-emu", "sdmc", "Nintendo 3DS", "id0", "id1", "title", "00040000",
			"00112233", "content", "game.app"),
		j("saves", "3ds", "azahar-emu", "nand", "data", "id0", "sysdata", "seed.db"),
		j("saves", "dolphin-emu", "Wii", "title", "00010000", "52534d45", "content", "title.tmd"),
		j("saves", "dolphin-emu", "Cache", "gc.bin"),
		j("saves", "switch", "citron", "keys", "prod.keys"),
		j("saves", "switch", "citron", "nand", "user", "Contents", "registered", "game.nca"),
		j("saves", "wiiu", "usr", "title", "00050000", "101c9400", "code", "game.rpx"),
		j("saves", "psvita", "ux0", "app", "PCSE0001", "eboot.bin"),
		j("saves", "xbox360", "0000000000000000", "4D5307E6", "00007000", "game.xex"),
		j("saves", "mame", "logs", "mame.log"),
		j(configs, "emulationstation", "imagecache.db"),
		j(configs, "emulationstation", "logs", "es_log.txt"),
		j(configs, "retroarch", "cores", "snes9x_libretro.info"),
		j(configs, "retroarch", "shaders", "crt.glsl"),
		j(configs, "pulse", "cookie"),
		j(configs, "citron", "sdmc", "atmosphere", "contents", "mod.bin"),
		j(configs, "citron", "keys", "prod.keys"),
		j(configs, "ppsspp", "PSP", "GAME", "HOMEBREW", "EBOOT.PBP"),
		j(configs, "ppsspp", "PSP", "SYSTEM", "DUMP", "log.txt"),
		j(configs, "rpcs3", "dev_hdd0", "game", "BLUS0", "USRDIR", "EBOOT.BIN"),
		j(configs, "rpcs3", "dev_flash", "sys", "internal", "vsh.self"),
		j(configs, "Ryujinx", "bis", "user", "Contents", "registered", "game.nca"),
		j(configs, "Ryujinx", "system", "prod.keys"),
		j(configs, "dosbox", "dosbox.log"),
		j("system", "wine-bottles", "windows", "Game.wine", "drive_c", "windows", "system32",
			"kernel32.dll"),
		j("system", "wine-bottles", "windows", "Game.wine", "drive_c", "users", "batocera",
			"AppData", "Local", "Temp", "tmp.bin"),
		j("system", "zaparoo"),
		j(configs, "play", "Play Data Files", "vfs", "hdd", "__common", "hdd.img"),
		j(configs, "redream", "redream"),
		j(configs, "redream", "boot.bin"),
		j(configs, "redream", "cache", "12345678.dat"),
		j(configs, "Ryujinx", "ryujinx"),
		j(configs, "Ryujinx", "libSkiaSharp.so"),
		j("system", "wine-bottles", "windows", "wine-proton", "Game.exe.wine", "drive_c", "windows",
			"system32", "kernel32.dll"),
		j("system", "wine-bottles", "windows", "Game.wsquashfs.work", "work", "x"),
	}
	for path := range durable {
		writeTestFile(t, filepath.Join(env.RootDir, path), "original "+path+"\n")
	}
	for _, path := range excluded {
		writeTestFile(t, filepath.Join(env.RootDir, path), "original "+path+"\n")
	}
	useBatoceraBackup(t, &env)

	info, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	staged := stageTestZip(t, info.Path)
	byRestorePath := make(map[string]FileRef, len(staged.Manifest.Files))
	for _, file := range staged.Manifest.Files {
		byRestorePath[file.RestorePath] = file
	}
	for path, category := range durable {
		file, ok := byRestorePath[filepath.ToSlash(path)]
		if assert.True(t, ok, "missing %s", path) {
			assert.Equal(t, category, file.Category, path)
		}
	}
	for _, path := range excluded {
		assert.NotContains(t, byRestorePath, filepath.ToSlash(path))
	}

	for path := range durable {
		writeTestFile(t, filepath.Join(env.RootDir, path), "changed\n")
	}
	_, err = env.Manager.Restore(context.Background(), info.Name)
	require.NoError(t, err)
	for path := range durable {
		data, readErr := os.ReadFile(filepath.Join(env.RootDir, path)) // #nosec G304 -- test-owned temp path.
		require.NoError(t, readErr)
		assert.Equal(t, "original "+path+"\n", string(data), path)
	}
}

func TestBatoceraRestorePolicyAcceptsFoldersMissingOnDestination(t *testing.T) {
	t.Parallel()
	env := newBackupTestEnv(t, platformids.Batocera)
	useBatoceraBackup(t, &env)

	restorePath := filepath.ToSlash(filepath.Join(
		"roms", "windows", "Elsewhere.wine", "drive_c", "users", "batocera", "Saved Games", "save.dat",
	))
	gamelist := filepath.ToSlash(filepath.Join("roms", "newsystem", "gamelist.xml"))
	err := env.Manager.validateManifestPolicy(&Manifest{Platform: platformids.Batocera, Files: []FileRef{
		{Category: CategoryZaparoo, RestorePath: "user.db", ArchivePath: zaparooArchive("user.db")},
		{Category: CategorySaves, RestorePath: restorePath, ArchivePath: platformArchive(restorePath)},
		{Category: CategorySettings, RestorePath: gamelist, ArchivePath: platformArchive(gamelist)},
	}})
	require.NoError(t, err)

	for _, invalid := range []FileRef{
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join("roms", "snes", "game.sfc"))},
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join(
			"roms", "snes", "sub", "gamelist.xml",
		))},
		{Category: CategorySaves, RestorePath: filepath.ToSlash(filepath.Join(
			"roms", "windows", "Game.pc", "drive_c", "users", "batocera", "Documents", "save.dat",
		))},
		{Category: CategorySettings, RestorePath: filepath.ToSlash(filepath.Join("bios", "scph5501.bin"))},
	} {
		invalid.ArchivePath = platformArchive(invalid.RestorePath)
		err = env.Manager.validateManifestPolicy(&Manifest{Platform: platformids.Batocera, Files: []FileRef{
			{Category: CategoryZaparoo, RestorePath: "user.db", ArchivePath: zaparooArchive("user.db")},
			invalid,
		}})
		require.ErrorContains(t, err, "not collected", invalid.RestorePath)
	}
}
