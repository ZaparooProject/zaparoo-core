//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snesLaunch(t *testing.T) esLaunch {
	t.Helper()
	systems, err := parseESSystems([]byte(testESSystems))
	require.NoError(t, err)
	return resolveLaunch(&systems[0], map[string]string{}, nil)
}

func TestBuildScript(t *testing.T) {
	t.Parallel()

	launch := snesLaunch(t)
	script, err := buildScript(&launch, "/home/ark")
	require.NoError(t, err)
	assert.Equal(t,
		`sudo perfmax performance "$1"; nice -n -19 /usr/local/bin/retroarch `+
			`-L /home/ark/.config/retroarch/cores/snes9x_libretro.so "$1"; sudo perfnorm`,
		script)
}

func TestBuildScript_AllPlaceholders(t *testing.T) {
	t.Parallel()

	launch := esLaunch{
		system:   &esSystem{Name: "ports"},
		command:  "%HOME%/run %SYSTEM% %ROM_RAW% %BASENAME% %ROM%",
		emulator: "e", core: "c", governor: "g",
	}
	script, err := buildScript(&launch, "/home/ark")
	require.NoError(t, err)
	assert.Equal(t, `/home/ark/run ports "$1" "$2" "$1"`, script)
}

func TestBuildScript_RefusesUnsafeTokens(t *testing.T) {
	t.Parallel()

	base := snesLaunch(t)
	for _, mutate := range []func(*esLaunch){
		func(l *esLaunch) { l.core = "snes9x; rm -rf /" },
		func(l *esLaunch) { l.emulator = "$(id)" },
		func(l *esLaunch) { l.governor = "performance`id`" },
		func(l *esLaunch) { l.system = &esSystem{Name: "a b"} },
	} {
		launch := base
		mutate(&launch)
		_, err := buildScript(&launch, "/home/ark")
		require.Error(t, err)
	}

	empty := base
	empty.command = ""
	_, err := buildScript(&empty, "/home/ark")
	require.Error(t, err)
}

func TestBuildScript_QuotedPlaceholders(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`run "%ROM%"`:                 `run "$1"`,
		`run '%ROM%'`:                 `run ''"$1"''`,
		`run "/saves/%BASENAME%.srm"`: `run "/saves/$2.srm"`,
		`sh -c 'run %ROM%' x`:         `sh -c 'run '"$1"'' x`,
		`echo \"%ROM%`:                `echo \""$1"`,
		`run "it's %ROM%"`:            `run "it's $1"`,
	}
	for command, want := range tests {
		launch := esLaunch{system: &esSystem{Name: "snes"}, command: command}
		got, err := buildScript(&launch, "/home/ark")
		require.NoError(t, err)
		assert.Equal(t, want, got, command)
	}
}

// TestGameScript_QuotedPlaceholdersKeepPathWhole runs templates that quote
// the placeholder themselves and checks the path arrives as one argument.
func TestGameScript_QuotedPlaceholdersKeepPathWhole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := `Two  Spaces $(touch pwned) 'q'`
	rom := filepath.Join(dir, name+".sfc")
	for _, command := range []string{
		`printf '[%s]' "%ROM%"`,
		`printf '[%s]' '%ROM%'`,
		`printf '[%s]' %ROM%`,
	} {
		launch := esLaunch{system: &esSystem{Name: "snes"}, command: command}
		acct := testAccount()
		acct.home = dir
		spec, err := newLaunchSpec(&launch, acct, rom)
		require.NoError(t, err)

		argv := append([]string{"-c", spec.script, shellArgv0}, spec.args...)
		//nolint:gosec // G204: runs the script under test
		game := exec.CommandContext(context.Background(), shellPath, argv...)
		game.Dir = dir
		out, err := game.Output()
		require.NoError(t, err, command)
		assert.Equal(t, "["+rom+"]", string(out), command)
	}
	assert.NoFileExists(t, filepath.Join(dir, "pwned"))
}

