//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"encoding/xml"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testHome = frontendHome

func testSNES() *esSystem {
	return &esSystem{Name: "snes", Path: "/roms/snes/"}
}

func gamelistXML(games string) string {
	return `<?xml version="1.0"?>` + "\n<gameList>\n" + games + "\n</gameList>\n"
}

func TestLoadGameMeta_ROMFolderGamelist(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/roms/snes/gamelist.xml", gamelistXML(`
		<folder><path>./sub</path><name>Folder</name><core>wrong</core></folder>
		<game><path>./Other.sfc</path><name>Other</name><core>wrong</core></game>
		<game><path>./Game (USA).sfc</path><name>First</name><core>snes9x2005</core></game>
		<game><path>./Game (USA).sfc</path><name>Game Name</name><emulator>retroarch32</emulator>
			<core>snes9x2005</core><governor>powersave</governor><desc>R&D</desc></game>`))
	// The user gamelist is not read while the ROM folder has one.
	writeFile(t, fs, filepath.Join(testHome, ".emulationstation", "gamelists", "snes", "gamelist.xml"),
		gamelistXML(`<game><path>./Game (USA).sfc</path><name>User</name></game>`))

	meta := loadGameMeta(fs, testSNES(), testROM, testHome)
	assert.Equal(t, gameMeta{
		name: "Game Name", emulator: "retroarch32", core: "snes9x2005", governor: "powersave",
	}, meta, "the last matching entry wins")
}

func TestLoadGameMeta_GamelistFallbacks(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/etc/emulationstation/gamelists/snes/gamelist.xml",
		gamelistXML(`<game><path>/roms/snes/Game (USA).sfc</path><name>System</name></game>`))
	assert.Equal(t, "System", loadGameMeta(fs, testSNES(), testROM, testHome).name)

	writeFile(t, fs, filepath.Join(testHome, ".emulationstation", "gamelists", "snes", "gamelist.xml"),
		gamelistXML(`<game><path>./Game (USA).sfc</path><name>User</name></game>`))
	assert.Equal(t, "User", loadGameMeta(fs, testSNES(), testROM, testHome).name)
}

func TestLoadGameMeta_TildeStartPath(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, filepath.Join(testHome, "roms", "snes", "gamelist.xml"),
		gamelistXML(`<game><path>./a.sfc</path><core>snes9x2010</core></game>`))
	sys := &esSystem{Name: "snes", Path: "~/roms/snes"}
	rom := filepath.Join(testHome, "roms", "snes", "a.sfc")
	assert.Equal(t, "snes9x2010", loadGameMeta(fs, sys, rom, testHome).core)
}

func TestLoadGameMeta_Recovery(t *testing.T) {
	t.Parallel()

	list := gamelistXML(`<game><path>./Game (USA).sfc</path><name>Listed</name><core>snes9x2010</core></game>`)
	recovery := func(hash int) string {
		return `<?xml version="1.0"?>` + "\n" + `<gameList parentHash="` + strconv.Itoa(hash) + `">` +
			`<game><path>./Game (USA).sfc</path><governor>ondemand</governor></game></gameList>`
	}
	recoveryPath := filepath.Join(testHome, ".emulationstation", "recovery", "snes", "Game (USA).xml")

	t.Run("matching parentHash replaces the entry", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/roms/snes/gamelist.xml", list)
		writeFile(t, fs, recoveryPath, recovery(len(list)))
		assert.Equal(t, gameMeta{governor: "ondemand"}, loadGameMeta(fs, testSNES(), testROM, testHome))
	})
	t.Run("stale parentHash is ignored", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/roms/snes/gamelist.xml", list)
		writeFile(t, fs, recoveryPath, recovery(len(list)+1))
		assert.Equal(t, gameMeta{name: "Listed", core: "snes9x2010"}, loadGameMeta(fs, testSNES(), testROM, testHome))
	})
	t.Run("no gamelist matches parentHash 0", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFile(t, fs, recoveryPath, recovery(0))
		assert.Equal(t, gameMeta{governor: "ondemand"}, loadGameMeta(fs, testSNES(), testROM, testHome))
	})
	t.Run("ROM in a subfolder", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFile(t, fs, filepath.Join(testHome, ".emulationstation", "recovery", "snes", "hacks", "Hack.xml"),
			`<gameList parentHash="0"><game><path>./hacks/Hack.sfc</path><core>bsnes</core></game></gameList>`)
		assert.Equal(t, "bsnes", loadGameMeta(fs, testSNES(), "/roms/snes/hacks/Hack.sfc", testHome).core)
	})
}

func TestLoadGameMeta_ROMOutsideSystemFolder(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/roms/snes/gamelist.xml",
		gamelistXML(`<game><path>/roms2/snes/Game (USA).sfc</path><core>snes9x2010</core></game>`))
	assert.Equal(t, gameMeta{}, loadGameMeta(fs, testSNES(), "/roms2/snes/Game (USA).sfc", testHome))
}

func TestLoadGameMeta_Malformed(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/roms/snes/gamelist.xml", `<gameList><game><path>./Game (USA).sfc</path><core>x`)
	assert.Equal(t, gameMeta{}, loadGameMeta(fs, testSNES(), testROM, testHome))
}

func TestESGameName(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	snes := testSNES()
	named := gameMeta{name: "Listed"}

	assert.Equal(t, "Listed", esGameName(fs, snes, map[string]string{}, named, testROM, testHome))
	assert.Equal(t, "Game (USA)", esGameName(fs, snes, map[string]string{}, gameMeta{}, testROM, testHome))
	assert.Equal(t, "Game (USA)",
		esGameName(fs, snes, map[string]string{"ShowFilenames": "true"}, named, testROM, testHome))

	arcade := &esSystem{Name: "mame2003", Path: "/roms/mame2003", Platform: "neogeo, arcade"}
	rom := "/roms/mame2003/sf2.zip"
	assert.Equal(t, "sf2", esGameName(fs, arcade, map[string]string{}, gameMeta{}, rom, testHome),
		"no mamenames.xml")

	writeFile(t, fs, filepath.Join(esExeDir, "resources", "mamenames.xml"), `<?xml version="1.0"?>
		<game><mamename>sf2</mamename><realname>Street Fighter II</realname></game>
		<game><mamename>mslug</mamename><realname>Metal Slug</realname></game>`)
	assert.Equal(t, "Street Fighter II", esGameName(fs, arcade, map[string]string{}, gameMeta{}, rom, testHome))
	assert.Equal(t, "Listed", esGameName(fs, arcade, map[string]string{}, named, rom, testHome))
	assert.Equal(t, "Street Fighter II",
		esGameName(fs, arcade, map[string]string{"ShowFilenames": "true"}, named, rom, testHome))
	assert.Equal(t, "unknown",
		esGameName(fs, arcade, map[string]string{}, gameMeta{}, "/roms/mame2003/unknown.zip", testHome))

	// The user's resources folder shadows ES's own, even without the entry.
	writeFile(t, fs, filepath.Join(testHome, ".emulationstation", "resources", "mamenames.xml"),
		`<game><mamename>mslug</mamename><realname>User Slug</realname></game>`)
	assert.Equal(t, "User Slug",
		esGameName(fs, arcade, map[string]string{}, gameMeta{}, "/roms/mame2003/mslug.zip", testHome))
	assert.Equal(t, "sf2", esGameName(fs, arcade, map[string]string{}, gameMeta{}, rom, testHome))
}

func TestResolveGameLaunch_UsesGamelistAndNameSetting(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	writeFile(t, fs, esSettingsPath, `<string name="Listed.governor" value="powersave" />
<string name="snes.governor" value="userspace" />`)
	writeFile(t, fs, "/roms/snes/gamelist.xml",
		gamelistXML(`<game><path>./Game (USA).sfc</path><name>Listed</name><emulator>retroarch32</emulator></game>`))
	p := newTestPlatform(t, fs, newMockCmd())
	systems, err := loadESSystems(fs)
	require.NoError(t, err)

	launch := p.resolveGameLaunch(&systems[0], testROM, testHome)
	assert.Equal(t, "retroarch32", launch.emulator)
	assert.Equal(t, "snes9x2005", launch.core)
	assert.Equal(t, "powersave", launch.governor)
}

func TestESStartPath(t *testing.T) {
	t.Parallel()

	assert.Empty(t, esStartPath(&esSystem{Name: "snes", Path: "  "}, testHome))
	assert.Equal(t, "/roms/snes", esStartPath(testSNES(), testHome))
	assert.Equal(t, testHome+"/roms/snes", esStartPath(&esSystem{Path: "~/roms/snes/"}, testHome))
}

func TestResolveGamelistPath(t *testing.T) {
	t.Parallel()

	assert.Empty(t, resolveGamelistPath("", "/roms/snes"))
	assert.Equal(t, "/roms/snes", resolveGamelistPath(".", "/roms/snes"))
	assert.Equal(t, "/roms/snes/a/b.sfc", resolveGamelistPath("./a//b.sfc", "/roms/snes"))
	assert.Equal(t, "/other/b.sfc", resolveGamelistPath("/other/./b.sfc", "/roms/snes"))
}

func TestAttrUint(t *testing.T) {
	t.Parallel()

	attrs := []xml.Attr{
		{Name: xml.Name{Local: "other"}, Value: "7"},
		{Name: xml.Name{Local: "parentHash"}, Value: " 42 "},
		{Name: xml.Name{Local: "bad"}, Value: "-1"},
	}
	assert.Equal(t, int64(42), attrUint(attrs, "parentHash"))
	assert.Zero(t, attrUint(attrs, "bad"), "malformed reads as 0")
	assert.Zero(t, attrUint(attrs, "missing"))
}

func TestFindGamelistEntry_Edges(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	_, ok := findGamelistEntry(fs, "/missing.xml", "/roms/snes", testROM, nil)
	assert.False(t, ok, "missing file")

	// Elements before <gameList> are skipped whole.
	writeFile(t, fs, "/skip.xml", `<?xml version="1.0"?><other><game><path>./Game (USA).sfc</path></game></other>`+
		gamelistXML(`<game><path>./Game (USA).sfc</path><core>snes9x</core></game>`))
	meta, ok := findGamelistEntry(fs, "/skip.xml", "/roms/snes", testROM, nil)
	require.True(t, ok)
	assert.Equal(t, "snes9x", meta.core)

	// A broken element before <gameList> ends the read.
	writeFile(t, fs, "/broken-skip.xml", `<other><a>`)
	_, ok = findGamelistEntry(fs, "/broken-skip.xml", "/roms/snes", testROM, nil)
	assert.False(t, ok)

	// Bad XML after a match drops it: ES would not load that file either.
	writeFile(t, fs, "/bad.xml", `<gameList><game><path>./Game (USA).sfc</path><core>a</core></game>`+"\x00")
	_, ok = findGamelistEntry(fs, "/bad.xml", "/roms/snes", testROM, nil)
	assert.False(t, ok)

	// Non-game elements are read and ignored.
	writeFile(t, fs, "/folder.xml", gamelistXML(`<folder><path>./Game (USA).sfc</path><core>x</core></folder>`))
	_, ok = findGamelistEntry(fs, "/folder.xml", "/roms/snes", testROM, nil)
	assert.False(t, ok)
}

func TestMameRealName(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	assert.Equal(t, "sf2", mameRealName(fs, testHome, "sf2"), "no mamenames.xml")

	userNames := filepath.Join(testHome, ".emulationstation", "resources", "mamenames.xml")
	writeFile(t, fs, userNames, `<?xml version="1.0"?><names>
<game><mamename>pacman</mamename><realname>Pac-Man</realname></game>
<bios><mamename>neogeo</mamename></bios>
<game><mamename>sf2</mamename><realname>Street Fighter II</realname></game>
</names>`)
	assert.Equal(t, "Street Fighter II", mameRealName(fs, testHome, "sf2"))
	assert.Equal(t, "dkong", mameRealName(fs, testHome, "dkong"), "no entry")

	writeFile(t, fs, userNames, `<names><game><mamename>sf2</mamename><realname>x`)
	assert.Equal(t, "sf2", mameRealName(fs, testHome, "sf2"), "malformed entry")
}
