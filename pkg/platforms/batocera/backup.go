//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/winebackup"
)

// Paths below follow Batocera's configgen (batoceraPaths.py and the
// per-emulator generators): RetroArch and most standalone emulators write
// saves under /userdata/saves, while a few keep save data in their config
// directory under /userdata/system/configs.
const (
	userdataRoot  = "/userdata"
	procMounts    = "/proc/mounts"
	maxMountsSize = int64(1 << 20)

	categorySettings   = "settings"
	categoryInputs     = "inputs"
	categorySaves      = "saves"
	categorySavestates = "savestates"
)

var (
	// savestatePatterns separates savestates from saves. RetroArch writes
	// both into saves/<system>/, naming states *.state, *.stateN and
	// *.state.auto with optional .png thumbnails; melonDS uses *.mlN.
	// Supermodel keeps NVRAM in saves/supermodel/NVRAM and states in
	// saves/supermodel/Saves.
	savestatePatterns = []platforms.BackupPattern{
		{Glob: "*.state*"},
		{Glob: "*.ml[0-9]"},
		{Glob: "supermodel/Saves/**"},
		{Contains: "states/"},
		{Contains: "statesaves/"},
		{Contains: "savestates/"},
		{Contains: "sstates/"},
		{Contains: "ppsspp_state/"},
	}
	// ownedSaveTrees are saves/ subtrees with their own definitions because
	// they mix saves with installed games, firmware, keys or states that the
	// generic rules cannot tell apart.
	ownedSaveTrees = []platforms.BackupPattern{
		{Glob: "3ds/**"},
		{Glob: "dolphin-emu/**"},
		{Glob: "duckstation/**"},
		{Glob: "mame/**"},
		{Glob: "psvita/**"},
		{Glob: "switch/**"},
		{Glob: "wiiu/**"},
		{Glob: "xbox/**"},
		{Glob: "xbox360/**"},
	}
	// saveTreeExclusions drops data under saves/ that is not save data:
	// Steam's flatpak (Steam Cloud covers its games), caches and logs.
	saveTreeExclusions = []platforms.BackupPattern{
		{Glob: "flatpak/**"},
		{Contains: "cache/"},
		{Contains: "logs/"},
	}
	secretExclusions = []platforms.BackupPattern{
		{Glob: "*.keys"},
		{Glob: "*.pem"},
		{Glob: "*.key"},
		{Glob: "*/keys/**"},
	}
	// configExclusions keeps generated, downloaded and save data out of
	// emulator settings. Save data kept under configs has its own definitions.
	configExclusions = []platforms.BackupPattern{
		{Glob: "*.log"},
		{Glob: "*.so"},
		{Contains: "cache/"},
		{Contains: "caches/"},
		{Contains: "logs/"},
		{Contains: "cores/"},
		{Contains: "downloads/"},
		{Contains: "downloaded_media/"},
		{Contains: "covers/"},
		{Contains: "screenshots/"},
		{Contains: "shaders/"},
		{Contains: "textures/"},
		{Contains: "texture_packs/"},
		{Contains: "themes/"},
		{Contains: "artwork/"},
		{Contains: "thumbnails/"},
		{Contains: "media/"},
		{Contains: "saves/"},
		{Contains: "states/"},
		{Contains: "statesaves/"},
		{Contains: "savestates/"},
		{Contains: "savedata/"},
		{Contains: "memcards/"},
		{Contains: "memorycards/"},
		{Contains: "sstates/"},
		{Contains: "ppsspp_state/"},
	}
	// configOwnedTrees are configs/ subtrees covered by other definitions or
	// holding installed games, firmware, keys, runtime state or the emulator
	// binaries configgen copies there (Redream, Ryujinx).
	configOwnedTrees = []platforms.BackupPattern{
		{Glob: "emulationstation/**"},
		{Glob: "evmapy/**"},
		{Glob: "retroarch/**"},
		{Glob: "pulse/**"},
		{Glob: "citron/nand/**"},
		{Glob: "citron/sdmc/**"},
		{Glob: "play/Play Data Files/vfs/**"},
		{Glob: "ppsspp/PSP/GAME/**"},
		{Glob: "ppsspp/PSP/SYSTEM/DUMP/**"},
		{Glob: "redream/redream"},
		{Glob: "redream/boot.bin"},
		{Glob: "redream/vmu*.bin"},
		{Glob: "rpcs3/dev_*/**"},
		{Glob: "Ryujinx/ryujinx"},
		{Glob: "Ryujinx/bis/**"},
		{Glob: "Ryujinx/sdcard/**"},
		{Glob: "Ryujinx/system/**"},
	}
	// winePrune stops the walk from descending into Windows system and game
	// install trees, which hold no user saves.
	winePrune = []platforms.BackupPattern{
		{Glob: "*/drive_c/windows/**"},
		{Glob: "*/drive_c/Program Files*/**"},
		{Glob: "*/*/drive_c/windows/**"},
		{Glob: "*/*/drive_c/Program Files*/**"},
		{Glob: "*/*/*/drive_c/windows/**"},
		{Glob: "*/*/*/drive_c/Program Files*/**"},
		// batocera-wine's overlay work folder for squashfs games.
		{Glob: "*/*.work/**"},
	}
)

