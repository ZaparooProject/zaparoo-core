//go:build linux

package tracker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectedFullPath(t *testing.T) {
	t.Parallel()

	t.Run("selected", func(t *testing.T) {
		path, err := selectedFullPath(
			[]byte("selected\n"),
			[]byte(" /media/fat/games/SNES/Game.sfc\n"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/media/fat/games/SNES/Game.sfc", path)
	})

	for _, status := range []string{"active", "cancelled", ""} {
		t.Run("ignores "+status, func(t *testing.T) {
			path, err := selectedFullPath([]byte(status), []byte("/media/fat/games/SNES/Game.sfc"))
			require.NoError(t, err)
			assert.Empty(t, path)
		})
	}

	t.Run("selected requires path", func(t *testing.T) {
		_, err := selectedFullPath([]byte("selected"), nil)
		require.Error(t, err)
	})
}

func TestResolveRecentGamePath(t *testing.T) {
	t.Parallel()

	relativePath := filepath.Join("games", "GBC", "Game.gbc")
	assert.Equal(t,
		filepath.Join(misterconfig.SDRootDir, relativePath),
		resolveRecentGamePath(relativePath, nil, func(string) bool { return false }),
	)

	usb1Path := filepath.Join(filepath.Dir(misterconfig.SDRootDir), "usb1", relativePath)
	assert.Equal(t, usb1Path, resolveRecentGamePath(
		relativePath,
		[]byte{1, 0, 0, 0},
		func(path string) bool { return path == usb1Path },
	))

	absolutePath := filepath.Join(string(filepath.Separator), "media", "network", "Game.gbc")
	assert.Equal(t,
		absolutePath,
		resolveRecentGamePath(absolutePath, []byte{1, 0, 0, 0}, func(string) bool { return false }),
	)
}

func TestRecentGamePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	gameDir := filepath.Join("games", "GBC")
	recentFile := filepath.Join(tmpDir, "GBC_recent_0.cfg")
	entry := make([]byte, 1024+256+256)
	copy(entry[:1024], gameDir)
	copy(entry[1024:1280], "Game.gbc")
	copy(entry[1280:], "Game")
	require.NoError(t, os.WriteFile(recentFile, entry, 0o600))

	path, err := recentGamePath(recentFile)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(misterconfig.SDRootDir, gameDir, "Game.gbc"), path)
}

func TestTrackerFileChanged(t *testing.T) {
	t.Parallel()

	assert.True(t, trackerFileChanged(fsnotify.Write))
	assert.True(t, trackerFileChanged(fsnotify.Create))
	assert.True(t, trackerFileChanged(fsnotify.Rename))
	assert.True(t, trackerFileChanged(fsnotify.Write|fsnotify.Chmod))
	assert.False(t, trackerFileChanged(fsnotify.Chmod))
	assert.False(t, trackerFileChanged(fsnotify.Remove))
}

func TestMediaLookupContextUsesServiceContext(t *testing.T) {
	t.Parallel()

	serviceCtx, cancelService := context.WithCancel(context.Background())
	tr := &Tracker{serviceCtx: serviceCtx}

	ctx, cancelLookup := tr.mediaLookupContext()
	defer cancelLookup()

	select {
	case <-ctx.Done():
		t.Fatal("MediaDB lookup context should not be canceled before service context")
	default:
	}

	cancelService()
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("MediaDB lookup context should follow service context")
	}
}
