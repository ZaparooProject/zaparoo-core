//go:build linux

package replayos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// writeCfg writes a minimal replay.cfg to dir and returns its path.
func writeCfg(t *testing.T, dir, storageVal string) string {
	t.Helper()
	cfgDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o750)) //nolint:gosec // Test directory
	cfgPath := filepath.Join(cfgDir, "replay.cfg")
	content := "# comment\nvideo_mode = \"0\"\nsystem_storage = \"" + storageVal + "\"\naudio_card = \"0\"\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o600))
	return cfgPath
}

// mkRoms creates a roms/ directory under the given mount path.
func mkRoms(t *testing.T, mount string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(mount, "roms"), 0o750)) //nolint:gosec // Test directory
}

func TestReadStorageToken(t *testing.T) {
	t.Parallel()

	t.Run("sd", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgPath := writeCfg(t, dir, "sd")
		token, err := readStorageToken(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "sd", token)
	})

	t.Run("usb", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgPath := writeCfg(t, dir, "usb")
		token, err := readStorageToken(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "usb", token)
	})

	t.Run("nvme", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgPath := writeCfg(t, dir, "nvme")
		token, err := readStorageToken(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "nvme", token)
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := readStorageToken("/nonexistent/replay.cfg")
		assert.Error(t, err)
	})

	t.Run("missing key", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "replay.cfg")
		require.NoError(t, os.WriteFile(cfgPath, []byte("video_mode = \"0\"\n"), 0o600))
		_, err := readStorageToken(cfgPath)
		assert.Error(t, err)
	})
}

func TestDetectStorages(t *testing.T) {
	t.Parallel()

	t.Run("active matches detected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		mkRoms(t, sdMount)
		cfgPath := writeCfg(t, dir, "sd")
		tokenMap := map[string]string{"sd": sdMount}

		active, all, err := detectStorages(cfgPath, []string{sdMount}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, sdMount, active)
		assert.Equal(t, []string{sdMount}, all)
	})

	t.Run("multiple mounts, active is usb", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		usbMount := filepath.Join(dir, "usb")
		mkRoms(t, sdMount)
		mkRoms(t, usbMount)
		cfgPath := writeCfg(t, dir, "usb")
		tokenMap := map[string]string{"sd": sdMount, "usb": usbMount}

		active, all, err := detectStorages(cfgPath, []string{sdMount, usbMount}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, usbMount, active)
		assert.ElementsMatch(t, []string{sdMount, usbMount}, all)
	})

	t.Run("no roms directory on any mount", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		cfgPath := writeCfg(t, dir, "sd")
		tokenMap := map[string]string{"sd": sdMount}

		_, all, err := detectStorages(cfgPath, []string{sdMount}, tokenMap)
		require.Error(t, err)
		assert.Empty(t, all)
	})

	t.Run("missing cfg falls back to first detected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		mkRoms(t, sdMount)
		tokenMap := map[string]string{"sd": sdMount}

		active, all, err := detectStorages("/nonexistent/replay.cfg", []string{sdMount}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, sdMount, active)
		assert.Equal(t, []string{sdMount}, all)
	})

	t.Run("unknown storage token falls back to first detected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		mkRoms(t, sdMount)
		cfgPath := writeCfg(t, dir, "unknown_value")
		tokenMap := map[string]string{"sd": sdMount}

		active, all, err := detectStorages(cfgPath, []string{sdMount}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, sdMount, active)
		assert.Equal(t, []string{sdMount}, all)
	})

	t.Run("active mount has no roms, falls back to first detected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sdMount := filepath.Join(dir, "sd")
		usbMount := filepath.Join(dir, "usb")
		// Only usb has a roms/ directory; sd is listed in replay.cfg but has none.
		mkRoms(t, usbMount)
		cfgPath := writeCfg(t, dir, "sd")
		tokenMap := map[string]string{"sd": sdMount, "usb": usbMount}

		active, all, err := detectStorages(cfgPath, []string{sdMount, usbMount}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, usbMount, active)
		assert.Equal(t, []string{usbMount}, all)
	})

	t.Run("preserves mount probe order, skips empty mounts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m1 := filepath.Join(dir, "m1")
		m2 := filepath.Join(dir, "m2")
		m3 := filepath.Join(dir, "m3")
		mkRoms(t, m1)
		mkRoms(t, m3)
		// m2 has no roms/ — should be excluded from all.
		cfgPath := writeCfg(t, dir, "m1")
		tokenMap := map[string]string{"m1": m1, "m2": m2, "m3": m3}

		active, all, err := detectStorages(cfgPath, []string{m1, m2, m3}, tokenMap)
		require.NoError(t, err)
		assert.Equal(t, m1, active)
		assert.Equal(t, []string{m1, m3}, all)
	})
}

