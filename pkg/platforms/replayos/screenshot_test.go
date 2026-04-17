//go:build linux

package replayos

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCapture(t *testing.T, capturesDir, system, name string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(capturesDir, system)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	return path
}

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
