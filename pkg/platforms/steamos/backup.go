//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
)

const (
	backupCategorySettings   = "settings"
	backupCategorySaves      = "saves"
	backupCategorySavestates = "savestates"
)

var (
	savestatePatterns = []platforms.BackupPattern{
		{Contains: "states/"},
		{Contains: "statesaves/"},
		{Contains: "savestates/"},
	}
	windowsUserSavePatterns = []platforms.BackupPattern{
		{Glob: "*/Documents/**"},
		{Glob: "*/Saved Games/**"},
		{Glob: "*/AppData/Roaming/**"},
		{Glob: "*/AppData/LocalLow/**"},
		{Glob: "*/AppData/Local/**"},
	}
	windowsSaveExclusions = []platforms.BackupPattern{
		{Glob: "*/Application Data/**"},
		{Glob: "*/Cookies/**"},
		{Glob: "*/Local Settings/**"},
		{Glob: "*/My Documents/**"},
		{Glob: "*/NetHood/**"},
		{Glob: "*/PrintHood/**"},
		{Glob: "*/Recent/**"},
		{Glob: "*/SendTo/**"},
		{Glob: "*/Start Menu/**"},
		{Glob: "*/Templates/**"},
		{Glob: "*/AppData/Local/Battle.net/**"},
		{Glob: "*/AppData/Local/Temp/**"},
		{Glob: "*/AppData/Local/D3DSCache/**"},
		{Glob: "*/AppData/Local/NVIDIA/**"},
		{Glob: "*/AppData/Local/AMD/**"},
		{Glob: "*.dxvk.bin"},
		{Glob: "*.dxvk.lut"},
		{Glob: "CachedData.db"},
		{Contains: "internet cache/"},
		{Contains: "cache/"},
		{Contains: "caches/"},
		{Contains: "crashdumps/"},
		{Contains: "crashes/"},
		{Contains: "logs/"},
		{Contains: "telemetry/"},
	}
	emuDeckConfigIncludes = []platforms.BackupPattern{
		{Glob: "settings.*"},
		{Glob: ".RPCS3MigrationCompleted"},
		{Glob: ".cloned"},
		{Glob: ".dolphinlegacysymlinks"},
		{Glob: ".dolphinsymlinks"},
		{Glob: ".finished"},
		{Glob: ".rat"},
		{Glob: ".rau"},
		{Glob: ".ui-finished"},
		{Glob: ".updaterId"},
		{Glob: "android/**"},
		{Glob: "custom_scripts/**"},
		{Glob: "feeds/**"},
		{Glob: "store/**"},
	}
	emuDeckConfigExclusions = []platforms.BackupPattern{
		{Glob: "backend/**"},
		{Glob: "blob_storage/**"},
		{Glob: "Cache/**"},
		{Glob: "Code Cache/**"},
		{Glob: "Crashpad/**"},
		{Glob: "databases/**"},
		{Glob: "DawnCache/**"},
		{Glob: "Dictionaries/**"},
		{Glob: "GPUCache/**"},
		{Glob: "IndexedDB/**"},
		{Glob: "Local Storage/**"},
		{Glob: "logs/**"},
		{Glob: "python_virtual_env/**"},
		{Glob: "Service Worker/**"},
		{Glob: "Session Storage/**"},
		{Glob: "shared_proto_db/**"},
		{Glob: "VideoDecodeStats/**"},
		{Glob: "WebStorage/**"},
	}
	providerBIOSExclusions = []platforms.BackupPattern{
		{Glob: "RetroArch_v*.zip"},
		{Glob: "RetroArch_v*/**"},
		{Glob: "HdPacks/**"},
		{Glob: "Mupen64plus/cache/**"},
		{Glob: "Mupen64plus/hires_texture/**"},
		{Glob: "PPSSPP/**"},
		{Glob: "Vita3K/ux0/user/**"},
		{Glob: "azahar/keys/**"},
		{Glob: "cemu/usr/save/**"},
		{Glob: "dolphin-emu/Sys/**"},
		{Glob: "fbneo/patched/**"},
		{Glob: "pico-8/carts/**"},
		{Glob: "pico-8/cdata/**"},
		{Glob: "rpcs3/dev_hdd0/home/**"},
		{Glob: "ryujinx/keys/**"},
		{Glob: "scummvm/extra/**"},
		{Glob: "shadps4/sys_modules/**"},
		{Glob: "ume/**"},
		{Contains: "themes/"},
	}
	emuDeckManagedSaveExclusions = []platforms.BackupPattern{
		{Glob: "Cemu/saves/**"},
		{Glob: "Vita3K/saves/**"},
		{Glob: "azahar/saves/**"},
		{Glob: "azahar/states/**"},
		{Glob: "dolphin/GC/**"},
		{Glob: "dolphin/StateSaves/**"},
		{Glob: "dolphin/Wii/**"},
		{Glob: "es-de/gamelists/**"},
		{Glob: "ppsspp/saves/**"},
		{Glob: "ppsspp/states/**"},
		{Glob: "primehack/GC/**"},
		{Glob: "primehack/StateSaves/**"},
		{Glob: "primehack/Wii/**"},
		{Glob: "retroarch/saves/**"},
		{Glob: "retroarch/states/**"},
		{Glob: "rpcs3/saves/**"},
		{Glob: "rpcs3/trophy/**"},
		{Glob: "ryujinx/saveMeta/**"},
		{Glob: "ryujinx/saves/**"},
		{Glob: "ryujinx/system/**"},
		{Glob: "ryujinx/system_saves/**"},
		{Glob: "shadps4/saves/**"},
		{Glob: "xenia/saves/**"},
	}
	emulatorConfigExclusions = []platforms.BackupPattern{
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
		{Contains: "sharedobjects/"},
		{Contains: "ppsspp_state/"},
	}
)

