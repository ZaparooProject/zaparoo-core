//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addProc(t *testing.T, fs afero.Fs, pid, ppid int, comm string) {
	t.Helper()
	writeFile(t, fs, fmt.Sprintf("/proc/%d/comm", pid), comm+"\n")
	writeFile(t, fs, fmt.Sprintf("/proc/%d/stat", pid), fmt.Sprintf("%d (%s) S %d 1 1 0", pid, comm, ppid))
}

func cmdline(args ...string) string {
	return strings.Join(args, "\x00") + "\x00"
}

func retroarchProc(pid int, rom string) procscanner.ProcessInfo {
	return procscanner.ProcessInfo{
		PID:  pid,
		Comm: "retroarch",
		Cmdline: cmdline("/usr/local/bin/retroarch", "-L",
			"/home/ark/.config/retroarch/cores/snes9x_libretro.so", rom),
	}
}

func TestESGameMatcher(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	addProc(t, fs, 100, 1, esComm)
	addProc(t, fs, 200, 100, "sh")
	addProc(t, fs, 300, 200, "retroarch")
	addProc(t, fs, 301, 100, "retroarch")
	addProc(t, fs, 400, 1, "bash")
	addProc(t, fs, 401, 400, "retroarch")
	addProc(t, fs, 500, 100, "python3")
	addProc(t, fs, 501, 500, "retroarch")
	m := &esGameMatcher{fs: fs, procDir: "/proc", roots: romRoots}

	tests := []struct {
		name string
		proc procscanner.ProcessInfo
		want bool
	}{
		{name: "child of ES shell", proc: retroarchProc(300, "/roms/snes/Game.sfc"), want: true},
		{name: "direct child of ES", proc: retroarchProc(301, "/roms2/snes/Game.sfc"), want: true},
		{name: "shell not started by ES", proc: retroarchProc(401, "/roms/snes/Game.sfc")},
		{name: "parent is not a shell", proc: retroarchProc(501, "/roms/snes/Game.sfc")},
		{name: "ROM outside roots", proc: retroarchProc(300, "/tmp/snes/Game.sfc")},
		{name: "relative ROM", proc: retroarchProc(300, "roms/snes/Game.sfc")},
		{name: "unclean ROM", proc: retroarchProc(300, "/roms/snes/../snes/Game.sfc")},
		{name: "unknown process", proc: retroarchProc(999, "/roms/snes/Game.sfc")},
		{
			name: "perfmax helper",
			proc: procscanner.ProcessInfo{
				PID: 300, Comm: "sudo", Cmdline: cmdline("sudo", "perfmax", "performance", "/roms/snes/Game.sfc"),
			},
		},
		{
			name: "no arguments",
			proc: procscanner.ProcessInfo{PID: 300, Comm: "retroarch", Cmdline: cmdline("/roms/snes/Game.sfc")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, m.Match(tt.proc))
		})
	}
}

func TestESGameMatcher_ParentWithParensInComm(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/proc/7/stat", "7 (a) b (c)) S 42 1 1")
	m := &esGameMatcher{fs: fs, procDir: "/proc"}
	ppid, ok := m.parent(7)
	require.True(t, ok)
	assert.Equal(t, 42, ppid)

	writeFile(t, fs, "/proc/8/stat", "8 (x S")
	_, ok = m.parent(8)
	assert.False(t, ok)
}

func TestESGameTracking(t *testing.T) {
	t.Parallel()

	p := newTestPlatform(t, newTestFs(t), newMockCmd())
	var current *models.ActiveMedia
	p.activeMedia = func() *models.ActiveMedia { return current }
	p.setActiveMedia = func(m *models.ActiveMedia) { current = m }

	p.esGameStarted(retroarchProc(300, "/roms/snes/Super Game (USA).sfc"))
	require.NotNil(t, current)
	info, ok := esde.LookupByFolderName("snes")
	require.True(t, ok)
	assert.Equal(t, info.SystemID, current.SystemID)
	assert.Equal(t, "Super Game (USA)", current.Name)
	assert.Equal(t, "/roms/snes/Super Game (USA).sfc", current.Path)
	assert.Equal(t, info.GetLauncherID(), current.LauncherID)

	p.esGameStopped(999)
	assert.NotNil(t, current, "another process exiting changes nothing")

	p.esGameStopped(300)
	assert.Nil(t, current)
}

