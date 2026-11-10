//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseESSystems(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)
	require.Len(t, systems, 4)

	snes := systems[0]
	assert.Equal(t, "snes", snes.Name)
	assert.Equal(t, "snes", snes.folder())
	assert.Equal(t, []string{".smc", ".sfc", ".zip"}, snes.extensions())
	require.Len(t, snes.Emulators, 2)
	assert.Equal(t, []string{"snes9x", "snes9x2010"}, snes.Emulators[0].Cores)
}

func TestParseESSystems_SkipsIncompleteAndRejectsGarbage(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(`<systemList>
<system><name>a</name><path>/roms/a</path></system>
<system><name></name><command>x</command></system>
<system><name>b</name><command>run %ROM%</command></system>
</systemList>`))
	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, "b", systems[0].Name)
	assert.Equal(t, "b", systems[0].folder(), "a system without a path is named for itself")

	_, err = parseESSystems([]byte("<systemList><system>"))
	require.Error(t, err)
}

// The stock R36S es_systems.cfg has a raw "2>&1" in its options system,
// which ES-fcamod accepts and a strict XML parser rejects.
func TestParseESSystems_RawAmpersandInCommand(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(`<systemList>
<system><name>nes</name><path>/roms/nes</path><command>run %ROM%</command></system>
<system><name>options</name><path>/opt/system</path>` +
		`<command>sudo chmod 666 /dev/tty1; %ROM% 2>&1 > /dev/tty1</command></system>
<system><name>snes</name><path>/roms/snes</path><command>run %ROM%</command></system>
</systemList>`))
	require.NoError(t, err)
	require.Len(t, systems, 3)
	assert.Equal(t, "snes", systems[2].Name, "systems after the raw & are still read")
	assert.Contains(t, systems[1].Command, "2>&1")
}

func TestLoadESSystems_UserFileWins(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, systemESSystemsPath,
		`<systemList><system><name>etc</name><command>x</command></system></systemList>`)
	systems, err := loadESSystems(fs)
	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, "etc", systems[0].Name)

	writeFile(t, fs, userESSystemsPath,
		`<systemList><system><name>home</name><command>x</command></system></systemList>`)
	systems, err = loadESSystems(fs)
	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, "home", systems[0].Name)
}

func TestLoadESSystems_Errors(t *testing.T) {
	t.Parallel()

	_, err := loadESSystems(afero.NewMemMapFs())
	require.Error(t, err)

	fs := afero.NewMemMapFs()
	writeFile(t, fs, userESSystemsPath, strings.Repeat(" ", maxConfigSize+1))
	_, err = loadESSystems(fs)
	require.ErrorContains(t, err, "larger than")
}

func TestParseESSettings(t *testing.T) {
	t.Parallel()

	settings := parseESSettings([]byte(`<?xml version="1.0"?>
<bool name="ShowHelpPrompts" value="true" />
<string name="snes.emulator" value="retroarch32" />
<string name="snes.core" value="snes9x2005" />
<string name="GlobalPerformanceGovernor" value="ondemand" />
<string name="novalue" />
`))
	assert.Equal(t, "retroarch32", settings["snes.emulator"])
	assert.Equal(t, "snes9x2005", settings["snes.core"])
	assert.Equal(t, "ondemand", settings["GlobalPerformanceGovernor"])
	assert.Equal(t, "true", settings["ShowHelpPrompts"])
	assert.NotContains(t, settings, "novalue")

	assert.Empty(t, loadESSettings(afero.NewMemMapFs()))
}

