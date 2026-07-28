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

func writeRecentEntry(t *testing.T, filename, directory, name string) {
	t.Helper()
	entry := make([]byte, 1024+256+256)
	copy(entry[:1024], directory)
	copy(entry[1024:1280], name)
	copy(entry[1280:], name)
	require.NoError(t, os.WriteFile(filename, entry, 0o600))
}

func TestSelectedFullPath(t *testing.T) {
	t.Parallel()

	selectedPath := filepath.Join(misterconfig.SDRootDir, "games", "SNES", "Game.sfc")
	t.Run("selected", func(t *testing.T) {
		path, err := selectedFullPath(
			[]byte("selected\n"),
			[]byte(" "+selectedPath+"\n"),
		)
		require.NoError(t, err)
		assert.Equal(t, selectedPath, path)
	})

	for _, status := range []string{"active", "cancelled", ""} {
		t.Run("ignores "+status, func(t *testing.T) {
			path, err := selectedFullPath([]byte(status), []byte(selectedPath))
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
	writeRecentEntry(t, recentFile, gameDir, "Game.gbc")

	path, err := recentGamePath(recentFile, []byte{0, 0, 0, 0})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(misterconfig.SDRootDir, gameDir, "Game.gbc"), path)
}

func TestRecentGamePath_CoreRecents(t *testing.T) {
	t.Parallel()

	t.Run("MGL resolves launched game", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		gamePath := filepath.Join(tmpDir, "games", "Game.rom")
		mglPath := filepath.Join(tmpDir, "Game.MGL")
		mglData := []byte(`<mistergamedescription><file path="` + gamePath + `"/></mistergamedescription>`)
		require.NoError(t, os.WriteFile(mglPath, mglData, 0o600))
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, filepath.Base(mglPath))

		path, err := recentGamePath(recentFile, nil)
		require.NoError(t, err)
		assert.Equal(t, gamePath, path)
	})

	t.Run("non-MGL core is ignored", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, "SNES.rbf")

		path, err := recentGamePath(recentFile, nil)
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("malformed MGL returns error", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		mglPath := filepath.Join(tmpDir, "Broken.mgl")
		require.NoError(t, os.WriteFile(mglPath, []byte(`<mistergamedescription>`), 0o600))
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, filepath.Base(mglPath))

		_, err := recentGamePath(recentFile, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error reading mgl file")
	})
}

func TestRecentGamePath_EmptyRecentFile(t *testing.T) {
	t.Parallel()

	recentFile := filepath.Join(t.TempDir(), "empty_recent.cfg")
	require.NoError(t, os.WriteFile(recentFile, nil, 0o600))

	path, err := recentGamePath(recentFile, nil)
	require.NoError(t, err)
	assert.Empty(t, path)
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

func TestTrackerRecentFileChanged(t *testing.T) {
	t.Parallel()

	assert.True(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "GBC_recent_0.cfg"),
	))
	assert.True(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "cores_recent.cfg"),
	))
	assert.False(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "device.bin"),
	))
	assert.False(t, trackerRecentFileChanged(
		filepath.Join(filepath.Dir(misterconfig.CoreConfigFolder), "config-backup", "GBC_recent_0.cfg"),
	))
}

func TestDispatchTrackerFileLoad(t *testing.T) {
	t.Parallel()

	settled := make(chan time.Time)
	loaded := make(chan struct{})
	dispatchTrackerFileLoad(settled, func() { close(loaded) })

	select {
	case <-loaded:
		t.Fatal("file load ran before settle signal")
	default:
	}

	settled <- time.Now()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("file load did not run after settle signal")
	}
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
