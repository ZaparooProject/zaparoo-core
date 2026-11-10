//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testROM = "/roms/snes/Game (USA).sfc"

func TestSettings(t *testing.T) {
	t.Parallel()

	s := NewPlatform().Settings()
	assert.Equal(t, InstallDir, s.DataDir)
	assert.Equal(t, InstallDir, s.ConfigDir)
	assert.Equal(t, filepath.Join(InstallDir, config.LogsDir), s.LogDir)
}

func TestScrapers(t *testing.T) {
	t.Parallel()

	scrapers := NewPlatform().Scrapers(nil)
	assert.Contains(t, scrapers, "gamelist.xml")
	assert.Contains(t, scrapers, "media-folder")
}

func TestRootDirs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/roms", 0o755))
	p := newTestPlatform(t, fs, newMockCmd())
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, []string{"/roms"}, p.RootDirs(cfg))

	require.NoError(t, fs.MkdirAll("/roms2", 0o755))
	assert.Equal(t, []string{"/roms", "/roms2"}, p.RootDirs(cfg))
}

func TestLaunchers(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	writeFile(t, fs, filepath.Join(frontendHome, ".config", "retroarch", "retroarch.cfg"),
		"network_cmd_enable = \"true\"\n")
	p := newTestPlatform(t, fs, newMockCmd())

	launchers := p.esLaunchers()
	snesInfo, ok := esde.LookupByFolderName("snes")
	require.True(t, ok)
	nesInfo, ok := esde.LookupByFolderName("nes")
	require.True(t, ok)

	byID := map[string]platforms.Launcher{}
	for _, l := range launchers {
		byID[l.ID] = l
	}
	snes, ok := byID[snesInfo.GetLauncherID()]
	require.True(t, ok)
	assert.Equal(t, platforms.LifecycleBlocking, snes.Lifecycle)
	assert.NotNil(t, snes.Launch)
	assert.Contains(t, snes.Folders, "snes")
	assert.Contains(t, snes.Extensions, ".sfc")
	assert.NotEmpty(t, snes.Controls, "retroarch has network commands on")

	nes, ok := byID[nesInfo.GetLauncherID()]
	require.True(t, ok)
	assert.Empty(t, nes.Controls, "retroarch32 config is absent")

	for _, l := range launchers {
		assert.NotContains(t, l.Folders, "zzunknown")
	}

	if sfcInfo, sfcOK := esde.LookupByFolderName("sfc"); sfcOK && sfcInfo.GetLauncherID() == snesInfo.GetLauncherID() {
		assert.Contains(t, snes.Folders, "sfc")
		assert.Contains(t, snes.Extensions, ".bs")
	}
}

func TestLaunchers_UnreadableConfig(t *testing.T) {
	t.Parallel()

	p := newTestPlatform(t, afero.NewMemMapFs(), newMockCmd())
	assert.Empty(t, p.esLaunchers())
}

func TestLaunchGame(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	writeFile(t, fs, testROM, "rom")
	writeFile(t, fs, esSettingsPath, `<string name="snes.core" value="snes9x2010" />`)
	cmd := newMockCmd()
	p := newTestPlatform(t, fs, cmd)

	self, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	var got *launchSpec
	p.startGame = func(spec *launchSpec) (*os.Process, error) {
		assert.Equal(t, []string{esStop}, cmdCalls(cmd), "ES is stopped before the game starts")
		got = spec
		return self, nil
	}

	proc, err := p.launchGame(nil, testROM, nil)
	require.NoError(t, err)
	assert.Same(t, self, proc)
	require.NotNil(t, got)
	assert.Equal(t, []string{testROM, "Game (USA)"}, got.args)
	assert.Contains(t, got.script, "snes9x2010_libretro.so")
	assert.NotContains(t, got.script, "Game (USA)")
	assert.Equal(t, os.Getpid(), p.fe.runningGame())
}

