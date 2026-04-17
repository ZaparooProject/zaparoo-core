//go:build linux

package replayos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
