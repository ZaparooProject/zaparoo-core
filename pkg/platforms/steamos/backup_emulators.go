//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

type emulatorPathDefinition struct {
	rel      string
	category string
	include  []platforms.BackupPattern
	exclude  []platforms.BackupPattern
	nonRecur bool
}

func emulatorDefinitions(home string) []platforms.BackupDefinition {
	paths := []emulatorPathDefinition{
		// RetroArch.
		retroArchSettings(filepath.Join(".var", "app", "org.libretro.RetroArch", "config", "retroarch")),
		savePath(filepath.Join(".var", "app", "org.libretro.RetroArch", "config", "retroarch", "saves")),
		statePath(filepath.Join(".var", "app", "org.libretro.RetroArch", "config", "retroarch", "states")),

		// Dolphin and PrimeHack split profiles/configuration into XDG config and console data into XDG data.
		settingsPath(filepath.Join(".config", "dolphin-emu")),
		settingsPath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "config", "dolphin-emu")),
		settingsPath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Config")),
		settingsPath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "GameSettings")),
		savePath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "GC")),
		dolphinWiiSaves(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Wii")),
		savePath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "GBA", "Saves")),
		statePath(filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "StateSaves")),
		settingsPath(filepath.Join(".local", "share", "dolphin-emu", "Config")),
		settingsPath(filepath.Join(".local", "share", "dolphin-emu", "GameSettings")),
		savePath(filepath.Join(".local", "share", "dolphin-emu", "GC")),
		dolphinWiiSaves(filepath.Join(".local", "share", "dolphin-emu", "Wii")),
		savePath(filepath.Join(".local", "share", "dolphin-emu", "GBA", "Saves")),
		statePath(filepath.Join(".local", "share", "dolphin-emu", "StateSaves")),
		settingsPath(filepath.Join(".var", "app", "io.github.shiiion.primehack", "config", "dolphin-emu")),
		settingsPath(filepath.Join(".var", "app", "io.github.shiiion.primehack", "data", "dolphin-emu", "Config")),
		settingsPath(filepath.Join(
			".var", "app", "io.github.shiiion.primehack", "data", "dolphin-emu", "GameSettings",
		)),
		savePath(filepath.Join(".var", "app", "io.github.shiiion.primehack", "data", "dolphin-emu", "GC")),
		dolphinWiiSaves(filepath.Join(".var", "app", "io.github.shiiion.primehack", "data", "dolphin-emu", "Wii")),
		statePath(filepath.Join(".var", "app", "io.github.shiiion.primehack", "data", "dolphin-emu", "StateSaves")),

		// PPSSPP.
		ppssppSettings(filepath.Join(".var", "app", "org.ppsspp.PPSSPP", "config", "ppsspp")),
		savePath(filepath.Join(".var", "app", "org.ppsspp.PPSSPP", "config", "ppsspp", "PSP", "SAVEDATA")),
		statePath(filepath.Join(".var", "app", "org.ppsspp.PPSSPP", "config", "ppsspp", "PSP", "PPSSPP_STATE")),

		// PCSX2 native/AppImage and Flatpak.
		settingsPath(filepath.Join(".config", "PCSX2")),
		savePath(filepath.Join(".config", "PCSX2", "memcards")),
		statePath(filepath.Join(".config", "PCSX2", "sstates")),
		settingsPath(filepath.Join(".var", "app", "net.pcsx2.PCSX2", "config", "PCSX2")),
		savePath(filepath.Join(".var", "app", "net.pcsx2.PCSX2", "config", "PCSX2", "memcards")),
		statePath(filepath.Join(".var", "app", "net.pcsx2.PCSX2", "config", "PCSX2", "sstates")),

		// Cemu configuration and user-supplied console material. Installed titles under mlc01 are excluded.
		cemuSettings(filepath.Join(".config", "Cemu")),
		nonRecursiveSettings(filepath.Join(".local", "share", "Cemu"), "keys.txt", "otp.bin", "seeprom.bin"),
		cemuSettings(filepath.Join(".var", "app", "info.cemu.Cemu", "config", "Cemu")),
		nonRecursiveSettings(
			filepath.Join(".var", "app", "info.cemu.Cemu", "data", "Cemu"),
			"keys.txt", "otp.bin", "seeprom.bin",
		),

		// xemu stores configuration and mutable EEPROM data under XDG data.
		nonRecursiveSettings(filepath.Join(".local", "share", "xemu"), "xemu.toml", "xemu.toml.bak"),
		nonRecursiveSave(filepath.Join(".local", "share", "xemu"), "eeprom.bin"),
		nonRecursiveSettings(
			filepath.Join(".var", "app", "app.xemu.xemu", "data", "xemu", "xemu"),
			"xemu.toml", "xemu.toml.bak",
		),
		nonRecursiveSave(
			filepath.Join(".var", "app", "app.xemu.xemu", "data", "xemu", "xemu"), "eeprom.bin",
		),

		// Citra and Azahar. CIA title content is excluded; title data/extdata and states are preserved separately.
		citraSettings(filepath.Join(".local", "share", "citra-emu")),
		citraSaves(filepath.Join(".local", "share", "citra-emu", "sdmc")),
		citraNANDSaves(filepath.Join(".local", "share", "citra-emu", "nand")),
		statePath(filepath.Join(".local", "share", "citra-emu", "states")),
		citraSettings(filepath.Join(".var", "app", "org.citra_emu.citra", "data", "citra-emu")),
		citraSaves(filepath.Join(".var", "app", "org.citra_emu.citra", "data", "citra-emu", "sdmc")),
		citraNANDSaves(filepath.Join(".var", "app", "org.citra_emu.citra", "data", "citra-emu", "nand")),
		statePath(filepath.Join(".var", "app", "org.citra_emu.citra", "data", "citra-emu", "states")),
		citraSettings(filepath.Join(".local", "share", "azahar-emu")),
		citraSaves(filepath.Join(".local", "share", "azahar-emu", "sdmc")),
		citraNANDSaves(filepath.Join(".local", "share", "azahar-emu", "nand")),
		statePath(filepath.Join(".local", "share", "azahar-emu", "states")),
		citraSettings(filepath.Join(".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu")),
		citraSaves(filepath.Join(".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "sdmc")),
		citraNANDSaves(filepath.Join(".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "nand")),
		statePath(filepath.Join(".var", "app", "org.azahar_emu.Azahar", "data", "azahar-emu", "states")),

		// Ryujinx and Ryubing configuration, firmware/keys, profiles, and user/system saves.
		ryujinxSettings(filepath.Join(".config", "Ryujinx")),
		savePath(filepath.Join(".config", "Ryujinx", "bis", "user", "save")),
		savePath(filepath.Join(".config", "Ryujinx", "bis", "user", "saveMeta")),
		savePath(filepath.Join(".config", "Ryujinx", "bis", "user", "savemeta")),
		savePath(filepath.Join(".config", "Ryujinx", "bis", "system", "save")),
		ryujinxSettings(filepath.Join(".config", "Ryubing")),
		savePath(filepath.Join(".config", "Ryubing", "bis", "user", "save")),
		savePath(filepath.Join(".config", "Ryubing", "bis", "user", "saveMeta")),
		savePath(filepath.Join(".config", "Ryubing", "bis", "user", "savemeta")),
		savePath(filepath.Join(".config", "Ryubing", "bis", "system", "save")),
		ryujinxSettings(filepath.Join(".var", "app", "org.ryujinx.Ryujinx", "config", "Ryujinx")),
		savePath(filepath.Join(".var", "app", "org.ryujinx.Ryujinx", "config", "Ryujinx", "bis", "user", "save")),
		savePath(filepath.Join(".var", "app", "org.ryujinx.Ryujinx", "config", "Ryujinx", "bis", "user", "saveMeta")),
		savePath(filepath.Join(".var", "app", "org.ryujinx.Ryujinx", "config", "Ryujinx", "bis", "user", "savemeta")),
		savePath(filepath.Join(".var", "app", "org.ryujinx.Ryujinx", "config", "Ryujinx", "bis", "system", "save")),
		ryujinxSettings(filepath.Join(".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx")),
		savePath(filepath.Join(".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx", "bis", "user", "save")),
		savePath(filepath.Join(
			".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx", "bis", "user", "saveMeta",
		)),
		savePath(filepath.Join(
			".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx", "bis", "user", "savemeta",
		)),
		savePath(filepath.Join(
			".var", "app", "io.github.ryubing.Ryujinx", "config", "Ryujinx", "bis", "system", "save",
		)),

		// RPCS3: keep configuration, firmware, user homes, virtual memory cards, and states without installed games.
		rpcs3Settings(filepath.Join(".config", "rpcs3")),
		settingsPath(filepath.Join(".config", "rpcs3", "dev_flash")),
		savePath(filepath.Join(".config", "rpcs3", "dev_hdd0", "home")),
		savePath(filepath.Join(".config", "rpcs3", "dev_hdd0", "savedata")),
		savePath(filepath.Join(".config", "rpcs3", "dev_usb000", "PS3", "SAVEDATA")),
		statePath(filepath.Join(".config", "rpcs3", "savestates")),
		rpcs3Settings(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3")),
		settingsPath(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_flash")),
		savePath(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_hdd0", "home")),
		savePath(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_hdd0", "savedata")),
		savePath(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "dev_usb000", "PS3", "SAVEDATA")),
		statePath(filepath.Join(".var", "app", "net.rpcs3.RPCS3", "config", "rpcs3", "savestates")),

		// Vita3K: keep configuration, firmware partitions, and per-user data without apps/patches.
		settingsPath(filepath.Join(".config", "Vita3K")),
		settingsPath(filepath.Join(".local", "share", "Vita3K", "vs0")),
		savePath(filepath.Join(".local", "share", "Vita3K", "ux0", "user")),
		settingsPath(filepath.Join(".var", "app", "org.vita3k.Vita3K", "config", "Vita3K")),
		settingsPath(filepath.Join(".var", "app", "org.vita3k.Vita3K", "data", "Vita3K", "vs0")),
		savePath(filepath.Join(".var", "app", "org.vita3k.Vita3K", "data", "Vita3K", "ux0", "user")),

		// DuckStation and melonDS.
		settingsPath(filepath.Join(".local", "share", "duckstation")),
		savePath(filepath.Join(".local", "share", "duckstation", "memcards")),
		statePath(filepath.Join(".local", "share", "duckstation", "savestates")),
		settingsPath(filepath.Join(".var", "app", "org.duckstation.DuckStation", "config", "duckstation")),
		savePath(filepath.Join(".var", "app", "org.duckstation.DuckStation", "config", "duckstation", "memcards")),
		statePath(filepath.Join(".var", "app", "org.duckstation.DuckStation", "config", "duckstation", "savestates")),
		settingsPath(filepath.Join(".config", "melonDS")),
		settingsPath(filepath.Join(".var", "app", "net.kuribo64.melonDS", "config", "melonDS")),

		// RMG and mGBA.
		settingsPath(filepath.Join(".config", "RMG")),
		savePath(filepath.Join(".local", "share", "RMG", "Save")),
		settingsPath(filepath.Join(".var", "app", "com.github.Rosalie241.RMG", "config", "RMG")),
		savePath(filepath.Join(".var", "app", "com.github.Rosalie241.RMG", "data", "RMG", "Save")),
		settingsPath(filepath.Join(".config", "mgba")),
		settingsPath(filepath.Join(".var", "app", "io.mgba.mGBA", "config", "mgba")),

		// MAME and Supermodel persisted user trees.
		mameSettings(".mame"),
		savePath(filepath.Join(".mame", "nvram")),
		savePath(filepath.Join(".mame", "memcard")),
		savePath(filepath.Join(".mame", "diff")),
		statePath(filepath.Join(".mame", "sta")),
		mameSettings(filepath.Join(".var", "app", "org.mamedev.MAME", ".mame")),
		savePath(filepath.Join(".var", "app", "org.mamedev.MAME", ".mame", "nvram")),
		savePath(filepath.Join(".var", "app", "org.mamedev.MAME", ".mame", "memcard")),
		savePath(filepath.Join(".var", "app", "org.mamedev.MAME", ".mame", "diff")),
		statePath(filepath.Join(".var", "app", "org.mamedev.MAME", ".mame", "sta")),
		settingsPath(filepath.Join(".supermodel", "Config")),
		savePath(filepath.Join(".supermodel", "NVRAM")),
		settingsPath(filepath.Join(".var", "app", "com.supermodel3.Supermodel", ".supermodel", "Config")),
		savePath(filepath.Join(".var", "app", "com.supermodel3.Supermodel", ".supermodel", "NVRAM")),

		// Flycast VMUs/NVRAM and configuration.
		settingsPath(filepath.Join(".config", "flycast")),
		flycastSaves(filepath.Join(".local", "share", "flycast")),
		settingsPath(filepath.Join(".var", "app", "org.flycast.Flycast", "config", "flycast")),
		flycastSaves(filepath.Join(".var", "app", "org.flycast.Flycast", "data", "flycast")),

		// ScummVM saves and configuration.
		settingsPath(filepath.Join(".config", "scummvm")),
		savePath(filepath.Join(".local", "share", "scummvm", "saves")),
		settingsPath(filepath.Join(".var", "app", "org.scummvm.ScummVM", "config", "scummvm")),
		savePath(filepath.Join(".var", "app", "org.scummvm.ScummVM", "data", "scummvm", "saves")),

		// Ruffle SharedObjects are game progress, not cache.
		settingsPath(filepath.Join(".config", "ruffle")),
		savePath(filepath.Join(".local", "share", "ruffle", "SharedObjects")),
		settingsPath(filepath.Join(".var", "app", "rs.ruffle.Ruffle", "config", "ruffle")),
		savePath(filepath.Join(".var", "app", "rs.ruffle.Ruffle", "data", "ruffle", "SharedObjects")),

		// shadPS4 configuration, firmware modules, input profiles, saves, and trophies.
		settingsPath(filepath.Join(".config", "shadps4")),
		settingsPath(filepath.Join(".config", "shadPS4")),
		shadPS4Settings(filepath.Join(".local", "share", "shadps4")),
		shadPS4Saves(filepath.Join(".local", "share", "shadps4")),
		shadPS4Settings(filepath.Join(".local", "share", "shadPS4")),
		shadPS4Saves(filepath.Join(".local", "share", "shadPS4")),
		settingsPath(filepath.Join(".var", "app", "net.shadps4.shadPS4", "config", "shadps4")),
		settingsPath(filepath.Join(".var", "app", "net.shadps4.shadPS4", "config", "shadPS4")),
		shadPS4Settings(filepath.Join(".var", "app", "net.shadps4.shadPS4", "data", "shadps4")),
		shadPS4Saves(filepath.Join(".var", "app", "net.shadps4.shadPS4", "data", "shadps4")),
		shadPS4Settings(filepath.Join(".var", "app", "net.shadps4.shadPS4", "data", "shadPS4")),
		shadPS4Saves(filepath.Join(".var", "app", "net.shadps4.shadPS4", "data", "shadPS4")),
	}

	definitions := make([]platforms.BackupDefinition, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, item := range paths {
		key := item.category + "\x00" + item.rel
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		def := definition(
			filepath.Join(home, item.rel), item.rel, item.category, item.include, item.exclude,
		)
		def.NonRecursive = item.nonRecur
		definitions = append(definitions, def)
	}
	return definitions
}

func retroArchSettings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}},
		exclude: appendBackupPatterns(emulatorConfigExclusions,
			platforms.BackupPattern{Contains: "assets/"},
			platforms.BackupPattern{Contains: "database/"},
			platforms.BackupPattern{Contains: "filters/"},
			platforms.BackupPattern{Contains: "info/"},
			platforms.BackupPattern{Contains: "overlays/"},
		),
	}
}

func settingsPath(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}}, exclude: emulatorConfigExclusions,
	}
}

