//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupDefinitionsCoverDurableDataWithoutBroadGameRoots(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	definitions := BackupDefinitions(home)

	assertDefinition := func(sourceRoot, restoreRoot, category string) platforms.BackupDefinition {
		t.Helper()
		for _, definition := range definitions {
			if definition.SourceRoot == sourceRoot && definition.RestoreRoot == restoreRoot &&
				definition.Category == category {
				return definition
			}
		}
		t.Fatalf("missing definition: %s -> %s (%s)", sourceRoot, restoreRoot, category)
		return platforms.BackupDefinition{}
	}

	assertDefinition(
		filepath.Join(home, "Emulation", "saves"), filepath.Join("Emulation", "saves"), backupCategorySaves,
	)
	assertDefinition(
		filepath.Join(home, "Emulation", "saves"), filepath.Join("Emulation", "saves"),
		backupCategorySavestates,
	)
	assertDefinition(
		filepath.Join(home, "Emulation", "bios"), filepath.Join("Emulation", "bios"), backupCategorySettings,
	)
	assertDefinition(
		filepath.Join(home, "retrodeck", "saves"), filepath.Join("retrodeck", "saves"), backupCategorySaves,
	)
	assertDefinition(
		filepath.Join(home, "ES-DE"), "ES-DE", backupCategorySettings,
	)
	assertDefinition(
		filepath.Join(home, ".config", "EmuDeck"), filepath.Join(".config", "EmuDeck"),
		backupCategorySettings,
	)
	for _, expected := range []struct {
		rel      string
		category string
	}{
		{filepath.Join(".config", "steam-rom-manager", "userData"), backupCategorySettings},
		{filepath.Join(".var", "app", "app.xemu.xemu", "data", "xemu", "xemu"), backupCategorySettings},
		{
			filepath.Join(".var", "app", "org.DolphinEmu.dolphin-emu", "data", "dolphin-emu", "Config"),
			backupCategorySettings,
		},
		{filepath.Join(".var", "app", "com.github.Rosalie241.RMG", "data", "RMG", "Save"), backupCategorySaves},
		{filepath.Join(".var", "app", "rs.ruffle.Ruffle", "data", "ruffle", "SharedObjects"), backupCategorySaves},
		{filepath.Join(".var", "app", "org.mamedev.MAME", ".mame", "nvram"), backupCategorySaves},
		{filepath.Join(".var", "app", "com.supermodel3.Supermodel", ".supermodel", "NVRAM"), backupCategorySaves},
		{filepath.Join(".kodi", "userdata"), backupCategorySettings},
		{filepath.Join(".config", "Moonlight Game Streaming Project"), backupCategorySettings},
	} {
		assertDefinition(filepath.Join(home, expected.rel), expected.rel, expected.category)
	}

	assertDefinition(
		filepath.Join(home, "Emulation", "roms", "wiiu", "mlc01", "usr", "save"),
		filepath.Join("Emulation", "roms", "wiiu", "mlc01", "usr", "save"),
		backupCategorySaves,
	)
	assertDefinition(
		filepath.Join(home, "Emulation", "roms", "xbox360", "content"),
		filepath.Join("Emulation", "roms", "xbox360", "content"),
		backupCategorySaves,
	)

	for _, definition := range definitions {
		assert.NotEqual(t, filepath.Join(home, ".local", "share", "Steam", "steamapps", "common"),
			definition.SourceRoot)
		assert.NotEqual(t, filepath.Join(home, ".local", "share", "Steam", "steamapps", "shadercache"),
			definition.SourceRoot)
		assert.NotEqual(t, filepath.Join(home, "Emulation", "roms"), definition.SourceRoot)
		assert.NotEqual(t, filepath.Join(home, "Emulation", "storage"), definition.SourceRoot)
		assert.NotEqual(t, filepath.Join(home, "retrodeck", "roms"), definition.SourceRoot)
		assert.NotEqual(t, filepath.Join(home, ".local", "share", "flatpak"), definition.SourceRoot)
	}
}