func TestLaunchGame_FailureRestoresES(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	writeFile(t, fs, testROM, "rom")
	cmd := newMockCmd()
	p := newTestPlatform(t, fs, cmd)
	p.startGame = func(*launchSpec) (*os.Process, error) { return nil, errors.New("exec failed") }

	_, err := p.launchGame(nil, testROM, nil)
	require.Error(t, err)
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
}

func TestLaunchGame_RejectsBadPaths(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	writeFile(t, fs, "/roms/gb/Game.gb", "rom")
	require.NoError(t, fs.MkdirAll("/roms/snes/dir.sfc", 0o755))
	cmd := newMockCmd()
	p := newTestPlatform(t, fs, cmd)
	p.lookupAccount = func() (*account, error) { return nil, errors.New("no user") }

	for _, path := range []string{
		"roms/snes/Game.sfc",
		"/roms/snes/Missing.sfc",
		"/roms/snes/dir.sfc",
		"/roms/gb/Game.gb",
	} {
		_, err := p.launchGame(nil, path, nil)
		require.Error(t, err, path)
	}

	writeFile(t, fs, testROM, "rom")
	_, err := p.launchGame(nil, testROM, nil)
	require.ErrorContains(t, err, "no user")
	assert.Empty(t, cmdCalls(cmd), "ES is never stopped for a launch that cannot run")
}

func TestWaitTrackedProcess_RestoresES(t *testing.T) {
	t.Parallel()

	cmd := newMockCmd()
	p := newTestPlatform(t, afero.NewMemMapFs(), cmd)

	game := exec.CommandContext(context.Background(), "true")
	require.NoError(t, game.Start())

	p.fe.beginLaunch()
	require.NoError(t, p.fe.stopES())
	p.fe.gameStarted(game.Process.Pid)
	p.fe.endLaunch()
	p.SetTrackedProcess(game.Process)

	require.NoError(t, p.WaitTrackedProcess(game.Process))
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
}

func TestStopActiveLauncher_KillsESGame(t *testing.T) {
	t.Parallel()

	p := newTestPlatform(t, newTestFs(t), newMockCmd())
	var current *models.ActiveMedia
	p.activeMedia = func() *models.ActiveMedia { return current }
	p.setActiveMedia = func(m *models.ActiveMedia) { current = m }
	var killed []int
	p.killESGame = func(pid int) { killed = append(killed, pid) }

	p.esGameStarted(retroarchProc(300, testROM))
	require.NotNil(t, current)

	require.NoError(t, p.ReturnToMenu())
	assert.Equal(t, []int{300}, killed)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForPreemption))
	assert.Equal(t, []int{300}, killed, "nothing left to kill")
}

func TestStop_RestoresES(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, esStoppedMarker, "")
	cmd := newMockCmd()
	p := newTestPlatform(t, fs, cmd)
	p.fe.stopped = true

	require.NoError(t, p.Stop())
	assert.Equal(t, []string{esStart}, cmdCalls(cmd))
}

func TestTerminateTree(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	game := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & wait")
	require.NoError(t, game.Start())
	done := make(chan error, 1)
	go func() { done <- game.Wait() }()

	p.terminateTree(game.Process.Pid)
	require.Error(t, <-done, "the shell was signalled")
}

// eventLog records event scripts and game starts in the order they happen,
// with the ES service commands run before each.
type eventLog struct {
	cmd    *mocks.MockCommandExecutor
	events []string
}

func (l *eventLog) add(event string) {
	l.events = append(l.events, event)
}

func setupEventPlatform(t *testing.T) (*Platform, *eventLog, afero.Fs) {
	t.Helper()
	fs := newTestFs(t)
	writeFile(t, fs, testROM, "rom")
	writeFile(t, fs, filepath.Join(homeStartDir, "start.sh"), "")
	writeFile(t, fs, filepath.Join(homeEndDir, "end.sh"), "")
	cmd := newMockCmd()
	p := newTestPlatform(t, fs, cmd)
	log := &eventLog{cmd: cmd}
	p.runScript = func(_ context.Context, spec *scriptSpec) error {
		log.add(filepath.Base(spec.path) + " " + spec.args[0] + "|" + spec.args[1] +
			" after " + strings.Join(cmdCalls(cmd), ","))
		return nil
	}
	return p, log, fs
}