func savePath(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves, include: []platforms.BackupPattern{{All: true}},
	}
}

func statePath(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySavestates, include: []platforms.BackupPattern{{All: true}},
	}
}

func nonRecursiveSettings(rel string, names ...string) emulatorPathDefinition {
	include := make([]platforms.BackupPattern, 0, len(names))
	for _, name := range names {
		include = append(include, platforms.BackupPattern{Glob: name})
	}
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings, include: include, nonRecur: true,
	}
}

func nonRecursiveSave(rel string, names ...string) emulatorPathDefinition {
	include := make([]platforms.BackupPattern, 0, len(names))
	for _, name := range names {
		include = append(include, platforms.BackupPattern{Glob: name})
	}
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves, include: include, nonRecur: true,
	}
}

func ppssppSettings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}},
		exclude: appendBackupPatterns(emulatorConfigExclusions,
			platforms.BackupPattern{Glob: "PSP/SYSTEM/DUMP/**"},
		),
	}
}

func cemuSettings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}},
		exclude: appendBackupPatterns(emulatorConfigExclusions,
			platforms.BackupPattern{Glob: "cemu/**"},
		),
	}
}

func citraSettings(rel string) emulatorPathDefinition {
	exclude := append([]platforms.BackupPattern{}, emulatorConfigExclusions...)
	exclude = append(exclude,
		platforms.BackupPattern{Glob: "log/**"},
		platforms.BackupPattern{Contains: "sdmc/"},
		platforms.BackupPattern{Contains: "nand/data/"},
		platforms.BackupPattern{Contains: "nand/title/"},
	)
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}}, exclude: exclude,
	}
}