func TestBackupPlanSelectsOnlyNonSteamCompatdata(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	steamRoot := filepath.Join(home, ".local", "share", "Steam")
	userConfig := filepath.Join(steamRoot, "userdata", "123", "config")
	require.NoError(t, os.MkdirAll(userConfig, 0o750))
	const shortcutAppID = uint32(4_000_000_001)
	shortcuts := fixtures.BuildShortcutsVDF([]fixtures.TestShortcut{{
		AppID: shortcutAppID, AppName: "Non-Steam Game", Exe: "game.exe", StartDir: ".", Optional: true,
	}})
	require.NoError(t, os.WriteFile(filepath.Join(userConfig, "shortcuts.vdf"), shortcuts, 0o600))

	nonSteamPrefix := filepath.Join(
		steamRoot, "steamapps", "compatdata", strconv.FormatUint(uint64(shortcutAppID), 10), "pfx",
	)
	steamGamePrefix := filepath.Join(steamRoot, "steamapps", "compatdata", "123456", "pfx")
	require.NoError(t, os.MkdirAll(filepath.Join(nonSteamPrefix, "drive_c", "users"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(steamGamePrefix, "drive_c", "users"), 0o750))

	definitions, warnings := discoverNonSteamDefinitions(home)
	require.Empty(t, warnings)

	sources := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		sources = append(sources, definition.SourceRoot)
	}
	assert.Contains(t, sources, userConfig)
	assert.Contains(t, sources, nonSteamPrefix)
	assert.Contains(t, sources, filepath.Join(nonSteamPrefix, "drive_c", "users"))
	assert.NotContains(t, sources, steamGamePrefix)
	assert.NotContains(t, sources, filepath.Join(steamGamePrefix, "drive_c", "users"))
	assert.NotContains(t, sources, filepath.Join(steamRoot, "steamapps", "compatdata"),
		"collection plan must never walk every Steam prefix")
}

func TestBackupPlanDiscoversFlatpakSteamAndExternalLibraryPrefix(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	steamRoot := filepath.Join(
		home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam",
	)
	userConfig := filepath.Join(steamRoot, "userdata", "123", "config")
	require.NoError(t, os.MkdirAll(userConfig, 0o750))
	const shortcutAppID = uint32(4_000_000_001)
	shortcuts := fixtures.BuildShortcutsVDF([]fixtures.TestShortcut{{
		AppID: shortcutAppID, AppName: "External Game", Exe: "game.exe", StartDir: ".", Optional: true,
	}})
	require.NoError(t, os.WriteFile(filepath.Join(userConfig, "shortcuts.vdf"), shortcuts, 0o600))

	externalLibrary := filepath.Join(home, "external-library")
	steamApps := filepath.Join(steamRoot, "steamapps")
	require.NoError(t, os.MkdirAll(steamApps, 0o750))
	libraryVDF := `"libraryfolders" { "1" { "path" "` + externalLibrary + `" } }`
	require.NoError(t, os.WriteFile(filepath.Join(steamApps, "libraryfolders.vdf"), []byte(libraryVDF), 0o600))
	externalPrefix := filepath.Join(
		externalLibrary, "steamapps", "compatdata", strconv.FormatUint(uint64(shortcutAppID), 10), "pfx",
	)
	require.NoError(t, os.MkdirAll(filepath.Join(externalPrefix, "drive_c", "users"), 0o750))

	definitions, warnings := discoverSteamShortcutDefinitions(home)
	require.Empty(t, warnings)
	sources := make([]string, 0, len(definitions))
	for _, def := range definitions {
		sources = append(sources, def.SourceRoot)
	}
	assert.Contains(t, sources, userConfig)
	assert.Contains(t, sources, externalPrefix)
	assert.Contains(t, sources, filepath.Join(externalPrefix, "drive_c", "users"))
}