func TestLaunchGame_FiresGameEvents(t *testing.T) {
	t.Parallel()

	p, log, _ := setupEventPlatform(t)
	// The process is never started or tracked: Base's wait fails at once and
	// the platform still handles the game's exit.
	game := &os.Process{Pid: 4242}
	p.startGame = func(*launchSpec) (*os.Process, error) {
		log.add("game")
		return game, nil
	}

	_, err := p.launchGame(nil, testROM, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		`start.sh /roms/snes/Game\ \(USA\).sfc|Game (USA) after ` + esStop,
		"game",
	}, log.events, "game-start runs once ES is stopped and before the game")

	require.Error(t, p.WaitTrackedProcess(game))
	assert.Equal(t, "end.sh | after "+esStop, log.events[2], "game-end runs before ES comes back")
	assert.Len(t, log.events, 3)
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(log.cmd))

	p.endGame(0)
	assert.Len(t, log.events, 3, "game-end runs once")
}

func TestLaunchGame_StartFailureFiresGameEnd(t *testing.T) {
	t.Parallel()

	p, log, _ := setupEventPlatform(t)
	p.startGame = func(*launchSpec) (*os.Process, error) {
		log.add("game")
		return nil, errors.New("exec failed")
	}

	_, err := p.launchGame(nil, testROM, nil)
	require.Error(t, err)
	assert.Equal(t, []string{
		`start.sh /roms/snes/Game\ \(USA\).sfc|Game (USA) after ` + esStop,
		"game",
		"end.sh | after " + esStop,
	}, log.events)
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(log.cmd))

	p.endGame(0)
	assert.Len(t, log.events, 3, "nothing is left pending")
}

func TestLaunchGame_ScriptFailureDoesNotBlockLaunch(t *testing.T) {
	t.Parallel()

	p, log, _ := setupEventPlatform(t)
	p.runScript = func(context.Context, *scriptSpec) error { return errors.New("exit status 1") }
	self, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	p.startGame = func(*launchSpec) (*os.Process, error) {
		log.add("game")
		return self, nil
	}

	proc, err := p.launchGame(nil, testROM, nil)
	require.NoError(t, err)
	assert.Same(t, self, proc)
	assert.Equal(t, []string{"game"}, log.events)
}

func TestLaunchGame_ReplacementEndsPendingGameFirst(t *testing.T) {
	t.Parallel()

	p, log, _ := setupEventPlatform(t)
	pids := []int{0}
	p.startGame = func(*launchSpec) (*os.Process, error) {
		pids = append(pids, len(pids)+1000)
		log.add("game")
		return &os.Process{Pid: pids[len(pids)-1]}, nil
	}

	_, err := p.launchGame(nil, testROM, nil)
	require.NoError(t, err)
	p.endGame(424242)
	assert.Len(t, log.events, 2, "another process's exit does not end the game")

	_, err = p.launchGame(nil, testROM, nil)
	require.NoError(t, err)
	start := `start.sh /roms/snes/Game\ \(USA\).sfc|Game (USA) after ` + esStop
	assert.Equal(t, []string{start, "game", "end.sh | after " + esStop, start, "game"}, log.events)

	p.endGame(pids[1])
	assert.Len(t, log.events, 5, "the replaced game's late exit does not end the new one")
	p.endGame(pids[2])
	assert.Equal(t, "end.sh | after "+esStop, log.events[5])
}

func TestStop_FiresPendingGameEnd(t *testing.T) {
	t.Parallel()

	p, log, _ := setupEventPlatform(t)
	p.pendingEnd = &pendingGameEnd{acct: testAccount(), pid: 1234}
	require.NoError(t, p.Stop())
	assert.Equal(t, []string{"end.sh | after "}, log.events)
}
