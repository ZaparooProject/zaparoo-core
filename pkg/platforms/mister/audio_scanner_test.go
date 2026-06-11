//go:build linux

package mister

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAudioScannerLauncher(t *testing.T) {
	t.Parallel()

	launcher := createAudioScannerLauncher()

	assert.Equal(t, misterAudioScannerLauncherID, launcher.ID)
	assert.Equal(t, systemdefs.SystemAudio, launcher.SystemID)
	assert.True(t, launcher.SkipFilesystemScan)
	assert.NotNil(t, launcher.Scanner)
	assert.Nil(t, launcher.Launch)
	assert.Empty(t, launcher.Extensions)
	assert.Empty(t, launcher.Folders)
}

func TestScanMiSTerAudioPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	musicDir := filepath.Join(root, "music")
	audioDir := filepath.Join(root, "games", "Audio")
	nestedDir := filepath.Join(audioDir, "Albums")
	require.NoError(t, os.MkdirAll(musicDir, 0o750))
	require.NoError(t, os.MkdirAll(nestedDir, 0o750))

	musicTrack := filepath.Join(musicDir, "track.mp3")
	audioTrack := filepath.Join(audioDir, "song.FLAC")
	nestedTrack := filepath.Join(nestedDir, "loop.ogg")
	ignoredFile := filepath.Join(audioDir, "cover.txt")
	for _, path := range []string{musicTrack, audioTrack, nestedTrack, ignoredFile} {
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))
	}

	results, err := scanMiSTerAudioPaths(context.Background(), []string{
		musicDir,
		filepath.Join(root, "missing"),
		audioDir,
		audioDir,
	})

	require.NoError(t, err)
	assert.Equal(t, []platforms.ScanResult{
		{Path: filepath.Join(audioDir, "Albums", "loop.ogg")},
		{Path: filepath.Join(audioDir, "song.FLAC")},
		{Path: filepath.Join(musicDir, "track.mp3")},
	}, results)
}

func TestScanMiSTerAudioPathsCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanMiSTerAudioPaths(ctx, []string{t.TempDir()})

	require.ErrorIs(t, err, context.Canceled)
}