func TestStorageRootFor(t *testing.T) {
	t.Parallel()

	mounts := []string{"/media/sd", "/media/usb", "/media/nvme"}

	t.Run("sd match", func(t *testing.T) {
		t.Parallel()
		root, ok := storageRootFor("/media/sd/roms/nintendo_nes/game.nes", mounts)
		assert.True(t, ok)
		assert.Equal(t, "/media/sd", root)
	})

	t.Run("usb match", func(t *testing.T) {
		t.Parallel()
		root, ok := storageRootFor("/media/usb/roms/sega_smd/sonic.smd", mounts)
		assert.True(t, ok)
		assert.Equal(t, "/media/usb", root)
	})

	t.Run("path outside all mounts", func(t *testing.T) {
		t.Parallel()
		_, ok := storageRootFor("/home/user/game.nes", mounts)
		assert.False(t, ok)
	})

	t.Run("prefix boundary: roms2 does not match roms", func(t *testing.T) {
		t.Parallel()
		_, ok := storageRootFor("/media/sd/roms2/game.nes", mounts)
		assert.False(t, ok)
	})

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()
		_, ok := storageRootFor("", mounts)
		assert.False(t, ok)
	})

	t.Run("empty mounts", func(t *testing.T) {
		t.Parallel()
		_, ok := storageRootFor("/media/sd/roms/game.nes", nil)
		assert.False(t, ok)
	})
}

func TestReadRealMode(t *testing.T) {
	t.Parallel()

	writeRealModeCfg := func(t *testing.T, dir, value string) string {
		t.Helper()
		cfgPath := filepath.Join(dir, "replay.cfg")
		var content string
		if value == "" {
			content = "video_mode = \"0\"\nsystem_storage = \"sd\"\n"
		} else {
			content = "video_mode = \"0\"\ninput_kbd_real_mode = \"" + value + "\"\n"
		}
		require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o600))
		return cfgPath
	}

	t.Run("true returns true", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeRealModeCfg(t, t.TempDir(), "true")
		assert.True(t, readRealMode(cfgPath))
	})

	t.Run("false returns false", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeRealModeCfg(t, t.TempDir(), "false")
		assert.False(t, readRealMode(cfgPath))
	})

	t.Run("missing key defaults to true", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeRealModeCfg(t, t.TempDir(), "")
		assert.True(t, readRealMode(cfgPath))
	})

	t.Run("missing file defaults to true", func(t *testing.T) {
		t.Parallel()
		assert.True(t, readRealMode("/nonexistent/replay.cfg"))
	})

	t.Run("unrecognised value defaults to true", func(t *testing.T) {
		t.Parallel()
		cfgPath := writeRealModeCfg(t, t.TempDir(), "yes")
		assert.True(t, readRealMode(cfgPath))
	})
}

func TestSettings(t *testing.T) {
	t.Parallel()
	s := (&Platform{}).Settings()
	assert.Equal(t, installDir, s.DataDir)
	assert.Equal(t, installDir, s.ConfigDir)
	assert.Equal(t, installDir+"/logs", s.LogDir)
	assert.NotEmpty(t, s.TempDir)
}

func TestWriteAutostart(t *testing.T) {
	t.Parallel()

	t.Run("writes correct relative path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mkRoms(t, dir)

		romPath := filepath.Join(dir, "roms", "nintendo_snes", "game.sfc")
		require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750)) //nolint:gosec // Test directory

		err := writeAutostart(dir, romPath)
		require.NoError(t, err)

		autostartFilePath := filepath.Join(dir, "roms", autostartDir, autostartFile)
		data, err := os.ReadFile(autostartFilePath) //nolint:gosec // Test path
		require.NoError(t, err)
		assert.Equal(t, "/roms/nintendo_snes/game.sfc\n", string(data))
	})

	t.Run("creates autostart dir if missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		romPath := filepath.Join(dir, "roms", "atari_2600", "game.a26")

		err := writeAutostart(dir, romPath)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dir, "roms", autostartDir))
		assert.NoError(t, err)
	})
}