// BackupRestoreRoot anchors every Batocera restore path under /userdata.
func (*Platform) BackupRestoreRoot() string {
	return userdataRoot
}

// BackupDefinitions returns the restore allow-list for /userdata.
func (*Platform) BackupDefinitions() []platforms.BackupDefinition {
	return BackupDefinitions(userdataRoot)
}

// BackupPlan returns the collection plan for /userdata.
func (*Platform) BackupPlan() platforms.BackupPlan {
	return BackupPlan(userdataRoot, procMounts)
}

// BackupDefinitions returns the restore allow-list rooted at root: everything
// BackupPlan collects plus the policy for any Windows prefix under
// roms/windows, so a prefix that exists only on the destination still
// restores.
func BackupDefinitions(root string) []platforms.BackupDefinition {
	definitions := durableDefinitions(root)
	definitions = append(definitions, romDefinitions(root)...)
	return append(definitions, gamelistPolicy(root), romPrefixPolicy(root))
}

// BackupPlan walks the durable roots under root, the gamelist in each
// system folder and the Windows prefixes found under roms/windows. Saves on a network share listed in mountsPath are
// still collected, with a warning.
func BackupPlan(root, mountsPath string) platforms.BackupPlan {
	definitions := durableDefinitions(root)
	definitions = append(definitions, romDefinitions(root)...)
	return platforms.BackupPlan{
		Definitions: definitions,
		Warnings:    networkMountWarnings(root, mountsPath),
	}
}

func durableDefinitions(root string) []platforms.BackupDefinition {
	definitions := make([]platforms.BackupDefinition, 0, 48)
	definitions = append(definitions, settingsDefinitions(root)...)
	definitions = append(definitions, inputDefinitions(root)...)
	definitions = append(definitions, saveTreeDefinitions(root)...)
	definitions = append(definitions, ownedSaveDefinitions(root)...)
	definitions = append(definitions, configSaveDefinitions(root)...)
	return append(definitions, wineBottleDefinitions(root)...)
}

func definition(
	root, rel, category string, include, exclude []platforms.BackupPattern,
) platforms.BackupDefinition {
	source := filepath.Join(root, rel)
	return platforms.BackupDefinition{
		SourceRoot: source, RestoreRoot: rel, Category: category,
		Include: include, Exclude: exclude, SourceTrustedRoots: []string{source},
	}
}

func nonRecursiveDefinition(
	root, rel, category string, include []platforms.BackupPattern,
) platforms.BackupDefinition {
	result := definition(root, rel, category, include, nil)
	result.NonRecursive = true
	return result
}

func all() []platforms.BackupPattern {
	return []platforms.BackupPattern{{All: true}}
}

func globs(names ...string) []platforms.BackupPattern {
	patterns := make([]platforms.BackupPattern, 0, len(names))
	for _, name := range names {
		patterns = append(patterns, platforms.BackupPattern{Glob: name})
	}
	return patterns
}