// TestGameScript_ROMPathIsNeverShellSource runs the generated script through
// the real shell with a hostile file name and checks the path arrives intact
// and nothing in it runs.
func TestGameScript_ROMPathIsNeverShellSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	canary := filepath.Join(dir, "pwned")
	name := `Game $(touch pwned) "x" 'y' ; touch pwned & b`
	rom := filepath.Join(dir, name+".sfc")

	launch := esLaunch{
		system:  &esSystem{Name: "snes"},
		command: `printf '%s|%s' %ROM% %BASENAME%`,
	}
	acct := testAccount()
	acct.home = dir
	spec, err := newLaunchSpec(&launch, acct, rom)
	require.NoError(t, err)
	assert.NotContains(t, spec.script, "touch")

	argv := append([]string{"-c", spec.script, shellArgv0}, spec.args...)
	//nolint:gosec // G204: runs the script under test
	game := exec.CommandContext(context.Background(), shellPath, argv...)
	game.Dir = dir
	out, err := game.Output()
	require.NoError(t, err)

	assert.Equal(t, rom+"|"+name, string(out))
	assert.NoFileExists(t, canary)
}

func TestGameCommand(t *testing.T) {
	t.Parallel()

	launch := snesLaunch(t)
	spec, err := newLaunchSpec(&launch, testAccount(), "/roms/snes/Game (USA).sfc")
	require.NoError(t, err)
	assert.Equal(t, []string{"/roms/snes/Game (USA).sfc", "Game (USA)"}, spec.args)

	cmd := gameCommand(spec)
	assert.Equal(t, shellPath, cmd.Path)
	assert.Equal(t,
		[]string{shellPath, "-c", spec.script, shellArgv0, "/roms/snes/Game (USA).sfc", "Game (USA)"},
		cmd.Args)
	assert.Equal(t, frontendHome, cmd.Dir)
	assert.Contains(t, cmd.Env, "HOME=/home/ark")
	assert.Contains(t, cmd.Env, "USER=ark")
	assert.Contains(t, cmd.Env, "XDG_RUNTIME_DIR=/run/user/1002/")
	assert.Contains(t, cmd.Env, "SDL_VIDEO_EGL_DRIVER=libEGL.so")
	require.NotNil(t, cmd.SysProcAttr)
	require.NotNil(t, cmd.SysProcAttr.Credential)
	assert.Equal(t, uint32(1002), cmd.SysProcAttr.Credential.Uid)
	assert.Equal(t, uint32(1002), cmd.SysProcAttr.Credential.Gid)
	assert.Equal(t, []uint32{4, 27}, cmd.SysProcAttr.Credential.Groups)
}

func TestNewLaunchSpec_RefusesUnsafeLaunch(t *testing.T) {
	t.Parallel()

	launch := snesLaunch(t)
	launch.core = "snes9x; id"
	spec, err := newLaunchSpec(&launch, testAccount(), "/roms/snes/Game.sfc")
	require.Error(t, err)
	assert.Nil(t, spec)
}

func TestAccountFromUser(t *testing.T) {
	t.Parallel()

	me, err := user.Current()
	require.NoError(t, err)

	acct, err := accountFromUser(me)
	require.NoError(t, err)
	assert.Equal(t, me.Username, acct.name)
	assert.Equal(t, me.Uid, strconv.FormatUint(uint64(acct.uid), 10))
	assert.Equal(t, me.Gid, strconv.FormatUint(uint64(acct.gid), 10))
	assert.NotEmpty(t, acct.groups)

	noHome := *me
	noHome.HomeDir = ""
	acct, err = accountFromUser(&noHome)
	require.NoError(t, err)
	assert.Equal(t, frontendHome, acct.home)

	badUID := *me
	badUID.Uid = "x"
	_, err = accountFromUser(&badUID)
	require.ErrorContains(t, err, "parse uid")

	badGID := *me
	badGID.Gid = "x"
	_, err = accountFromUser(&badGID)
	require.ErrorContains(t, err, "parse gid")
}

func TestStartGameProcess_ReportsStartFailure(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root may switch credentials, so the start would succeed")
	}

	spec := &launchSpec{acct: testAccount(), script: "exit 0"}
	spec.acct.home = t.TempDir()
	proc, err := startGameProcess(spec)
	require.ErrorContains(t, err, "start game")
	assert.Nil(t, proc)
}