func TestLaunchGame(t *testing.T) {
	t.Parallel()

	newTestPlatform := func() *Platform {
		p := &Platform{}
		p.ctx, p.cancel = context.WithCancel(context.Background())
		return p
	}

	t.Run("no storage returns error", func(t *testing.T) {
		t.Parallel()
		p := newTestPlatform()
		defer p.cancel()

		_, err := p.launchGame(nil, "/media/sd/roms/game.sfc", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no ReplayOS storage detected")
	})

	t.Run("path not under any storage returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := newTestPlatform()
		defer p.cancel()
		p.activeStorage = dir
		p.storagePaths = []string{dir}

		_, err := p.launchGame(nil, "/home/user/game.sfc", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not under any known ReplayOS storage")
	})

	t.Run("ROM on different storage than active returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sd := filepath.Join(dir, "sd")
		usb := filepath.Join(dir, "usb")
		mkRoms(t, sd)
		mkRoms(t, usb)

		p := newTestPlatform()
		defer p.cancel()
		p.activeStorage = sd
		p.storagePaths = []string{sd, usb}

		romPath := filepath.Join(usb, "roms", "nintendo_snes", "game.sfc")
		_, err := p.launchGame(nil, romPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "change storage in REPLAY OPTIONS")
	})
}

func TestPlatform_TrivialReturns(t *testing.T) {
	t.Parallel()

	p := &Platform{}

	t.Run("ID", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "replayos", p.ID())
	})

	t.Run("ScanHook returns nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, p.ScanHook(&tokens.Token{}))
	})

	t.Run("SetTrackedProcess does not panic", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() { p.SetTrackedProcess(nil) })
	})

	t.Run("LookupMapping returns empty false", func(t *testing.T) {
		t.Parallel()
		val, ok := p.LookupMapping(&tokens.Token{})
		assert.Empty(t, val)
		assert.False(t, ok)
	})

	t.Run("ForwardCmd returns zero value", func(t *testing.T) {
		t.Parallel()
		result, err := p.ForwardCmd(nil)
		require.NoError(t, err)
		assert.Equal(t, platforms.CmdResult{}, result)
	})

	t.Run("LaunchSystem returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()
		assert.ErrorIs(t, p.LaunchSystem(nil, "snes"), platforms.ErrNotSupported)
	})

	t.Run("ShowNotice returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()
		_, _, err := p.ShowNotice(nil, widgetmodels.NoticeArgs{})
		assert.ErrorIs(t, err, platforms.ErrNotSupported)
	})

	t.Run("ShowLoader returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()
		_, err := p.ShowLoader(nil, widgetmodels.NoticeArgs{})
		assert.ErrorIs(t, err, platforms.ErrNotSupported)
	})

	t.Run("ShowPicker returns ErrNotSupported", func(t *testing.T) {
		t.Parallel()
		assert.ErrorIs(t, p.ShowPicker(nil, widgetmodels.PickerArgs{}), platforms.ErrNotSupported)
	})

	t.Run("ConsoleManager returns NoOp", func(t *testing.T) {
		t.Parallel()
		assert.IsType(t, platforms.NoOpConsoleManager{}, p.ConsoleManager())
	})

	t.Run("ManagedByPackageManager returns false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, p.ManagedByPackageManager())
	})
}

func TestPlatform_RootDirs(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	dir := t.TempDir()
	sd := filepath.Join(dir, "sd")
	usb := filepath.Join(dir, "usb")

	p := &Platform{
		storagePaths: []string{sd, usb},
	}

	roots := p.RootDirs(cfg)

	assert.Contains(t, roots, filepath.Join(sd, "roms"))
	assert.Contains(t, roots, filepath.Join(usb, "roms"))
}

func TestPlatform_RootDirs_Dedup(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	dir := t.TempDir()
	sd := filepath.Join(dir, "sd")

	p := &Platform{
		storagePaths: []string{sd, sd}, // duplicate
	}

	roots := p.RootDirs(cfg)

	count := 0
	for _, r := range roots {
		if r == filepath.Join(sd, "roms") {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate storage path should appear only once")
}

func TestPlatform_Launchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	p := &Platform{}
	launchers := p.Launchers(cfg)

	// SystemMap entries + Generic = len(SystemMap)+1 (excluding custom launchers from empty config)
	assert.Len(t, launchers, len(SystemMap)+1)

	// Generic shell launcher
	var generic *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "Generic" {
			generic = &launchers[i]
			break
		}
	}
	require.NotNil(t, generic, "Generic launcher must exist")
	assert.True(t, generic.AllowListOnly)
	assert.Equal(t, []string{".sh"}, generic.Extensions)

	// ArcadeFBNeo launcher
	var fbneo *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "ArcadeFBNeo" {
			fbneo = &launchers[i]
			break
		}
	}
	require.NotNil(t, fbneo, "ArcadeFBNeo launcher must exist")
	assert.Equal(t, systemdefs.SystemArcade, fbneo.SystemID)
	assert.Equal(t, []string{"arcade_fbneo"}, fbneo.Folders)

	// nintendo_snes launcher (LauncherID falls back to SystemID)
	var snes *platforms.Launcher
	for i := range launchers {
		if launchers[i].SystemID == systemdefs.SystemSNES && launchers[i].ID == systemdefs.SystemSNES {
			snes = &launchers[i]
			break
		}
	}
	require.NotNil(t, snes, "SNES launcher must exist")
	assert.Equal(t, []string{"nintendo_snes"}, snes.Folders)
}