func join(groups ...[]platforms.BackupPattern) []platforms.BackupPattern {
	var size int
	for _, group := range groups {
		size += len(group)
	}
	result := make([]platforms.BackupPattern, 0, size)
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

func settingsDefinitions(root string) []platforms.BackupDefinition {
	configs := filepath.Join("system", "configs")
	retroarch := filepath.Join(configs, "retroarch")
	return []platforms.BackupDefinition{
		// configgen regenerates each emulator's config from batocera.conf on
		// launch, so this file carries most of the user's settings.
		nonRecursiveDefinition(root, "system", categorySettings, globs("batocera.conf")),
		definition(root, filepath.Join(configs, "emulationstation"), categorySettings,
			globs("es_settings.cfg", "es_systems_*.cfg", "es_features_*.cfg", "collections/**", "scripts/**"),
			nil),
		nonRecursiveDefinition(root, retroarch, categorySettings, globs("*.cfg")),
		definition(root, retroarch, categorySettings,
			globs("cores/retroarch-core-options.cfg", "config/**"), globs("config/remaps/**")),
		definition(root, configs, categorySettings, all(),
			join(configOwnedTrees, configExclusions, secretExclusions)),
	}
}

func inputDefinitions(root string) []platforms.BackupDefinition {
	configs := filepath.Join("system", "configs")
	return []platforms.BackupDefinition{
		nonRecursiveDefinition(root, filepath.Join(configs, "emulationstation"), categoryInputs,
			globs("es_input.cfg")),
		definition(root, filepath.Join(configs, "retroarch", "config", "remaps"), categoryInputs,
			all(), nil),
		definition(root, filepath.Join(configs, "evmapy"), categoryInputs, all(), nil),
	}
}

func saveTreeDefinitions(root string) []platforms.BackupDefinition {
	return []platforms.BackupDefinition{
		definition(root, "saves", categorySaves, all(),
			join(savestatePatterns, ownedSaveTrees, saveTreeExclusions)),
		definition(root, "saves", categorySavestates, savestatePatterns,
			join(ownedSaveTrees, saveTreeExclusions)),
	}
}

func ownedSaveDefinitions(root string) []platforms.BackupDefinition {
	saves := "saves"
	azahar := filepath.Join(saves, "3ds", "azahar-emu")
	dolphin := filepath.Join(saves, "dolphin-emu")
	duckstation := filepath.Join(saves, "duckstation")
	mame := filepath.Join(saves, "mame")
	mameExclusions := join(saveTreeExclusions, globs("*.log"))
	return []platforms.BackupDefinition{
		// Azahar keeps installed titles and keys (sysdata) beside saves, so
		// only the save data folders are taken.
		definition(root, filepath.Join(azahar, "sdmc"), categorySaves,
			[]platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}},
			[]platforms.BackupPattern{{Contains: "/content/"}}),
		definition(root, filepath.Join(azahar, "nand"), categorySaves,
			[]platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}},
			[]platforms.BackupPattern{{Contains: "/content/"}, {Contains: "sysdata/"}}),
		definition(root, filepath.Join(azahar, "states"), categorySavestates, all(), nil),

		// Dolphin's Wii folder is a NAND; only the save set is user data.
		definition(root, dolphin, categorySaves,
			globs(
				"GC/**", "GBA/Saves/**", "Wii/shared2/sys/**", "Wii/shared2/wc24/**",
				"Wii/shared2/menu/FaceLib/**", "Wii/sys/**", "Wii/title/*/*/data/**",
			),
			saveTreeExclusions),
		definition(root, filepath.Join(dolphin, "StateSaves"), categorySavestates, all(), nil),
		definition(root, filepath.Join(dolphin, "GameSettings"), categorySettings, all(), nil),

		// DuckStation writes memory cards to memcards/ and states as *.sav
		// at the top of its folder.
		definition(root, filepath.Join(duckstation, "memcards"), categorySaves, all(), nil),
		nonRecursiveDefinition(root, duckstation, categorySavestates, globs("*.sav")),

		definition(root, mame, categorySaves, all(),
			join(mameExclusions, []platforms.BackupPattern{{Contains: "cfg/"}, {Contains: "state/"}})),
		definition(root, mame, categorySettings,
			[]platforms.BackupPattern{{Contains: "cfg/"}}, mameExclusions),
		definition(root, mame, categorySavestates,
			[]platforms.BackupPattern{{Contains: "state/"}},
			join(mameExclusions, []platforms.BackupPattern{{Contains: "cfg/"}})),

		// Vita3K's ux0/app holds installed games; ux0/user holds saves.
		definition(root, filepath.Join(saves, "psvita", "ux0", "user"), categorySaves, all(), nil),

		// Switch emulators keep firmware, keys and installed content beside
		// the save folders.
		definition(root, filepath.Join(saves, "switch"), categorySaves,
			globs(
				"*/bis/user/save/**", "*/bis/user/saveMeta/**", "*/bis/system/save/**",
				"*/nand/user/save/**", "*/nand/system/save/**",
			),
			nil),

		// Cemu's mlc root is saves/wiiu; installed games and updates sit in
		// usr/title and sys.
		definition(root, filepath.Join(saves, "wiiu", "usr", "save"), categorySaves, all(), nil),

		// xemu's hard disk usually exceeds the per-file limit and is skipped
		// with a warning; a small one is kept.
		nonRecursiveDefinition(root, filepath.Join(saves, "xbox"), categorySaves,
			globs("xbox_hdd.qcow2", "eeprom.bin")),

		// Xenia's content root: 00000001 is save data, FFFE07D1 profiles.
		definition(root, filepath.Join(saves, "xbox360"), categorySaves,
			[]platforms.BackupPattern{{Contains: "00000001/"}, {Contains: "fffe07d1/"}}, nil),
	}
}