// BackupRestoreRoot is the fallback for legacy definitions. SteamOS definitions
// use explicit restore targets so custom provider roots and category symlinks
// restore to the destination installation's active paths.
func (*Platform) BackupRestoreRoot() string {
	return steamOSHomeDir()
}

// BackupDefinitions returns the restore allow-list. Dynamic provider mappings
// are listed before narrow compatibility fallbacks so configured external roots
// win when they are available on the destination.
func (p *Platform) BackupDefinitions() []platforms.BackupDefinition {
	home := steamOSHomeDir()
	return steamOSBackupDefinitions(home, p.configuredSteamBackupRoot())
}

// BackupPlan walks only durable roots and discovered non-Steam prefixes. Broad
// compatibility definitions are restore-only and never traversed for backup.
func (p *Platform) BackupPlan() platforms.BackupPlan {
	home := steamOSHomeDir()
	if !validBackupHome(home) {
		return platforms.BackupPlan{Warnings: []platforms.BackupWarning{unresolvedHomeWarning()}}
	}
	definitions := durableDefinitions(home)
	prefixDefinitions, warnings := discoverNonSteamDefinitionsAt(home, p.configuredSteamBackupRoot())
	definitions = append(definitions, prefixDefinitions...)
	return platforms.BackupPlan{Definitions: definitions, Warnings: warnings}
}

// validBackupHome rejects an unresolved home directory. Definitions built from
// an empty home are relative, and the collector would walk the working
// directory as a trusted source root.
func validBackupHome(home string) bool {
	return home != "" && filepath.IsAbs(home)
}

func unresolvedHomeWarning() platforms.BackupWarning {
	return platforms.BackupWarning{
		Category: backupCategorySettings,
		Path:     "",
		Reason:   "home directory could not be resolved; nothing was collected",
	}
}

func (p *Platform) configuredSteamBackupRoot() string {
	if p == nil {
		return ""
	}
	root := p.backupSteamRoot.Load()
	if root == nil {
		return ""
	}
	return *root
}

func steamOSHomeDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

// BackupDefinitions returns SteamOS restore policy rooted at home.
func BackupDefinitions(home string) []platforms.BackupDefinition {
	return steamOSBackupDefinitions(home, "")
}

func steamOSBackupDefinitions(home, configuredSteamRoot string) []platforms.BackupDefinition {
	if !validBackupHome(home) {
		return nil
	}
	definitions := durableDefinitions(home)
	discovered, _ := discoverNonSteamDefinitionsAt(home, configuredSteamRoot)
	definitions = append(definitions, discovered...)
	definitions = append(definitions, broadNonSteamPoliciesAt(home, configuredSteamRoot)...)
	return definitions
}

func durableDefinitions(home string) []platforms.BackupDefinition {
	if !validBackupHome(home) {
		return nil
	}
	definitions := make([]platforms.BackupDefinition, 0, 96)
	definitions = append(definitions,
		definition(
			filepath.Join(home, ".config", "EmuDeck"), filepath.Join(".config", "EmuDeck"),
			backupCategorySettings, emuDeckConfigIncludes, emuDeckConfigExclusions,
		),
		esdeDefinition(home),
		steamROMManagerDefinition(home),
	)
	emuDeckPaths := linuxemu.DefaultEmuDeckPaths(home)
	retroDECKPaths := linuxemu.DefaultRetroDECKPaths(home)
	definitions = append(definitions, emuDeckDefinitions(&emuDeckPaths)...)
	definitions = append(definitions, retroDECKDefinitions(home, &retroDECKPaths)...)
	definitions = append(definitions, emulatorDefinitions(home)...)
	definitions = append(definitions, launcherDefinitions(home)...)
	definitions = append(definitions, faugusMetadataDefinitions(home)...)
	return absoluteDefinitions(definitions)
}

// absoluteDefinitions drops any definition whose source root is relative. A
// provider path that failed discovery would otherwise resolve against the
// working directory.
func absoluteDefinitions(definitions []platforms.BackupDefinition) []platforms.BackupDefinition {
	filtered := definitions[:0]
	for i := range definitions {
		if filepath.IsAbs(definitions[i].SourceRoot) {
			filtered = append(filtered, definitions[i])
		}
	}
	return filtered
}

func definition(
	sourceRoot, restoreRoot, category string,
	include, exclude []platforms.BackupPattern,
) platforms.BackupDefinition {
	targetRoot := sourceRoot
	trustedRoot := sourceRoot
	if !safeDefinitionRoot(sourceRoot) {
		targetRoot = filepath.Join(sourceRoot, ".zaparoo-rejected-root")
		trustedRoot = targetRoot
	}
	return platforms.BackupDefinition{
		SourceRoot: sourceRoot, RestoreRoot: restoreRoot, RestoreTargetRoot: targetRoot,
		Category: category, Include: include, Exclude: exclude,
		SourceTrustedRoots: []string{trustedRoot},
	}
}