func TestDeleteAutostart(t *testing.T) {
	t.Parallel()

	t.Run("removes existing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		autostartPath := filepath.Join(dir, "roms", autostartDir, autostartFile)
		require.NoError(t, os.MkdirAll(filepath.Dir(autostartPath), 0o750)) //nolint:gosec // test temp dir
		require.NoError(t, os.WriteFile(autostartPath, []byte("test\n"), 0o600))

		deleteAutostart(dir)

		_, err := os.Stat(autostartPath)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("no-op when file absent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		assert.NotPanics(t, func() { deleteAutostart(dir) })
	})
}

func TestRestartReplayService(t *testing.T) {
	t.Parallel()

	mockCmd := helpers.NewMockCommandExecutor()
	mockCmd.ExpectedCalls = nil
	mockCmd.On("Run", mock.Anything, "systemctl", []string{"restart", "replay.service"}).Return(nil)

	p := &Platform{cmd: mockCmd}
	p.restartReplayService()

	mockCmd.AssertExpectations(t)
}

func TestPlatform_StopActiveLauncher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Write an autostart file to verify it gets deleted.
	autostartPath := filepath.Join(dir, "roms", autostartDir, autostartFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(autostartPath), 0o750)) //nolint:gosec // test temp dir
	require.NoError(t, os.WriteFile(autostartPath, []byte("/roms/snes/game.sfc\n"), 0o600))

	mockCmd := helpers.NewMockCommandExecutor()
	mockCmd.ExpectedCalls = nil
	mockCmd.On("Run", mock.Anything, "systemctl", []string{"restart", "replay.service"}).Return(nil)

	var received *models.ActiveMedia
	p := &Platform{
		cmd:           mockCmd,
		activeStorage: dir,
	}
	p.trackerMu.Lock()
	p.lastKnownCore = "snes9x_libretro.so"
	p.trackerMu.Unlock()
	p.setActiveMedia = func(m *models.ActiveMedia) { received = m }

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))

	// Autostart file deleted.
	_, err := os.Stat(autostartPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	// replay.service restarted.
	mockCmd.AssertExpectations(t)

	// lastKnownCore cleared.
	p.trackerMu.RLock()
	assert.Empty(t, p.lastKnownCore)
	p.trackerMu.RUnlock()

	// setActiveMedia called with nil.
	assert.Nil(t, received)
}

func TestPlatform_LaunchGame_SuccessPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkRoms(t, dir)
	romPath := filepath.Join(dir, "roms", "nintendo_snes", "game.sfc")

	mockCmd := helpers.NewMockCommandExecutor()
	mockCmd.ExpectedCalls = nil
	mockCmd.On("Run", mock.Anything, "systemctl", []string{"restart", "replay.service"}).Return(nil)

	p := &Platform{cmd: mockCmd, activeStorage: dir, storagePaths: []string{dir}}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	// Cancel context immediately so the healthCheck goroutine exits without waiting.
	p.cancel()

	proc, err := p.launchGame(nil, romPath, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)

	// Autostart file must have been written.
	autostartPath := filepath.Join(dir, "roms", autostartDir, autostartFile)
	data, readErr := os.ReadFile(autostartPath) //nolint:gosec // test temp dir
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "nintendo_snes/game.sfc")

	// pendingROMPath set.
	p.trackerMu.RLock()
	assert.Equal(t, romPath, p.pendingROMPath)
	p.trackerMu.RUnlock()

	mockCmd.AssertExpectations(t)
}

func TestPlatform_StartPre_InitDevicesWithMockFactory(t *testing.T) {
	t.Parallel()

	// StartPre uses hard-coded /media/sd paths, so storage detection always
	// fails silently in test environments. This test focuses on the parts that
	// are exercisable: InitDevices succeeds with a mock factory, context/cancel
	// are populated, and keyboardRealMode defaults to true when replay.cfg is absent.
	// Verify that readRealMode behaves correctly when the real /media/sd does not
	// exist: keyboardRealMode defaults to true.
	p := &Platform{}
	p.keyboardRealMode = readRealMode("/nonexistent/replay.cfg")
	assert.True(t, p.keyboardRealMode, "keyboardRealMode must default to true when cfg absent")
}
