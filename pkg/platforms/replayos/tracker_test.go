//go:build linux

package replayos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// writeProcExe creates a fake /proc/{pid}/exe symlink pointing to target.
func writeProcExe(t *testing.T, procDir string, pid int, target string) {
	t.Helper()
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "exe")))
}

// writeProcChildren creates a fake /proc/{pid}/task/{pid}/children file.
func writeProcChildren(t *testing.T, procDir string, pid int, childPIDs []int) {
	t.Helper()
	taskDir := filepath.Join(procDir, strconv.Itoa(pid), "task", strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(taskDir, 0o750))
	content := ""
	for _, c := range childPIDs {
		if content != "" {
			content += " "
		}
		content += strconv.Itoa(c)
	}
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "children"), []byte(content), 0o600))
}

// writeProcMaps creates a fake /proc/{pid}/maps file.
func writeProcMaps(t *testing.T, procDir string, pid int, content string) {
	t.Helper()
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "maps"), []byte(content), 0o600))
}

func TestParseRecFile(t *testing.T) {
	t.Parallel()

	t.Run("standard /roms/ prefix", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "game.rec")
		require.NoError(t, os.WriteFile(recPath, []byte("/roms/nintendo_snes/game.sfc\n"), 0o600))

		got, err := parseRecFile("/media/sd", recPath)
		require.NoError(t, err)
		assert.Equal(t, "/media/sd/roms/nintendo_snes/game.sfc", got)
	})

	t.Run("path without /roms/ prefix joined directly", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "game.rec")
		require.NoError(t, os.WriteFile(recPath, []byte("/other/path/game.sfc\n"), 0o600))

		got, err := parseRecFile("/media/sd", recPath)
		require.NoError(t, err)
		assert.Equal(t, "/media/sd/other/path/game.sfc", got)
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "game.rec")
		require.NoError(t, os.WriteFile(recPath, []byte("  /roms/sega_smd/sonic.md  \n"), 0o600))

		got, err := parseRecFile("/media/sd", recPath)
		require.NoError(t, err)
		assert.Equal(t, "/media/sd/roms/sega_smd/sonic.md", got)
	})

	t.Run("empty file returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "empty.rec")
		require.NoError(t, os.WriteFile(recPath, []byte(""), 0o600))

		_, err := parseRecFile("/media/sd", recPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty rec file")
	})

	t.Run("whitespace-only file returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "ws.rec")
		require.NoError(t, os.WriteFile(recPath, []byte("   \n"), 0o600))

		_, err := parseRecFile("/media/sd", recPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty rec file")
	})

	t.Run("multi-line file uses only first line", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		recPath := filepath.Join(dir, "game.rec")
		require.NoError(t, os.WriteFile(recPath, []byte("/roms/nintendo_snes/game.sfc\nextra line\n"), 0o600))

		got, err := parseRecFile("/media/sd", recPath)
		require.NoError(t, err)
		assert.Equal(t, "/media/sd/roms/nintendo_snes/game.sfc", got)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		t.Parallel()
		_, err := parseRecFile("/media/sd", "/nonexistent/game.rec")
		require.Error(t, err)
	})
}

func TestSystemIDFromROMPath(t *testing.T) {
	t.Parallel()

	storage := "/media/sd"

	t.Run("known system folder", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/media/sd/roms/nintendo_snes/game.sfc", storage)
		assert.Equal(t, systemdefs.SystemSNES, got)
	})

	t.Run("another known system", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/media/sd/roms/sega_smd/sonic.md", storage)
		assert.Equal(t, systemdefs.SystemGenesis, got)
	})

	t.Run("unknown folder returns empty", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/media/sd/roms/unknown_system/game.rom", storage)
		assert.Empty(t, got)
	})

	t.Run("path not under storage returns empty", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/home/user/game.sfc", storage)
		assert.Empty(t, got)
	})

	t.Run("empty romPath returns empty", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("", storage)
		assert.Empty(t, got)
	})

	t.Run("empty storagePath returns empty", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/media/sd/roms/nintendo_snes/game.sfc", "")
		assert.Empty(t, got)
	})

	t.Run("arcade launcher ID differs from system ID", func(t *testing.T) {
		t.Parallel()
		got := systemIDFromROMPath("/media/sd/roms/arcade_fbneo/game.zip", storage)
		assert.Equal(t, systemdefs.SystemArcade, got)
	})
}