func TestResolveLaunch(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)
	snes := &systems[0]

	tests := []struct {
		settings map[string]string
		name     string
		emulator string
		core     string
		governor string
	}{
		{
			name:     "defaults",
			settings: map[string]string{},
			emulator: "retroarch", core: "snes9x", governor: "performance",
		},
		{
			name: "chosen emulator and core",
			settings: map[string]string{
				"snes.emulator": "retroarch32", "snes.core": "snes9x2005", "snes.governor": "powersave",
			},
			emulator: "retroarch32", core: "snes9x2005", governor: "powersave",
		},
		{
			name:     "core not offered by emulator falls back to first",
			settings: map[string]string{"snes.core": "snes9x2005", "GlobalPerformanceGovernor": "ondemand"},
			emulator: "retroarch", core: "snes9x", governor: "ondemand",
		},
		{
			name:     "unknown emulator falls back to first",
			settings: map[string]string{"snes.emulator": "mupen64plus"},
			emulator: "retroarch", core: "snes9x", governor: "performance",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			launch := resolveLaunch(snes, tt.settings, nil)
			assert.Equal(t, tt.emulator, launch.emulator)
			assert.Equal(t, tt.core, launch.core)
			assert.Equal(t, tt.governor, launch.governor)
			assert.Equal(t, snes.Command, launch.command)
		})
	}
}

func TestResolveLaunch_EmulatorCommandOverrides(t *testing.T) {
	t.Parallel()

	sys := &esSystem{
		Name:    "psp",
		Command: "default %ROM%",
		Emulators: []esEmulator{
			{Name: "ppsspp", Command: "standalone %ROM%"},
		},
	}
	launch := resolveLaunch(sys, map[string]string{}, nil)
	assert.Equal(t, "standalone %ROM%", launch.command)
	assert.Equal(t, "ppsspp", launch.emulator)
	assert.Empty(t, launch.core)
}

func TestSystemForPath(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)

	tests := []struct {
		name   string
		path   string
		system string
	}{
		{name: "system path", path: "/roms/snes/Game.sfc", system: "snes"},
		{name: "subfolder", path: "/roms/snes/USA/Game.sfc", system: "snes"},
		{name: "case-insensitive path", path: "/ROMS/SNES/Game.sfc", system: "snes"},
		{name: "second card by folder", path: "/roms2/nes/Game.nes", system: "nes"},
		{name: "sibling with shared prefix", path: "/roms/sfc/Game.sfc", system: "sfc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sys, sysErr := systemForPath(systems, tt.path, romRoots)
			require.NoError(t, sysErr)
			assert.Equal(t, tt.system, sys.Name)
		})
	}

	_, err = systemForPath(systems, "/roms/gb/Game.gb", romRoots)
	require.ErrorIs(t, err, errNoSystem)
	_, err = systemForPath(systems, "/tmp/nes/Game.nes", romRoots)
	require.ErrorIs(t, err, errNoSystem)
	_, err = systemForPath(systems, "/roms2/Game.nes", romRoots)
	require.ErrorIs(t, err, errNoSystem)
}

func TestTopFolder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "snes", topFolder("/roms/snes/a/b.sfc", romRoots))
	assert.Equal(t, "nes", topFolder("/roms2/nes/b.nes", romRoots))
	assert.Empty(t, topFolder("/roms/b.nes", romRoots))
	assert.Empty(t, topFolder("/roms/../etc/passwd", romRoots))
	assert.Empty(t, topFolder("/other/nes/b.nes", romRoots))
}