func citraSaves(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves,
		include: []platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}},
	}
}

func citraNANDSaves(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves,
		include: []platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}},
	}
}

func ryujinxSettings(rel string) emulatorPathDefinition {
	exclude := appendBackupPatterns(emulatorConfigExclusions,
		platforms.BackupPattern{Contains: "bis/user/save/"},
		platforms.BackupPattern{Contains: "bis/user/savemeta/"},
		platforms.BackupPattern{Contains: "bis/system/save/"},
		platforms.BackupPattern{Contains: "bis/user/contents/"},
		platforms.BackupPattern{Contains: "games/"},
	)
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{{All: true}}, exclude: exclude,
	}
}

func rpcs3Settings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{
			{Glob: "*.yml"},
			{Glob: "*.yaml"},
			{Glob: "*.dat"},
			{Glob: "*.ini"},
			{Contains: "config/"},
			{Contains: "guiconfigs/"},
			{Contains: "input_configs/"},
			{Contains: "patches/"},
		},
		exclude: appendBackupPatterns(emulatorConfigExclusions,
			platforms.BackupPattern{Contains: "dev_bdvd/"},
			platforms.BackupPattern{Contains: "dev_hdd0/game/"},
			platforms.BackupPattern{Contains: "dev_hdd0/savedata/"},
			platforms.BackupPattern{Contains: "dev_hdd1/"},
			platforms.BackupPattern{Contains: "dev_usb000/"},
		),
	}
}