func configSaveDefinitions(root string) []platforms.BackupDefinition {
	configs := filepath.Join("system", "configs")
	return []platforms.BackupDefinition{
		definition(root, filepath.Join(configs, "citron", "nand"), categorySaves,
			globs("user/save/**", "system/save/**"), nil),
		// Play! (Batocera 43 and older) keeps memory cards in vfs/mc0 and
		// vfs/mc1 beside the PS2 hard disk image.
		definition(root, filepath.Join(configs, "play", "Play Data Files"), categorySaves,
			globs("vfs/mc0/**", "vfs/mc1/**"), nil),
		definition(root, filepath.Join(configs, "play", "Play Data Files", "states"), categorySavestates,
			all(), nil),
		definition(root, filepath.Join(configs, "ppsspp", "PSP", "SAVEDATA"), categorySaves, all(), nil),
		definition(root, filepath.Join(configs, "ppsspp", "PSP", "PPSSPP_STATE"), categorySavestates,
			all(), nil),
		// Redream runs from its config folder and keeps its data beside it:
		// VMUs at the top, per-game saves in saves/ and states in states/.
		nonRecursiveDefinition(root, filepath.Join(configs, "redream"), categorySaves, globs("vmu*.bin")),
		definition(root, filepath.Join(configs, "redream", "saves"), categorySaves, all(), nil),
		definition(root, filepath.Join(configs, "redream", "states"), categorySavestates, all(), nil),
		definition(root, filepath.Join(configs, "rpcs3", "dev_hdd0", "home"), categorySaves, all(), nil),
		definition(root, filepath.Join(configs, "rpcs3", "savestates"), categorySavestates, all(), nil),
		definition(root, filepath.Join(configs, "Ryujinx", "bis"), categorySaves,
			globs("user/save/**", "user/saveMeta/**", "system/save/**"), nil),
	}
}

// wineBottleDefinitions covers the Wine bottles: configgen's one bottle per
// emulator (wine-bottles/<name>) and batocera-wine's one per game
// (wine-bottles/<system>/<wine version>/<game>.wine).
func wineBottleDefinitions(root string) []platforms.BackupDefinition {
	bottles := filepath.Join("system", "wine-bottles")
	model2 := filepath.Join(bottles, "model2", "model2emu")
	depths := 3
	include := make([]platforms.BackupPattern, 0, depths*len(winebackup.UserSavePatterns))
	exclude := make([]platforms.BackupPattern, 0, len(winePrune)+depths*len(winebackup.SaveExclusions))
	exclude = append(exclude, winePrune...)
	for _, depth := range []string{"*", filepath.Join("*", "*"), filepath.Join("*", "*", "*")} {
		users := filepath.Join(depth, "drive_c", "users")
		include = append(include, winebackup.PrefixPatterns(users, winebackup.UserSavePatterns)...)
		exclude = append(exclude, winebackup.PrefixPatterns(users, winebackup.SaveExclusions)...)
	}
	return []platforms.BackupDefinition{
		definition(root, bottles, categorySettings, globs("*/*.reg", "*/*/*.reg", "*/*/*/*.reg"), winePrune),
		definition(root, bottles, categorySaves, include, exclude),
		definition(root, filepath.Join(model2, "NVDATA"), categorySaves, all(), nil),
		definition(root, filepath.Join(model2, "CFG"), categorySettings, all(), nil),
	}
}

