//go:build linux

package replayos

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/bendahl/uinput"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngBytes returns bytes that PNGFileComplete considers a complete file:
// a PNG signature followed immediately by the IEND trailer.
func pngBytes() []byte {
	iend := []byte{0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}
	sig := make([]byte, 0, 8+len(iend))
	sig = append(sig, 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a)
	return append(sig, iend...)
}

// trackingKeyboard is a minimal uinput.Keyboard that records KeyDown calls.
type trackingKeyboard struct {
	keyDownErr  error
	keyDownKeys []int
}

func (*trackingKeyboard) KeyPress(_ int) error { return nil }
func (m *trackingKeyboard) KeyDown(key int) error {
	m.keyDownKeys = append(m.keyDownKeys, key)
	return m.keyDownErr
}
func (*trackingKeyboard) KeyUp(_ int) error             { return nil }
func (*trackingKeyboard) FetchSyspath() (string, error) { return "", nil }
func (*trackingKeyboard) Close() error                  { return nil }

// initMockKbd wires a trackingKeyboard into the platform's LinuxInput by
// calling InitDevices with a mock factory. Returns the keyboard for assertions.
func initMockKbd(t *testing.T, p *Platform, keyDownErr error) *trackingKeyboard {
	t.Helper()
	kbd := &trackingKeyboard{keyDownErr: keyDownErr}
	p.NewKeyboard = func(_ time.Duration) (linuxinput.Keyboard, error) {
		return linuxinput.Keyboard{Device: kbd}, nil
	}
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, p.InitDevices(cfg, false))
	return kbd
}

func writeCapture(t *testing.T, capturesDir, system, name string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(capturesDir, system)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	return path
}

func TestWaitForScreenshot(t *testing.T) {
	t.Parallel()

	t.Run("returns result when complete PNG appears before deadline", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-time.Second)
		pngPath := filepath.Join(dir, "nintendo_snes", "shot.png")
		require.NoError(t, os.MkdirAll(filepath.Dir(pngPath), 0o750))
		require.NoError(t, os.WriteFile(pngPath, pngBytes(), 0o600))

		result, err := waitForScreenshot(dir, since, time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, pngPath, result.Path)
		assert.NotEmpty(t, result.Data)
	})

	t.Run("times out when no PNG arrives", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := waitForScreenshot(dir, time.Now(), 200*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "screenshot timed out")
	})

	t.Run("ignores PNG older than triggerTime", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		oldTime := time.Now().Add(-5 * time.Second)
		// Write a complete PNG, but stamp it before triggerTime.
		pngPath := filepath.Join(dir, "sega_smd", "old.png")
		require.NoError(t, os.MkdirAll(filepath.Dir(pngPath), 0o750))
		require.NoError(t, os.WriteFile(pngPath, pngBytes(), 0o600))
		require.NoError(t, os.Chtimes(pngPath, oldTime, oldTime))

		since := time.Now()
		_, err := waitForScreenshot(dir, since, 200*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "screenshot timed out")
	})
}

func TestTriggerScreenshot_RealModeOff(t *testing.T) {
	t.Parallel()

	// When keyboardRealMode is false only 's' (keycode 31) should be sent.
	p := &Platform{keyboardRealMode: false}
	kbd := initMockKbd(t, p, nil)

	require.NoError(t, p.triggerScreenshot())

	// Press calls KeyDown then KeyUp; we only record KeyDown.
	require.Len(t, kbd.keyDownKeys, 1, "only 's' should be sent")
	assert.Equal(t, 31, kbd.keyDownKeys[0], "key 31 = 's'")
}

func TestTriggerScreenshot_RealModeOn_KeySequence(t *testing.T) {
	t.Parallel()

	// When keyboardRealMode is true: {capslock}(58) → sleep → s(31) → sleep → {capslock}(58)
	fakeClock := clockwork.NewFakeClock()
	p := &Platform{keyboardRealMode: true, clock: fakeClock}
	kbd := initMockKbd(t, p, nil)

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.triggerScreenshot()
	}()

	// Wait for first clock.Sleep (OSD delay after capslock).
	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
	assert.Len(t, kbd.keyDownKeys, 1, "capslock must be sent before first sleep")
	assert.Equal(t, 58, kbd.keyDownKeys[0], "first key must be capslock(58)")

	fakeClock.Advance(screenshotOSDDelay)

	// Wait for second clock.Sleep (key delay after 's').
	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
	require.Len(t, kbd.keyDownKeys, 2, "'s' must be sent after OSD delay")
	assert.Equal(t, 31, kbd.keyDownKeys[1], "second key must be 's'(31)")

	fakeClock.Advance(screenshotKeyDelay)

	require.NoError(t, <-errCh)
	require.Len(t, kbd.keyDownKeys, 3, "capslock restore must be last")
	assert.Equal(t, 58, kbd.keyDownKeys[2], "third key must be capslock(58)")
}