func mameSettings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{
			{Glob: "*.ini"}, {Contains: "cfg/"}, {Contains: "ctrlr/"}, {Contains: "ini/"},
		},
		exclude: emulatorConfigExclusions,
	}
}

func dolphinWiiSaves(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves,
		include: []platforms.BackupPattern{
			{Glob: "shared2/sys/**"},
			{Glob: "shared2/wc24/**"},
			{Glob: "shared2/menu/FaceLib/**"},
			{Glob: "sys/**"},
			{Glob: "title/*/*/data/**"},
		},
	}
}

func shadPS4Settings(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySettings,
		include: []platforms.BackupPattern{
			{Glob: "*.ini"},
			{Glob: "*.json"},
			{Glob: "*.toml"},
			{Glob: "*.xml"},
			{Glob: "*.yaml"},
			{Glob: "*.yml"},
			{Contains: "cheats/"},
			{Contains: "custom_configs/"},
			{Contains: "inputs/"},
			{Contains: "patches/"},
			{Contains: "sys_modules/"},
		},
		exclude: appendBackupPatterns(emulatorConfigExclusions,
			platforms.BackupPattern{Contains: "captures/"},
			platforms.BackupPattern{Contains: "download/"},
			platforms.BackupPattern{Contains: "fonts/"},
			platforms.BackupPattern{Contains: "game_data/"},
			platforms.BackupPattern{Contains: "savedata/"},
			platforms.BackupPattern{Contains: "shader/"},
			platforms.BackupPattern{Contains: "temp/"},
			platforms.BackupPattern{Contains: "trophy/"},
		),
	}
}

func shadPS4Saves(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves,
		include: []platforms.BackupPattern{
			{Glob: "savedata/**"},
			{Glob: "trophy/**"},
			{Glob: "custom_trophy/**"},
			{Glob: "home/*/savedata/**"},
			{Glob: "home/*/trophy/**"},
		},
	}
}

func flycastSaves(rel string) emulatorPathDefinition {
	return emulatorPathDefinition{
		rel: rel, category: backupCategorySaves,
		include: []platforms.BackupPattern{
			{Glob: "*.bin"},
			{Glob: "*.vmu"},
			{Glob: "*.nvm"},
			{Glob: "*.flash"},
			{Contains: "vmu"},
		},
	}
}