// romDefinitions covers the files kept beside ROMs. EmulationStation stores
// favorites, play counts and last played in each system's gamelist.xml, and
// Windows games can ship as ready-made Wine prefixes
// (roms/windows/<game>.wine). Each system folder and prefix is read on its
// own so the ROM folders are never walked.
func romDefinitions(root string) []platforms.BackupDefinition {
	entries, err := os.ReadDir(filepath.Join(root, "roms"))
	if err != nil {
		return nil
	}
	var definitions []platforms.BackupDefinition
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		definitions = append(definitions, nonRecursiveDefinition(
			root, filepath.Join("roms", entry.Name()), categorySettings, globs("gamelist.xml")))
	}
	return append(definitions, romPrefixDefinitions(root)...)
}

func romPrefixDefinitions(root string) []platforms.BackupDefinition {
	windows := filepath.Join("roms", "windows")
	entries, err := os.ReadDir(filepath.Join(root, windows))
	if err != nil {
		return nil
	}
	var definitions []platforms.BackupDefinition
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".wine") {
			continue
		}
		prefix := filepath.Join(windows, entry.Name())
		definitions = append(definitions,
			nonRecursiveDefinition(root, prefix, categorySettings, globs("*.reg")),
			definition(root, filepath.Join(prefix, "drive_c", "users"), categorySaves,
				winebackup.UserSavePatterns, winebackup.SaveExclusions),
		)
	}
	return definitions
}

// gamelistPolicy is restore-only: it admits a gamelist for a system folder
// that does not exist on the destination yet.
func gamelistPolicy(root string) platforms.BackupDefinition {
	return definition(root, "roms", categorySettings, globs("*/gamelist.xml"), nil)
}

// romPrefixPolicy is restore-only: it admits the files romPrefixDefinitions
// collects from any prefix name without walking roms/windows.
func romPrefixPolicy(root string) platforms.BackupDefinition {
	users := filepath.Join("*.wine", "drive_c", "users")
	return definition(root, filepath.Join("roms", "windows"), categorySaves,
		winebackup.PrefixPatterns(users, winebackup.UserSavePatterns),
		winebackup.PrefixPatterns(users, winebackup.SaveExclusions))
}

var networkFilesystems = map[string]struct{}{
	"9p": {}, "ceph": {}, "cifs": {}, "fuse.sshfs": {}, "glusterfs": {},
	"nfs": {}, "nfs4": {}, "smb3": {}, "smbfs": {},
}

// networkMountWarnings reports Batocera's network storage options
// (sharenetwork) when they put /userdata, its saves or its system folder on
// a share. The backup still runs; the warning explains why another device
// sharing the same storage can change what it holds.
func networkMountWarnings(root, mountsPath string) []platforms.BackupWarning {
	file, err := os.Open(mountsPath) //nolint:gosec // Fixed kernel mount table path, injectable for tests.
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	saves := filepath.Join(root, "saves")
	system := filepath.Join(root, "system")
	var warnings []platforms.BackupWarning
	scanner := bufio.NewScanner(io.LimitReader(file, maxMountsSize))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if _, ok := networkFilesystems[fields[2]]; !ok {
			continue
		}
		mountPoint := filepath.Clean(unescapeMountField(fields[1]))
		category := ""
		switch {
		case mountPoint == root, within(mountPoint, saves):
			category = categorySaves
		case within(mountPoint, system):
			category = categorySettings
		default:
			continue
		}
		warnings = append(warnings, platforms.BackupWarning{
			Category: category,
			Path:     mountPoint,
			Reason:   "stored on a network share (" + fields[2] + "); other devices using it can change these files",
		})
	}
	return warnings
}

func within(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+string(filepath.Separator))
}

// unescapeMountField decodes the octal escapes /proc/mounts uses for spaces,
// tabs, newlines and backslashes in mount points.
func unescapeMountField(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var builder strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+3 < len(field) {
			if value, err := strconv.ParseUint(field[i+1:i+4], 8, 8); err == nil {
				builder.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		builder.WriteByte(field[i])
	}
	return builder.String()
}