func TestBackupUsesConfiguredSteamRoot(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	configuredRoot := filepath.Join(home, "custom-steam")
	configRoot := filepath.Join(configuredRoot, "userdata", "123", "config")
	require.NoError(t, os.MkdirAll(configRoot, 0o750))
	shortcuts := fixtures.BuildShortcutsVDF([]fixtures.TestShortcut{{
		AppID: 4_000_000_001, AppName: "Configured Game", Exe: "game.exe", StartDir: ".", Optional: true,
	}})
	require.NoError(t, os.WriteFile(filepath.Join(configRoot, "shortcuts.vdf"), shortcuts, 0o600))

	definitions, warnings := discoverSteamShortcutDefinitionsAt(home, configuredRoot)
	require.Empty(t, warnings)
	require.NotEmpty(t, definitions)
	assert.Equal(t, configRoot, definitions[0].SourceRoot)
	assert.Equal(t, configuredRoot, selectedSteamInstallRoot(home, configuredRoot))
}

func TestSteamInstallRootSupportsSnap(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	snapRoot := filepath.Join(home, "snap", "steam", "common", ".steam", "steam")
	require.NoError(t, os.MkdirAll(snapRoot, 0o750))
	assert.Equal(t, snapRoot, steamInstallRoot(home))
}

func TestBackupPlanReportsMalformedSteamShortcuts(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	configRoot := filepath.Join(home, ".local", "share", "Steam", "userdata", "123", "config")
	require.NoError(t, os.MkdirAll(configRoot, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(configRoot, "shortcuts.vdf"), []byte("invalid"), 0o600))
	highBitPrefix := filepath.Join(
		home, ".local", "share", "Steam", "steamapps", "compatdata", "4000000001", "pfx",
	)
	require.NoError(t, os.MkdirAll(filepath.Join(highBitPrefix, "drive_c", "users"), 0o750))

	definitions, warnings := discoverSteamShortcutDefinitions(home)

	require.Len(t, definitions, 3)
	sources := make([]string, 0, len(definitions))
	for _, def := range definitions {
		sources = append(sources, def.SourceRoot)
	}
	assert.Contains(t, sources, configRoot)
	assert.Contains(t, sources, highBitPrefix)
	assert.Contains(t, sources, filepath.Join(highBitPrefix, "drive_c", "users"))
	require.Len(t, warnings, 1)
	assert.Equal(t, backupCategorySettings, warnings[0].Category)
	assert.Equal(t, "Steam shortcuts could not be parsed; raw metadata was preserved", warnings[0].Reason)
}

func TestBackupDefinitionsRejectDangerousCategorySymlink(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	emulation := filepath.Join(home, "Emulation")
	require.NoError(t, os.MkdirAll(emulation, 0o750))
	saves := filepath.Join(emulation, "saves")
	require.NoError(t, os.Symlink(home, saves))

	definitions := BackupDefinitions(home)
	for _, def := range definitions {
		if def.RestoreRoot != filepath.Join("Emulation", "saves") || def.Category != backupCategorySaves {
			continue
		}
		assert.NotEqual(t, saves, def.RestoreTargetRoot)
		assert.Contains(t, def.RestoreTargetRoot, ".zaparoo-rejected-root")
		return
	}
	t.Fatal("missing EmuDeck saves definition")
}

func TestSafeDefinitionRootRejectsExistingPathThroughRootSymlink(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	rootLink := filepath.Join(home, "root-link")
	require.NoError(t, os.Symlink(string(filepath.Separator), rootLink))

	assert.False(t, safeDefinitionRoot(filepath.Join(rootLink, "tmp")))
}

func TestSafeDefinitionRootAllowsExternalCategorySymlink(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(home, "external-saves")
	require.NoError(t, os.Symlink(target, link))

	assert.True(t, safeDefinitionRoot(link))
}