func safeDefinitionRoot(candidate string) bool {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	absolute = filepath.Clean(absolute)
	resolved, existingRoot, ok := resolvedBackupPathFromExistingAncestor(absolute)
	if !ok || existingRoot == string(filepath.Separator) || resolved == string(filepath.Separator) {
		return false
	}
	rel, err := filepath.Rel(resolved, absolute)
	return err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvedBackupPathFromExistingAncestor(candidate string) (resolved, existingRoot string, ok bool) {
	if hasAncestorResolvingSymlink(candidate) {
		return "", "", false
	}
	current := filepath.Clean(candidate)
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			root, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", "", false
			}
			root = filepath.Clean(root)
			return filepath.Clean(filepath.Join(append([]string{root}, suffix...)...)), root, true
		}
		if !os.IsNotExist(err) {
			return "", "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", false
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func appendBackupPatterns(
	base []platforms.BackupPattern, extra ...platforms.BackupPattern,
) []platforms.BackupPattern {
	result := make([]platforms.BackupPattern, 0, len(base)+len(extra))
	result = append(result, base...)
	return append(result, extra...)
}

func hasAncestorResolvingSymlink(candidate string) bool {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return true
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return false
		}
		if statErr != nil {
			return true
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr != nil {
			return true
		}
		rel, relErr := filepath.Rel(filepath.Clean(resolved), current)
		if relErr != nil || rel == "." || rel == ".." ||
			(!filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func nonRecursiveDefinition(
	sourceRoot, restoreRoot, category string,
	include []platforms.BackupPattern,
) platforms.BackupDefinition {
	result := definition(sourceRoot, restoreRoot, category, include, nil)
	result.NonRecursive = true
	return result
}

func esdeDefinition(home string) platforms.BackupDefinition {
	return definition(
		filepath.Join(home, "ES-DE"), "ES-DE", backupCategorySettings,
		[]platforms.BackupPattern{
			{Glob: "es_settings.xml"},
			{Contains: "collections/"},
			{Contains: "custom_systems/"},
			{Contains: "gamelists/"},
			{Contains: "scripts/"},
		},
		[]platforms.BackupPattern{
			{Glob: "es_log.txt"},
			{Contains: "downloaded_media/"},
			{Contains: "themes/"},
		},
	)
}

func steamROMManagerDefinition(home string) platforms.BackupDefinition {
	rel := filepath.Join(".config", "steam-rom-manager", "userData")
	return definition(
		filepath.Join(home, rel), rel, backupCategorySettings,
		[]platforms.BackupPattern{{All: true}},
		[]platforms.BackupPattern{
			{Glob: "*cache.json"},
			{Contains: "cache/"},
			{Contains: "logs/"},
			{Contains: "downloaded_media/"},
		},
	)
}

func emuDeckDefinitions(paths *linuxemu.EmuDeckPaths) []platforms.BackupDefinition {
	xemuDefinition := nonRecursiveDefinition(
		filepath.Join(paths.StoragePath, "xemu"), filepath.Join("Emulation", "storage", "xemu"),
		backupCategorySaves,
		[]platforms.BackupPattern{{Glob: "xbox_hdd.qcow2"}, {Glob: "eeprom.bin"}},
	)
	return []platforms.BackupDefinition{
		definition(paths.SavesPath, filepath.Join("Emulation", "saves"), backupCategorySaves,
			[]platforms.BackupPattern{{All: true}},
			appendBackupPatterns(savestatePatterns, emuDeckManagedSaveExclusions...)),
		definition(paths.SavesPath, filepath.Join("Emulation", "saves"), backupCategorySavestates,
			savestatePatterns, emuDeckManagedSaveExclusions),
		definition(paths.BiosPath, filepath.Join("Emulation", "bios"), backupCategorySettings,
			[]platforms.BackupPattern{{All: true}}, providerBIOSExclusions),
		definition(
			filepath.Join(paths.StoragePath, "rpcs3", "dev_hdd0", "home"),
			filepath.Join("Emulation", "storage", "rpcs3", "dev_hdd0", "home"),
			backupCategorySaves, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "rpcs3", "dev_flash"),
			filepath.Join("Emulation", "storage", "rpcs3", "dev_flash"),
			backupCategorySettings, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "Vita3K", "ux0", "user"),
			filepath.Join("Emulation", "storage", "Vita3K", "ux0", "user"),
			backupCategorySaves, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "Vita3K", "vs0"),
			filepath.Join("Emulation", "storage", "Vita3K", "vs0"),
			backupCategorySettings, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "yuzu", "nand", "user", "save"),
			filepath.Join("Emulation", "storage", "yuzu", "nand", "user", "save"),
			backupCategorySaves, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "ryujinx", "games"),
			filepath.Join("Emulation", "storage", "ryujinx", "games"),
			backupCategorySettings, []platforms.BackupPattern{{All: true}}, emulatorConfigExclusions,
		),
		xemuDefinition,
		definition(
			filepath.Join(paths.StoragePath, "citra", "sdmc"),
			filepath.Join("Emulation", "storage", "citra", "sdmc"),
			backupCategorySaves,
			[]platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}}, nil,
		),
		definition(
			filepath.Join(paths.StoragePath, "azahar", "sdmc"),
			filepath.Join("Emulation", "storage", "azahar", "sdmc"),
			backupCategorySaves,
			[]platforms.BackupPattern{{Contains: "data/"}, {Contains: "extdata/"}}, nil,
		),
		definition(
			filepath.Join(paths.RomsPath, "wiiu", "mlc01", "usr", "save"),
			filepath.Join("Emulation", "roms", "wiiu", "mlc01", "usr", "save"),
			backupCategorySaves, []platforms.BackupPattern{{All: true}}, nil,
		),
		definition(
			filepath.Join(paths.RomsPath, "xbox360", "content"),
			filepath.Join("Emulation", "roms", "xbox360", "content"),
			backupCategorySaves,
			[]platforms.BackupPattern{{Contains: "00000001/"}, {Contains: "fffe07d1/"}}, nil,
		),
	}
}