func TestIsReplayBinary(t *testing.T) {
	t.Parallel()

	t.Run("returns true when exe matches replayBinaryPath", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcExe(t, procDir, 1234, replayBinaryPath)
		p := &Platform{procPath: procDir}
		assert.True(t, p.isReplayBinary(1234))
	})

	t.Run("returns false when exe differs", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcExe(t, procDir, 5678, "/usr/bin/bash")
		p := &Platform{procPath: procDir}
		assert.False(t, p.isReplayBinary(5678))
	})

	t.Run("returns false when exe link absent", func(t *testing.T) {
		t.Parallel()
		p := &Platform{procPath: t.TempDir()}
		assert.False(t, p.isReplayBinary(9999))
	})
}

func TestFindReplayChild(t *testing.T) {
	t.Parallel()

	t.Run("returns first child whose exe is replay binary", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcChildren(t, procDir, 100, []int{200, 300})
		writeProcExe(t, procDir, 200, "/usr/bin/bash")
		writeProcExe(t, procDir, 300, replayBinaryPath)

		p := &Platform{procPath: procDir}
		pid, err := p.findReplayChild(100)
		require.NoError(t, err)
		assert.Equal(t, 300, pid)
	})

	t.Run("error when no child matches", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcChildren(t, procDir, 100, []int{200})
		writeProcExe(t, procDir, 200, "/usr/bin/bash")

		p := &Platform{procPath: procDir}
		_, err := p.findReplayChild(100)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "replay binary not found")
	})

	t.Run("error when children file absent", func(t *testing.T) {
		t.Parallel()
		p := &Platform{procPath: t.TempDir()}
		_, err := p.findReplayChild(100)
		require.Error(t, err)
	})
}

func TestGetLoadedCore(t *testing.T) {
	t.Parallel()

	makeMapsLine := func(path string) string {
		return fmt.Sprintf("7f1234567000-7f1234568000 r-xp 00000000 08:01 12345 %s\n", path)
	}

	t.Run("returns core from maps file", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		corePath := coresDir + "/snes9x_libretro.so"
		writeProcMaps(t, procDir, 42, makeMapsLine(corePath))

		p := &Platform{procPath: procDir}
		assert.Equal(t, "snes9x_libretro.so", p.getLoadedCore(42))
	})

	t.Run("skips menu core", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		menuPath := coresDir + "/" + menuCore
		writeProcMaps(t, procDir, 42, makeMapsLine(menuPath))

		p := &Platform{procPath: procDir}
		assert.Empty(t, p.getLoadedCore(42))
	})

	t.Run("ignores non-coresDir lines", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcMaps(t, procDir, 42, makeMapsLine("/usr/lib/libfoo.so"))

		p := &Platform{procPath: procDir}
		assert.Empty(t, p.getLoadedCore(42))
	})

	t.Run("returns empty when maps absent", func(t *testing.T) {
		t.Parallel()
		p := &Platform{procPath: t.TempDir()}
		assert.Empty(t, p.getLoadedCore(99))
	})
}

func TestGetReplayPID(t *testing.T) {
	t.Parallel()

	systemctlArgs := []string{"show", "-p", "MainPID", "--value", "replay.service"}

	t.Run("empty output returns error", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte(""), nil)
		p := &Platform{cmd: cmd}
		_, err := p.getReplayPID()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})

	t.Run("PID=0 returns error", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("0\n"), nil)
		p := &Platform{cmd: cmd}
		_, err := p.getReplayPID()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})

	t.Run("non-numeric PID returns error", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("abc\n"), nil)
		p := &Platform{cmd: cmd}
		_, err := p.getReplayPID()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid PID")
	})

	t.Run("MainPID is replay binary — returned directly", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcExe(t, procDir, 555, replayBinaryPath)

		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("555\n"), nil)
		p := &Platform{cmd: cmd, procPath: procDir}
		pid, err := p.getReplayPID()
		require.NoError(t, err)
		assert.Equal(t, 555, pid)
	})

	t.Run("MainPID is launcher, child is replay binary", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		writeProcExe(t, procDir, 100, "/usr/bin/bash")
		writeProcChildren(t, procDir, 100, []int{200})
		writeProcExe(t, procDir, 200, replayBinaryPath)

		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("100\n"), nil)
		p := &Platform{cmd: cmd, procPath: procDir}
		pid, err := p.getReplayPID()
		require.NoError(t, err)
		assert.Equal(t, 200, pid)
	})

	t.Run("systemctl command error", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return(nil, errors.New("exec error"))
		p := &Platform{cmd: cmd}
		_, err := p.getReplayPID()
		require.Error(t, err)
	})
}