func TestBackupDefinitionsUseIndependentProviderTargets(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	emuSettings := filepath.Join(home, ".config", "EmuDeck")
	require.NoError(t, os.MkdirAll(emuSettings, 0o750))
	emuSaves := filepath.Join(home, "custom", "emu-saves")
	emuBios := filepath.Join(home, "custom", "emu-bios")
	emuStorage := filepath.Join(home, "custom", "emu-storage")
	emuRoms := filepath.Join(home, "custom", "emu-roms")
	require.NoError(t, os.WriteFile(filepath.Join(emuSettings, "settings.sh"), []byte(
		"romsPath=\""+emuRoms+"\"\n"+
			"savesPath=\""+emuSaves+"\"\n"+
			"biosPath=\""+emuBios+"\"\n"+
			"storagePath=\""+emuStorage+"\"\n",
	), 0o600))

	retroConfig := filepath.Join(home, ".var", "app", "net.retrodeck.retrodeck", "config", "retrodeck")
	require.NoError(t, os.MkdirAll(retroConfig, 0o750))
	retroSaves := filepath.Join(home, "custom", "retro-saves")
	retroStates := filepath.Join(home, "custom", "retro-states")
	retroBios := filepath.Join(home, "custom", "retro-bios")
	content, err := json.Marshal(map[string]any{"paths": map[string]string{
		"saves_path": retroSaves, "states_path": retroStates, "bios_path": retroBios,
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(retroConfig, "retrodeck.json"), content, 0o600))

	definitions := BackupDefinitions(home)
	assertTarget := func(restoreRoot, category, target string) {
		t.Helper()
		for _, def := range definitions {
			if def.RestoreRoot == restoreRoot && def.Category == category && def.RestoreTargetRoot == target {
				return
			}
		}
		t.Fatalf("missing target %s -> %s (%s)", restoreRoot, target, category)
	}
	assertTarget(filepath.Join("Emulation", "saves"), backupCategorySaves, emuSaves)
	assertTarget(filepath.Join("Emulation", "bios"), backupCategorySettings, emuBios)
	assertTarget(filepath.Join("retrodeck", "saves"), backupCategorySaves, retroSaves)
	assertTarget(filepath.Join("retrodeck", "states"), backupCategorySavestates, retroStates)
	assertTarget(filepath.Join("retrodeck", "bios"), backupCategorySettings, retroBios)
}

func TestPrepareBackupRestorePreservesDestinationProviderPaths(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	emuSettings := filepath.Join(home, ".config", "EmuDeck", "settings.sh")
	destinationEmulation := filepath.Join(home, "destination", "Emulation")
	writeBackupTestFile(t, emuSettings,
		"emulationPath=\""+destinationEmulation+"\"\n"+
			"romsPath=\""+filepath.Join(destinationEmulation, "roms")+"\"\n"+
			"savesPath=\""+filepath.Join(destinationEmulation, "saves")+"\"\n"+
			"biosPath=\""+filepath.Join(destinationEmulation, "bios")+"\"\n"+
			"storagePath=\""+filepath.Join(destinationEmulation, "storage")+"\"\n")

	retroConfig := filepath.Join(
		home, ".var", "app", "net.retrodeck.retrodeck", "config", "retrodeck", "retrodeck.json",
	)
	destinationRetro := filepath.Join(home, "destination", "retrodeck")
	writeBackupTestFile(t, retroConfig, `{"paths":{"rd_home_path":"`+destinationRetro+`",`+
		`"roms_path":"`+filepath.Join(destinationRetro, "roms")+`",`+
		`"saves_path":"`+filepath.Join(destinationRetro, "saves")+`",`+
		`"states_path":"`+filepath.Join(destinationRetro, "states")+`",`+
		`"bios_path":"`+filepath.Join(destinationRetro, "bios")+`",`+
		`"storage_path":"`+filepath.Join(destinationRetro, "storage")+`"}}`)

	destinationEmuDeckPaths := linuxemu.DefaultEmuDeckPaths(home)
	destinationRetroPaths := destinationRetroDECKPaths(home)
	writeBackupTestFile(t, emuSettings, "emulationPath=\"/old/Emulation\"\nromsPath=\"/old/roms\"\n")
	writeBackupTestFile(t, retroConfig, `{"paths":{"rd_home_path":"/old/retrodeck",`+
		`"roms_path":"/old/roms","saves_path":"/old/saves","states_path":"/old/states",`+
		`"bios_path":"/old/bios","storage_path":"/old/storage"}}`)
	require.NoError(t, rewriteEmuDeckPaths(home, &destinationEmuDeckPaths))
	require.NoError(t, rewriteRetroDECKPaths(home, destinationRetroPaths))

	emuPaths := linuxemu.DefaultEmuDeckPaths(home)
	assert.Equal(t, destinationEmulation, emuPaths.EmulationPath)
	assert.Equal(t, filepath.Join(destinationEmulation, "roms"), emuPaths.RomsPath)
	assert.Equal(t, filepath.Join(destinationEmulation, "saves"), emuPaths.SavesPath)
	assert.Equal(t, filepath.Join(destinationEmulation, "bios"), emuPaths.BiosPath)
	assert.Equal(t, filepath.Join(destinationEmulation, "storage"), emuPaths.StoragePath)
	retroPaths := linuxemu.DefaultRetroDECKPaths(home)
	assert.Equal(t, destinationRetro, retroPaths.HomePath)
	assert.Equal(t, filepath.Join(destinationRetro, "roms"), retroPaths.RomsPath)
	assert.Equal(t, filepath.Join(destinationRetro, "saves"), retroPaths.SavesPath)
	assert.Equal(t, filepath.Join(destinationRetro, "states"), retroPaths.StatesPath)
	assert.Equal(t, filepath.Join(destinationRetro, "bios"), retroPaths.BiosPath)
	assert.Equal(t, filepath.Join(destinationRetro, "storage"), retroPaths.StoragePath)
}

func writeBackupTestFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func TestDiscoverFaugusCustomPrefix(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	customPrefix := filepath.Join(home, "custom-prefix")
	require.NoError(t, os.MkdirAll(filepath.Join(customPrefix, "drive_c", "users"), 0o750))
	libraryPath := filepath.Join(home, ".local", "share", "faugus-launcher", "games.json")
	writeBackupTestFile(t, libraryPath, `[{"gameid":"game-1","prefix":"`+customPrefix+`"}]`)

	definitions, warnings := discoverFaugusDefinitions(home)
	require.Empty(t, warnings)
	sources := make([]string, 0, len(definitions))
	for _, def := range definitions {
		sources = append(sources, def.SourceRoot)
	}
	assert.Contains(t, sources, customPrefix)
	assert.Contains(t, sources, filepath.Join(customPrefix, "drive_c", "users"))
}

func TestDiscoverBottlesReportsUnreadableRoot(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "bottles", "bottles")
	writeBackupTestFile(t, root, "not a directory")

	definitions, warnings := discoverBottlesDefinitions(home)
	assert.Empty(t, definitions)
	require.Len(t, warnings, 1)
	assert.Equal(t, "Bottles data is unreadable", warnings[0].Reason)
}

func TestDiscoverBottlesAndFaugusDefinitionsAvoidInstalledGames(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bottleRoot := filepath.Join(
		home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "My Bottle",
	)
	faugusRoot := filepath.Join(home, "Faugus", "My Game", "pfx")
	for _, root := range []string{bottleRoot, faugusRoot} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "drive_c", "users", "steamuser", "Documents"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "drive_c", "Program Files", "Game"), 0o750))
	}

	bottles, bottleWarnings := discoverBottlesDefinitions(home)
	faugus, faugusWarnings := discoverFaugusDefinitions(home)
	require.Empty(t, bottleWarnings)
	require.Empty(t, faugusWarnings)
	definitions := bottles
	definitions = append(definitions, faugus...)
	sources := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		sources = append(sources, definition.SourceRoot)
	}

	assert.Contains(t, sources, filepath.Join(bottleRoot, "drive_c", "users"))
	assert.Contains(t, sources, filepath.Join(faugusRoot, "drive_c", "users"))
	for _, source := range sources {
		assert.NotContains(t, source, filepath.Join("drive_c", "Program Files"))
	}
}
