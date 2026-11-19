//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findDefinition(
	t *testing.T, definitions []platforms.BackupDefinition, rel, category string,
) platforms.BackupDefinition {
	t.Helper()
	for i := range definitions {
		if definitions[i].RestoreRoot == rel && definitions[i].Category == category {
			return definitions[i]
		}
	}
	t.Fatalf("missing definition: %s (%s)", rel, category)
	return platforms.BackupDefinition{}
}

func TestPlatformBackupUsesUserdata(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	assert.Equal(t, "/userdata", p.BackupRestoreRoot())
	for _, definition := range p.BackupDefinitions() {
		assert.True(t, strings.HasPrefix(definition.SourceRoot, "/userdata/"), definition.SourceRoot)
	}
	for _, definition := range p.BackupPlan().Definitions {
		assert.True(t, strings.HasPrefix(definition.SourceRoot, "/userdata/"), definition.SourceRoot)
	}
}

func TestBackupDefinitionsCoverEveryCategory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	definitions := BackupDefinitions(root)

	for _, expected := range []struct {
		rel      string
		category string
	}{
		{"system", categorySettings},
		{filepath.Join("system", "configs"), categorySettings},
		{filepath.Join("system", "configs", "emulationstation"), categorySettings},
		{filepath.Join("system", "configs", "retroarch"), categorySettings},
		{filepath.Join("system", "configs", "emulationstation"), categoryInputs},
		{filepath.Join("system", "configs", "retroarch", "config", "remaps"), categoryInputs},
		{filepath.Join("system", "configs", "evmapy"), categoryInputs},
		{"saves", categorySaves},
		{"saves", categorySavestates},
		{filepath.Join("saves", "dolphin-emu"), categorySaves},
		{filepath.Join("saves", "duckstation", "memcards"), categorySaves},
		{filepath.Join("saves", "psvita", "ux0", "user"), categorySaves},
		{filepath.Join("saves", "wiiu", "usr", "save"), categorySaves},
		{filepath.Join("system", "configs", "ppsspp", "PSP", "SAVEDATA"), categorySaves},
		{filepath.Join("system", "configs", "rpcs3", "dev_hdd0", "home"), categorySaves},
		{filepath.Join("system", "wine-bottles"), categorySaves},
		{filepath.Join("roms", "windows"), categorySaves},
	} {
		definition := findDefinition(t, definitions, expected.rel, expected.category)
		assert.Equal(t, filepath.Join(root, expected.rel), definition.SourceRoot)
		assert.Equal(t, []string{definition.SourceRoot}, definition.SourceTrustedRoots)
	}

	for _, definition := range definitions {
		rel := definition.RestoreRoot
		assert.False(t, rel == "bios" || strings.HasPrefix(rel, "bios"+string(filepath.Separator)), rel)
		assert.NotContains(t, rel, "flatpak")
	}
}

func TestBackupPlanOmitsRestoreOnlyPolicies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, definition := range BackupPlan(root, filepath.Join(root, "missing-mounts")).Definitions {
		assert.NotEqual(t, "roms", definition.RestoreRoot, "the plan must not walk the ROM folders")
		assert.NotEqual(t, filepath.Join("roms", "windows"), definition.RestoreRoot,
			"the plan must not walk the whole Windows ROM folder")
	}
}

func TestBackupPlanReadsOnlyGamelistsFromSystemFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "roms", "snes"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "roms", "readme.txt"), nil, 0o600))

	plan := BackupPlan(root, filepath.Join(root, "missing-mounts"))
	snes := findDefinition(t, plan.Definitions, filepath.Join("roms", "snes"), categorySettings)
	assert.True(t, snes.NonRecursive)
	assert.Equal(t, []platforms.BackupPattern{{Glob: "gamelist.xml"}}, snes.Include)
	for _, definition := range plan.Definitions {
		assert.NotEqual(t, filepath.Join("roms", "readme.txt"), definition.RestoreRoot)
	}

	policy := findDefinition(t, BackupDefinitions(root), "roms", categorySettings)
	assert.Equal(t, []platforms.BackupPattern{{Glob: "*/gamelist.xml"}}, policy.Include)
}

func TestBackupPlanDiscoversWindowsROMPrefixes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	windows := filepath.Join(root, "roms", "windows")
	require.NoError(t, os.MkdirAll(filepath.Join(windows, "Game.WINE", "drive_c", "users"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(windows, "Other.pc"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(windows, "file.wine"), nil, 0o600))

	plan := BackupPlan(root, filepath.Join(root, "missing-mounts"))
	prefix := filepath.Join("roms", "windows", "Game.WINE")
	settings := findDefinition(t, plan.Definitions, prefix, categorySettings)
	assert.True(t, settings.NonRecursive)
	findDefinition(t, plan.Definitions, filepath.Join(prefix, "drive_c", "users"), categorySaves)
	for _, definition := range plan.Definitions {
		if definition.Category == categorySaves {
			assert.NotContains(t, definition.RestoreRoot, "Other.pc")
		}
		assert.NotContains(t, definition.RestoreRoot, "file.wine")
	}

	definitions := BackupDefinitions(root)
	findDefinition(t, definitions, filepath.Join(prefix, "drive_c", "users"), categorySaves)
	findDefinition(t, definitions, filepath.Join("roms", "windows"), categorySaves)
}

func TestBackupPlanWarnsAboutNetworkStorage(t *testing.T) {
	t.Parallel()
	root := "/userdata"
	mounts := filepath.Join(t.TempDir(), "mounts")
	require.NoError(t, os.WriteFile(mounts, []byte(strings.Join([]string{
		"/dev/mmcblk0p2 /userdata ext4 rw,relatime 0 0",
		"nas:/saves /userdata/saves nfs4 rw 0 0",
		"//nas/My\\040Configs /userdata/system/configs cifs rw 0 0",
		"//nas/roms /userdata/roms cifs rw 0 0",
		"nas:/other /media/share nfs rw 0 0",
		"short line",
	}, "\n")+"\n"), 0o600))

	warnings := BackupPlan(root, mounts).Warnings
	require.Len(t, warnings, 2)
	assert.Equal(t, categorySaves, warnings[0].Category)
	assert.Equal(t, "/userdata/saves", warnings[0].Path)
	assert.Contains(t, warnings[0].Reason, "nfs4")
	assert.Equal(t, categorySettings, warnings[1].Category)
	assert.Equal(t, "/userdata/system/configs", warnings[1].Path)
}

func TestBackupPlanWarnsWhenUserdataIsShared(t *testing.T) {
	t.Parallel()
	mounts := filepath.Join(t.TempDir(), "mounts")
	require.NoError(t, os.WriteFile(mounts, []byte("//nas/batocera /userdata smb3 rw 0 0\n"), 0o600))

	warnings := BackupPlan("/userdata", mounts).Warnings
	require.Len(t, warnings, 1)
	assert.Equal(t, categorySaves, warnings[0].Category)
	assert.Equal(t, "/userdata", warnings[0].Path)
}

func TestBackupPlanWithoutMountTable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	assert.Empty(t, BackupPlan(root, filepath.Join(root, "missing")).Warnings)
}

func TestUnescapeMountField(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/plain", unescapeMountField("/plain"))
	assert.Equal(t, "/a b\tc", unescapeMountField(`/a\040b\011c`))
	assert.Equal(t, `/back\slash`, unescapeMountField(`/back\134slash`))
	assert.Equal(t, `/bad\9xy`, unescapeMountField(`/bad\9xy`))
	assert.Equal(t, `/end\04`, unescapeMountField(`/end\04`))
}