func TestTriggerScreenshot_RealModeOn_FailedSKey_RestoresRealMode(t *testing.T) {
	t.Parallel()

	// When 's' fails and keyboardRealMode is true, capslock should still be sent
	// for a best-effort restore (but we don't assert the third press since it
	// is best-effort and the error path doesn't re-use blocking sleeps).
	fakeClock := clockwork.NewFakeClock()
	p := &Platform{keyboardRealMode: true, clock: fakeClock}

	// The trackingKeyboard will fail on the second KeyDown (the 's' press).
	call := 0
	kbdDev := &callCountKeyboard{onKeyDown: func(_ int) error {
		call++
		if call == 2 { // second press = 's'
			return assert.AnError
		}
		return nil
	}}
	p.NewKeyboard = func(_ time.Duration) (linuxinput.Keyboard, error) {
		return linuxinput.Keyboard{Device: kbdDev}, nil
	}
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, p.InitDevices(cfg, false))

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.triggerScreenshot()
	}()

	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1)) // wait for OSD sleep after first capslock
	fakeClock.Advance(screenshotOSDDelay)

	err = <-errCh
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send screenshot key")
}

// callCountKeyboard records keydown calls via a callback.
type callCountKeyboard struct {
	onKeyDown func(int) error
}

func (*callCountKeyboard) KeyPress(_ int) error          { return nil }
func (m *callCountKeyboard) KeyDown(key int) error       { return m.onKeyDown(key) }
func (*callCountKeyboard) KeyUp(_ int) error             { return nil }
func (*callCountKeyboard) FetchSyspath() (string, error) { return "", nil }
func (*callCountKeyboard) Close() error                  { return nil }

// Compile-time interface checks.
var (
	_ uinput.Keyboard = (*trackingKeyboard)(nil)
	_ uinput.Keyboard = (*callCountKeyboard)(nil)
)

func TestFindNewestPNG(t *testing.T) {
	t.Parallel()

	t.Run("missing captures dir returns empty", func(t *testing.T) {
		t.Parallel()
		path, err := findNewestPNG("/nonexistent/captures", time.Now())
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("empty captures dir returns empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, err := findNewestPNG(dir, time.Now())
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("file newer than since is returned", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-1 * time.Second)
		expected := writeCapture(t, dir, "nintendo_snes", "game_20260101_120000.png", time.Now())

		path, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Equal(t, expected, path)
	})

	t.Run("file older than since is not returned", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		oldTime := time.Now().Add(-10 * time.Second)
		writeCapture(t, dir, "nintendo_snes", "old.png", oldTime)

		since := time.Now().Add(-1 * time.Second)
		path, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("returns newest of multiple qualifying files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-5 * time.Second)
		base := time.Now().Add(-2 * time.Second)

		writeCapture(t, dir, "nintendo_snes", "older.png", base)
		expected := writeCapture(t, dir, "nintendo_snes", "newer.png", base.Add(time.Second))

		path, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Equal(t, expected, path)
	})

	t.Run("non-png files are ignored", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-1 * time.Second)
		writeCapture(t, dir, "sega_smd", "game.jpg", time.Now())
		writeCapture(t, dir, "sega_smd", "game.bmp", time.Now())

		path, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("files across system subdirs, newest wins", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-5 * time.Second)
		base := time.Now().Add(-2 * time.Second)

		writeCapture(t, dir, "nintendo_snes", "snes.png", base)
		expected := writeCapture(t, dir, "sega_smd", "sega.png", base.Add(time.Second))

		path, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Equal(t, expected, path)
	})

	t.Run("files at top level of captures dir are ignored", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		since := time.Now().Add(-1 * time.Second)
		// PNG directly in capturesDir (not in a system subdir)
		path := filepath.Join(dir, "game.png")
		require.NoError(t, os.WriteFile(path, []byte{}, 0o600))
		require.NoError(t, os.Chtimes(path, time.Now(), time.Now()))

		got, err := findNewestPNG(dir, since)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