func TestResolveLaunch_GameOverrides(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)
	snes := &systems[0]

	tests := []struct {
		settings map[string]string
		game     *esGame
		name     string
		emulator string
		core     string
		governor string
	}{
		{
			name:     "game emulator and core beat system settings",
			settings: map[string]string{"snes.emulator": "retroarch", "snes.core": "snes9x2010"},
			game:     &esGame{emulator: "retroarch32", core: "snes9x2005"},
			emulator: "retroarch32", core: "snes9x2005", governor: "performance",
		},
		{
			name:     "game emulator takes that emulator's default core",
			settings: map[string]string{"snes.core": "snes9x2010"},
			game:     &esGame{emulator: "retroarch32"},
			emulator: "retroarch32", core: "snes9x2005", governor: "performance",
		},
		{
			name:     "game core is used as stored",
			settings: map[string]string{},
			game:     &esGame{core: "bsnes"},
			emulator: "retroarch", core: "bsnes", governor: "performance",
		},
		{
			name:     "game governor beats every setting",
			settings: map[string]string{"snes.governor": "ondemand", "Game.governor": "ondemand"},
			game:     &esGame{governor: "powersave", name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "powersave",
		},
		{
			name:     "system governor offered by the emulator beats the game name setting",
			settings: map[string]string{"snes.governor": "ondemand", "Game.governor": "powersave"},
			game:     &esGame{name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "ondemand",
		},
		{
			name:     "game name setting beats a system governor the emulator does not offer",
			settings: map[string]string{"snes.governor": "userspace", "Game.governor": "powersave"},
			game:     &esGame{name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "powersave",
		},
		{
			name:     "system governor the emulator does not offer still beats the global one",
			settings: map[string]string{"snes.governor": "userspace", "GlobalPerformanceGovernor": "ondemand"},
			game:     &esGame{name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "userspace",
		},
		{
			name:     "game name setting beats the global governor",
			settings: map[string]string{"Game.governor": "powersave", "GlobalPerformanceGovernor": "ondemand"},
			game:     &esGame{name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "powersave",
		},
		{
			name:     "another game's setting is ignored",
			settings: map[string]string{"Other.governor": "powersave", "GlobalPerformanceGovernor": "ondemand"},
			game:     &esGame{name: func() string { return "Game" }},
			emulator: "retroarch", core: "snes9x", governor: "ondemand",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			launch := resolveLaunch(snes, tt.settings, tt.game)
			assert.Equal(t, tt.emulator, launch.emulator)
			assert.Equal(t, tt.core, launch.core)
			assert.Equal(t, tt.governor, launch.governor)
		})
	}
}

func TestResolveLaunch_GameNameOnlyWhenNeeded(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)
	called := false
	game := &esGame{governor: "powersave", name: func() string {
		called = true
		return "Game"
	}}
	launch := resolveLaunch(&systems[0], map[string]string{}, game)
	assert.Equal(t, "powersave", launch.governor)
	assert.False(t, called, "the game name is not resolved when no setting needs it")
}

func TestResolveLaunch_EmulatorGovernorsExtendBuiltins(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(`<systemList><system>
		<name>n64</name><path>/roms/n64/</path><extension>.z64</extension>
		<command>%ROM%</command>
		<emulators>
			<emulator name="mupen64plus">
				<cores><core>gliden64</core></cores>
				<governors><governor> schedutil </governor></governors>
			</emulator>
			<emulator name="retroarch"><cores><core>parallel_n64</core></cores></emulator>
		</emulators>
	</system></systemList>`))
	require.NoError(t, err)
	n64 := &systems[0]
	game := &esGame{name: func() string { return "Game" }}
	settings := map[string]string{"n64.governor": "schedutil", "Game.governor": "powersave"}

	launch := resolveLaunch(n64, settings, game)
	assert.Equal(t, "schedutil", launch.governor, "the emulator lists schedutil")

	game.emulator = "retroarch"
	launch = resolveLaunch(n64, settings, game)
	assert.Equal(t, "powersave", launch.governor, "retroarch does not list schedutil")
	assert.Equal(t, "parallel_n64", launch.core)
}

// ES-fcamod parses <defaultCore> and <defaultGov> but never uses them.
func TestResolveLaunch_IgnoresDefaultCoreAndGov(t *testing.T) {
	t.Parallel()

	systems, err := parseESSystems([]byte(`<systemList><system>
		<name>snes</name><path>/roms/snes/</path><extension>.sfc</extension>
		<command>%ROM%</command>
		<defaultCore>snes9x2010</defaultCore>
		<defaultGov>powersave</defaultGov>
		<emulators>
			<emulator name="retroarch"><cores><core>snes9x</core><core>snes9x2010</core></cores></emulator>
		</emulators>
	</system></systemList>`))
	require.NoError(t, err)
	launch := resolveLaunch(&systems[0], map[string]string{}, nil)
	assert.Equal(t, "snes9x", launch.core)
	assert.Equal(t, "performance", launch.governor)
}