func TestESGameTracking_KeepsNewerMedia(t *testing.T) {
	t.Parallel()

	p := newTestPlatform(t, newTestFs(t), newMockCmd())
	var current *models.ActiveMedia
	p.activeMedia = func() *models.ActiveMedia { return current }
	p.setActiveMedia = func(m *models.ActiveMedia) { current = m }

	p.esGameStarted(retroarchProc(300, "/roms/snes/Game.sfc"))
	current = &models.ActiveMedia{Path: "/roms/nes/Other.nes"}
	p.esGameStopped(300)
	require.NotNil(t, current)
	assert.Equal(t, "/roms/nes/Other.nes", current.Path)
}

func TestESGameTracking_IgnoresUnknownSystems(t *testing.T) {
	t.Parallel()

	p := newTestPlatform(t, newTestFs(t), newMockCmd())
	called := false
	p.setActiveMedia = func(*models.ActiveMedia) { called = true }

	p.esGameStarted(retroarchProc(300, "/roms/zzunknown/a.bin"))
	p.esGameStarted(retroarchProc(301, "/roms/gb/a.gb"))
	assert.False(t, called)
	assert.Zero(t, p.takeESGame())
}

func TestLookupSystem_AliasesAndPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want string
		sys  esSystem
	}{
		{"Genesis", esSystem{Name: "genesis", Path: "/roms/genesis"}},
		{"SNES", esSystem{Name: "sfc", Path: "/roms/sfc"}},
		{"Arcade", esSystem{Name: "mame2003", Path: "/roms/mame2003", Platform: "arcade"}},
		{"SNES", esSystem{Name: "snes-hacks", Path: "/roms/snes-hacks", Platform: "snes"}},
	}
	for _, tt := range tests {
		info, ok := lookupSystem(&tt.sys)
		require.True(t, ok, tt.sys.Name)
		assert.Equal(t, tt.want, info.SystemID, tt.sys.Name)
	}

	_, ok := lookupSystem(&esSystem{Name: "zzunknown", Path: "/roms/zzunknown", Platform: "zzunknown"})
	assert.False(t, ok)
}

func TestLookupSystem_ArkOSSystems(t *testing.T) {
	t.Parallel()

	const singe = "sudo perfmax %GOVERNOR% %ROM%; nice -n -19 /usr/local/bin/singe.sh %ROM%; sudo perfnorm"
	const gametank = "sudo perfmax %GOVERNOR% %ROM%; nice -n -19 /usr/local/bin/gametank.sh " +
		"%EMULATOR% %CORE% %ROM%; sudo perfnorm"
	tests := []struct {
		wantSystem   string
		wantLauncher string
		sys          esSystem
	}{
		{"VMU", "VMU", esSystem{Name: "vmu", Path: "/roms/vmu", Command: testCommand}},
		{"GameTank", "GameTank", esSystem{Name: "gametank", Path: "/roms/gametank", Command: gametank}},
		{"C16", "C16", esSystem{Name: "c16", Path: "/roms/c16", Command: testCommand}},
		{"PalmOS", "PalmOS", esSystem{Name: "palm", Path: "/roms/palm", Command: testCommand}},
		{"DAPHNE", "Daphne", esSystem{Name: "alg", Path: "/roms/alg", Command: singe}},
		{"DAPHNE", "Daphne", esSystem{
			Name: "alg", Path: "/roms/alg", Command: testCommand,
			Emulators: []esEmulator{{Name: "hypseus", Command: "/opt/hypseus-singe/hypseus-singe %ROM%"}},
		}},
		{"Arcade", "Arcade", esSystem{Name: "alg", Path: "/roms/alg", Command: testCommand}},
	}
	for _, tt := range tests {
		info, ok := lookupSystem(&tt.sys)
		require.True(t, ok, tt.sys.Name)
		assert.Equal(t, tt.wantSystem, info.SystemID, tt.sys.Name)
		assert.Equal(t, tt.wantLauncher, info.GetLauncherID(), tt.sys.Name)
	}
}