func retroDECKDefinitions(home string, paths *linuxemu.RetroDECKPaths) []platforms.BackupDefinition {
	esdeRoot := filepath.Dir(paths.GamelistPath)
	return []platforms.BackupDefinition{
		definition(paths.SavesPath, filepath.Join("retrodeck", "saves"), backupCategorySaves,
			[]platforms.BackupPattern{{All: true}}, nil),
		definition(paths.StatesPath, filepath.Join("retrodeck", "states"), backupCategorySavestates,
			[]platforms.BackupPattern{{All: true}}, nil),
		definition(paths.BiosPath, filepath.Join("retrodeck", "bios"), backupCategorySettings,
			[]platforms.BackupPattern{{All: true}}, providerBIOSExclusions),
		definition(esdeRoot, filepath.Join("retrodeck", "ES-DE"), backupCategorySettings,
			[]platforms.BackupPattern{
				{Glob: "es_settings.xml"},
				{Contains: "collections/"},
				{Contains: "custom_systems/"},
				{Contains: "gamelists/"},
				{Contains: "scripts/"},
			},
			[]platforms.BackupPattern{{Contains: "downloaded_media/"}, {Contains: "themes/"}},
		),
		definition(
			filepath.Join(home, ".var", "app", "net.retrodeck.retrodeck", "config"),
			filepath.Join(".var", "app", "net.retrodeck.retrodeck", "config"),
			backupCategorySettings, []platforms.BackupPattern{{All: true}},
			appendBackupPatterns(emulatorConfigExclusions,
				platforms.BackupPattern{Glob: "*cache.json"},
				platforms.BackupPattern{Glob: "Cemu/cemu/**"},
				platforms.BackupPattern{Glob: "ES-DE/collections/**"},
				platforms.BackupPattern{Glob: "ES-DE/custom_systems/**"},
				platforms.BackupPattern{Glob: "ES-DE/gamelists/**"},
				platforms.BackupPattern{Glob: "mame/hiscore/**"},
				platforms.BackupPattern{Glob: "melonDS/bios/**"},
				platforms.BackupPattern{Glob: "ppsspp/PSP/Cheats/**"},
				platforms.BackupPattern{Glob: "retroarch/system/**"},
				platforms.BackupPattern{Glob: "Ryujinx/games/**"},
				platforms.BackupPattern{Contains: "resources/"},
				platforms.BackupPattern{Contains: "retroarch/assets/"},
				platforms.BackupPattern{Contains: "retroarch/database/"},
				platforms.BackupPattern{Contains: "retroarch/filters/"},
				platforms.BackupPattern{Contains: "retroarch/info/"},
				platforms.BackupPattern{Contains: "retroarch/overlays/"},
			),
		),
	}
}