func TestCheckAndUpdateRunningGame(t *testing.T) {
	t.Parallel()

	systemctlArgs := []string{"show", "-p", "MainPID", "--value", "replay.service"}

	t.Run("service not running, no prior game — no media update", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("0\n"), nil)

		callCount := 0
		p := &Platform{cmd: cmd, procPath: t.TempDir()}
		p.checkAndUpdateRunningGame(func(_ *models.ActiveMedia) { callCount++ })
		assert.Equal(t, 0, callCount, "setActiveMedia must not be called when no game was running")
	})

	t.Run("service stops after game was running — clears media", func(t *testing.T) {
		t.Parallel()
		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("0\n"), nil)

		var received *models.ActiveMedia
		p := &Platform{cmd: cmd, procPath: t.TempDir()}
		p.trackerMu.Lock()
		p.lastKnownCore = "snes9x_libretro.so"
		p.trackerMu.Unlock()

		p.checkAndUpdateRunningGame(func(m *models.ActiveMedia) { received = m })
		assert.Nil(t, received, "nil must be passed when game exits")
	})

	t.Run("game starts — media populated from core map", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		corePath := coresDir + "/snes9x_libretro.so"
		writeProcExe(t, procDir, 42, replayBinaryPath)
		writeProcMaps(t, procDir, 42, fmt.Sprintf(
			"7f0000000000-7f0000001000 r-xp 00000000 08:01 1 %s\n", corePath,
		))

		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("42\n"), nil)

		var received *models.ActiveMedia
		p := &Platform{cmd: cmd, procPath: procDir, activeStorage: "/media/sd"}
		p.checkAndUpdateRunningGame(func(m *models.ActiveMedia) { received = m })

		require.NotNil(t, received)
		assert.Equal(t, systemdefs.SystemSNES, received.SystemID)
	})

	t.Run("ROM path used to determine system when set", func(t *testing.T) {
		t.Parallel()
		procDir := t.TempDir()
		corePath := coresDir + "/gambatte_libretro.so" // gambatte = GB, but ROM path says GBC
		writeProcExe(t, procDir, 77, replayBinaryPath)
		writeProcMaps(t, procDir, 77, fmt.Sprintf(
			"7f0000000000-7f0000001000 r-xp 00000000 08:01 1 %s\n", corePath,
		))

		cmd := helpers.NewMockCommandExecutor()
		cmd.ExpectedCalls = nil
		cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("77\n"), nil)

		storage := t.TempDir()
		romPath := filepath.Join(storage, "roms", "nintendo_gbc", "game.gbc")

		var received *models.ActiveMedia
		p := &Platform{cmd: cmd, procPath: procDir, activeStorage: storage}
		p.trackerMu.Lock()
		p.pendingROMPath = romPath
		p.trackerMu.Unlock()

		p.checkAndUpdateRunningGame(func(m *models.ActiveMedia) { received = m })

		require.NotNil(t, received)
		assert.Equal(t, systemdefs.SystemGameboyColor, received.SystemID,
			"ROM path system ID must take precedence over core map")
	})
}

func TestStartGameTracker_ShutdownClean(t *testing.T) {
	t.Parallel()

	systemctlArgs := []string{"show", "-p", "MainPID", "--value", "replay.service"}

	// Service always appears not running — tracker ticks but makes no state changes.
	cmd := helpers.NewMockCommandExecutor()
	cmd.ExpectedCalls = nil
	cmd.On("Output", mock.Anything, "systemctl", systemctlArgs).Return([]byte("0\n"), nil).Maybe()

	fakeClock := clockwork.NewFakeClock()
	dir := t.TempDir()
	p := &Platform{
		cmd:           cmd,
		clock:         fakeClock,
		procPath:      dir,
		activeStorage: dir,
	}

	stopFn, err := p.startGameTracker(func(_ *models.ActiveMedia) {})
	require.NoError(t, err)
	require.NotNil(t, stopFn)

	// Advance clock to trigger one ticker cycle.
	fakeClock.Advance(trackerInterval)
	time.Sleep(50 * time.Millisecond)

	// Stop the tracker — must return without deadlock.
	done := make(chan error, 1)
	go func() { done <- stopFn() }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("stopFn did not return within 2 seconds")
	}
}

func TestStartRecentWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := &Platform{
		activeStorage: dir,
		ctx:           context.Background(),
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	watcher, done, err := p.startRecentWatcher()
	require.NoError(t, err)
	require.NotNil(t, watcher)

	// Write a .rec file into the watched directory.
	recentDir := filepath.Join(dir, "roms", recFileDir)
	recPath := filepath.Join(recentDir, "game.rec")
	require.NoError(t, os.WriteFile(recPath, []byte("/roms/nintendo_snes/game.sfc\n"), 0o600))

	expectedPath := filepath.Join(dir, "roms", "nintendo_snes", "game.sfc")
	require.Eventually(t, func() bool {
		p.trackerMu.RLock()
		got := p.pendingROMPath
		p.trackerMu.RUnlock()
		return got == expectedPath
	}, 2*time.Second, 10*time.Millisecond, "pendingROMPath not set after .rec write")

	require.NoError(t, watcher.Close())
	<-done
}
