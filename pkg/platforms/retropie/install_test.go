//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	"errors"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testExe = "/home/pi/zaparoo/zaparoo"

type commandLog struct {
	failOn string
	calls  []string
}

func (c *commandLog) run(_ context.Context, name string, args ...string) error {
	call := strings.Join(append([]string{name}, args...), " ")
	c.calls = append(c.calls, call)
	if c.failOn != "" && strings.Contains(call, c.failOn) {
		return errors.New("command failed")
	}
	return nil
}

func TestRenderUnit(t *testing.T) {
	t.Parallel()

	unit, err := renderUnit("pi", testExe)
	require.NoError(t, err)
	for _, line := range []string{
		"User=pi\n",
		"WorkingDirectory=/home/pi/zaparoo\n",
		"ExecStart=/home/pi/zaparoo/zaparoo -daemon\n",
		"Restart=on-failure\n",
		"RestartPreventExitStatus=78\n",
		"WantedBy=multi-user.target\n",
	} {
		assert.Contains(t, unit, line)
	}
	assert.NotContains(t, unit, "@")

	for name, tc := range map[string]struct{ user, exe string }{
		"root-like user":    {user: "pi\nUser=root", exe: testExe},
		"empty user":        {user: "", exe: testExe},
		"upper user":        {user: "Pi", exe: testExe},
		"relative exe":      {user: "pi", exe: "zaparoo"},
		"space in exe":      {user: "pi", exe: "/home/pi/my apps/zaparoo"},
		"specifier in exe":  {user: "pi", exe: "/home/pi/%h/zaparoo"},
		"newline in exe":    {user: "pi", exe: "/home/pi/z\nExecStartPre=/bin/sh"},
		"semicolon in exe":  {user: "pi", exe: "/home/pi/z;reboot"},
		"backslash in exe":  {user: "pi", exe: `/home/pi/z\x20`},
		"quote in exe path": {user: "pi", exe: `/home/pi/"z"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := renderUnit(tc.user, tc.exe)
			require.Error(t, err)
		})
	}
}

func newSystemSide(cmds *commandLog) (side systemSide, installed, uninstalled *int) {
	installed, uninstalled = new(int), new(int)
	return systemSide{
		fs:  afero.NewMemMapFs(),
		run: cmds.run,
		lookupUser: func(name string) (*user.User, error) {
			switch name {
			case "pi":
				return &user.User{Username: "pi", Uid: "1000"}, nil
			case "root":
				return &user.User{Username: "root", Uid: "0"}, nil
			default:
				return nil, user.UnknownUserError(name)
			}
		},
		installHardware:   func() error { *installed++; return nil },
		uninstallHardware: func() error { *uninstalled++; return nil },
		unitPath:          ServiceFilePath,
	}, installed, uninstalled
}

func TestSystemInstall(t *testing.T) {
	t.Parallel()
	cmds := &commandLog{}
	side, installed, uninstalled := newSystemSide(cmds)

	require.NoError(t, side.install(context.Background(), "pi", testExe))

	unit := readTestFile(t, side.fs, ServiceFilePath)
	want, err := renderUnit("pi", testExe)
	require.NoError(t, err)
	assert.Equal(t, want, unit)
	info, err := side.fs.Stat(ServiceFilePath)
	require.NoError(t, err)
	assert.Equal(t, "-rw-r--r--", info.Mode().Perm().String())
	assert.Equal(t, 1, *installed)
	assert.Equal(t, []string{
		"systemctl daemon-reload",
		"systemctl enable zaparoo.service",
		"systemctl restart zaparoo.service",
	}, cmds.calls)

	cmds.calls = nil
	require.NoError(t, side.uninstall(context.Background()))
	exists, err := afero.Exists(side.fs, ServiceFilePath)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, 1, *uninstalled)
	assert.Equal(t, []string{
		"systemctl stop zaparoo.service",
		"systemctl disable zaparoo.service",
		"systemctl daemon-reload",
	}, cmds.calls)
}

func TestSystemInstallRefusals(t *testing.T) {
	t.Parallel()

	for name, userName := range map[string]string{
		"root":    "root",
		"unknown": "nobody-here",
		"invalid": "pi;rm",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmds := &commandLog{}
			side, installed, _ := newSystemSide(cmds)
			require.Error(t, side.install(context.Background(), userName, testExe))
			assert.Empty(t, cmds.calls)
			assert.Zero(t, *installed)
			exists, err := afero.Exists(side.fs, ServiceFilePath)
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestUserInstall(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
	cmds := &commandLog{}

	require.NoError(t, tp.installUser(context.Background(), "pi", testExe, cmds.run))

	assert.Contains(t, readTestFile(t, tp.fs, allPath(autostartFile)), blockBegin)
	assert.Contains(t, readTestFile(t, tp.fs, allPath(onStartFile)), blockBegin)
	assert.Contains(t, readTestFile(t, tp.fs, allPath(onEndFile)), blockBegin)
	isDir, err := afero.IsDir(tp.fs, testStateDir)
	require.NoError(t, err)
	assert.True(t, isDir)
	assert.Equal(t, []string{"sudo -- " + testExe + " -install-system=pi"}, cmds.calls)

	cmds.calls = nil
	require.NoError(t, tp.uninstallUser(context.Background(), testExe, cmds.run))
	assert.Equal(t, retroPieAuto, readTestFile(t, tp.fs, allPath(autostartFile)))
	assert.Equal(t, []string{"sudo -- " + testExe + " -uninstall-system"}, cmds.calls)
}

func TestUserInstallStopsBeforeSudo(t *testing.T) {
	t.Parallel()

	t.Run("no autostart", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		cmds := &commandLog{}
		require.Error(t, tp.installUser(context.Background(), "pi", testExe, cmds.run))
		assert.Empty(t, cmds.calls)
	})

	t.Run("unusable binary path", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
		cmds := &commandLog{}
		require.Error(t, tp.installUser(context.Background(), "pi", "/home/pi/my zaparoo/zaparoo", cmds.run))
		assert.Empty(t, cmds.calls)
		assert.Equal(t, retroPieAuto, readTestFile(t, tp.fs, allPath(autostartFile)))
	})

	t.Run("sudo fails", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
		cmds := &commandLog{failOn: "sudo"}
		require.Error(t, tp.installUser(context.Background(), "pi", testExe, cmds.run))
		assert.Equal(t, retroPieAuto, readTestFile(t, tp.fs, allPath(autostartFile)),
			"hooks are rolled back when the service install fails")
		exists, err := afero.Exists(tp.fs, allPath(onStartFile))
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("desktop autostart", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
		writeTestFile(t, tp.fs, "/home/pi/.config/autostart/retropie.desktop", "[Desktop Entry]\n", 0o644)
		cmds := &commandLog{}
		err := tp.installUser(context.Background(), "pi", testExe, cmds.run)
		require.ErrorContains(t, err, "desktop session")
		assert.Empty(t, cmds.calls)
		assert.Equal(t, retroPieAuto, readTestFile(t, tp.fs, allPath(autostartFile)))
	})
}

func TestSystemMap(t *testing.T) {
	t.Parallel()

	ids := map[string]string{}
	for folder, info := range systemMap {
		require.NoError(t, validateSystemName(folder), folder)
		_, err := systemdefs.GetSystem(info.systemID)
		require.NoError(t, err, "%s: %s", folder, info.systemID)
		if other, dup := ids[info.launcherID]; dup {
			t.Errorf("launcher ID %q used by %s and %s", info.launcherID, other, folder)
		}
		ids[info.launcherID] = folder
		assert.NotEmpty(t, info.extensions, folder)
		for _, ext := range info.extensions {
			assert.Equal(t, strings.ToLower(ext), ext, folder)
			assert.True(t, strings.HasPrefix(ext, "."), folder)
		}
		if info.kind == requestPort {
			assert.Equal(t, "ports", folder)
		}
	}
}

func TestLaunchers(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	var nes, ports bool
	launchers := tp.Launchers(cfg)
	for i := range launchers {
		l := launchers[i]
		switch l.ID {
		case "NES":
			nes = true
			assert.Equal(t, []string{"nes"}, l.Folders)
			assert.NotNil(t, l.WaitForReady)
			assert.NotNil(t, l.GameRunning)
		case "Ports":
			ports = true
			assert.Nil(t, l.WaitForReady, "ports are not tracked")
			assert.Nil(t, l.GameRunning)
		}
	}
	assert.True(t, nes)
	assert.True(t, ports)
}

func TestDescendantsOf(t *testing.T) {
	t.Parallel()

	table := map[int]procInfo{
		1:   {comm: "init", ppid: 0},
		700: {comm: "runcommand.sh", ppid: 500},
		701: {comm: "retroarch", ppid: 700},
		702: {comm: "sh", ppid: 701},
		703: {comm: "helper", ppid: 702},
		800: {comm: "other", ppid: 1},
	}
	got := descendantsOf(table, 700)
	assert.ElementsMatch(t, []int{701, 702, 703}, got)
	assert.Empty(t, descendantsOf(table, 703))
}

func TestProcFS(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(pid, stat, cmdline string) {
		dir := filepath.Join(root, pid)
		require.NoError(t, afero.NewOsFs().MkdirAll(dir, 0o755))
		writeHostFile(t, filepath.Join(dir, "stat"), stat, 0o600)
		writeHostFile(t, filepath.Join(dir, "cmdline"), cmdline, 0o600)
	}
	write("600", "600 (emulationstatio) S 500 600 600 0", "emulationstation\x00--no-splash\x00")
	write("700", "700 (run (x) cmd) S 500 700 700 0", "bash\x00runcommand.sh\x00")
	write("701", "701 (retroarch) Z 700 700 700 0", "")
	write("702", "702 (emulationstatio) Z 1 1 1 0", "")

	p := procFS{root: root}
	assert.True(t, p.alive(600))
	assert.False(t, p.alive(701), "a zombie has exited")
	assert.False(t, p.alive(999))
	assert.Equal(t, "emulationstation --no-splash", p.cmdline(600))
	assert.Equal(t, []int{600}, p.findByComm(esComm), "zombies are not running")
	assert.Equal(t, []int{701}, p.descendants(700))
	info, ok := p.stat(700)
	require.True(t, ok)
	assert.Equal(t, "run (x) cmd", info.comm)
	assert.Equal(t, 500, info.ppid)
}
