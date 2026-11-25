//go:build linux && !android

package mister

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withMediaCenterBinary points the launcher at path for one test.
func withMediaCenterBinary(t *testing.T, path string) {
	t.Helper()
	old := mediaCenterBinary
	mediaCenterBinary = path
	t.Cleanup(func() { mediaCenterBinary = old })
}

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("x"), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func TestBuildMediaCenterCommand(t *testing.T) {
	withMediaCenterBinary(t, "/media/fat/linux/mediacenter")
	path := "/media/fat/games/Video/Film (1999).mkv"

	cmd := buildMediaCenterCommand(context.Background(), path)

	assert.Equal(t, []string{"/media/fat/linux/mediacenter", path}, cmd.Args)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setsid)
	assert.NotNil(t, cmd.Cancel, "a cancelled launcher context must not SIGKILL the player")
	assert.Contains(t, cmd.Env, "MC_ARMSCALE=1",
		"the player must scale on the ARM instead of waiting on framebuffers Main does not provide")
}

// The player takes a framebuffer under 720 lines for a 15 kHz CRT mode and
// breaks its picture over HDMI, so the default keeps the native size.
func TestMediaCenterDefaultFramebufferIsNative(t *testing.T) {
	t.Parallel()

	mode, err := resolveFramebufferMode(nil, mediaCenterRenderScale, "")
	require.NoError(t, err)
	assert.Equal(t, framebufferMode{divisor: 1}, mode)
}

// A stop cancels the launcher context while the player closes on SIGTERM;
// the cancel must send SIGTERM too, not exec's default SIGKILL.
func TestTerminateOnCancelSendsSIGTERM(t *testing.T) {
	t.Parallel()

	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, sleepPath, "30") //nolint:gosec // G204: sleep from PATH, fixed argument
	terminateOnCancel(cmd)
	require.NoError(t, cmd.Start())

	cancel()
	waitErr := cmd.Wait()

	var exitErr *exec.ExitError
	require.ErrorAs(t, waitErr, &exitErr)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	require.True(t, status.Signaled())
	assert.Equal(t, syscall.SIGTERM, status.Signal())
}

func TestMediaCenterInstalled(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		setup func(path string)
		name  string
		want  bool
	}{
		{name: "missing", setup: func(string) {}},
		{name: "not executable", setup: func(p string) { writeFile(t, p, 0o600) }},
		{name: "directory", setup: func(p string) { require.NoError(t, os.Mkdir(p, 0o750)) }},
		{name: "executable", setup: func(p string) { writeFile(t, p, 0o700) }, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name)
			tt.setup(path)
			withMediaCenterBinary(t, path)
			assert.Equal(t, tt.want, mediaCenterInstalled())
		})
	}
}

func TestMediaCenterLauncherNotInstalled(t *testing.T) {
	withMediaCenterBinary(t, filepath.Join(t.TempDir(), "mediacenter"))

	launcher := createMediaCenterLauncher(NewPlatform())

	require.NotNil(t, launcher.Detected)
	assert.False(t, *launcher.Detected)
	assert.Nil(t, launcher.Availability, "a removed player must reach Launch so fvp can take the file")

	var repair *platforms.LaunchRepairError
	_, err := launcher.Launch(nil, "/media/fat/games/Video/a.iso", nil)
	require.ErrorAs(t, err, &repair, "a file fvp cannot play is refused before the console is touched")
	assert.Equal(t, platforms.LaunchRepairLauncherNotInstalled, repair.Reason())

	_, err = launcher.Launch(nil, "", nil)
	require.Error(t, err)
}

func TestMediaCenterLauncherInstalled(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "mediacenter")
	writeFile(t, bin, 0o700)
	withMediaCenterBinary(t, bin)

	launcher := createMediaCenterLauncher(NewPlatform())

	assert.Equal(t, tracker.MediaCenterLauncherID, launcher.ID)
	assert.Equal(t, platforms.LifecycleTracked, launcher.Lifecycle)
	require.NotNil(t, launcher.Detected)
	assert.True(t, *launcher.Detected)
}

// When the player has gone since the launcher list was built, a file fvp
// plays falls back to it instead of failing.
func TestFvpPlays(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"a.mp4", "a.MKV", "a.avi", "a.mov", "a.webm"} {
		assert.True(t, fvpPlays(path), path)
	}
	for _, path := range []string{"a.iso", "a.cue", "a.m2ts", "a.vob", "a", ""} {
		assert.False(t, fvpPlays(path), path)
	}
}

// MediaCenter takes the video folders from fvp while it is installed, and
// gives them back when it is not. It never claims a disc image outside them.
func TestMediaCenterPrecedenceOverFvp(t *testing.T) {
	root := filepath.Join(misterconfig.SDRootDir, "games")
	bin := filepath.Join(t.TempDir(), "mediacenter")

	for _, installed := range []bool{true, false} {
		name := "missing"
		if installed {
			name = "installed"
			writeFile(t, bin, 0o700)
		} else {
			require.NoError(t, os.Remove(bin))
		}
		t.Run(name, func(t *testing.T) {
			withMediaCenterBinary(t, bin)
			mockPl := mocks.NewMockPlatform()
			cfg := &config.Instance{}
			mockPl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()})
			mockPl.On("RootDirs", cfg).Return([]string{root})

			p := NewPlatform()
			launchers := append(CreateLaunchers(mockPl), createMediaCenterLauncher(p), createVideoLauncher(p))
			oldCache := helpers.GlobalLauncherCache
			helpers.GlobalLauncherCache = &helpers.LauncherCache{}
			t.Cleanup(func() { helpers.GlobalLauncherCache = oldCache })
			helpers.GlobalLauncherCache.InitializeFromSlice(launchers)

			mediaCenterWhenInstalled := tracker.VideoLauncherID
			if installed {
				mediaCenterWhenInstalled = tracker.MediaCenterLauncherID
			}
			for _, tc := range []struct {
				path string
				want string
			}{
				{filepath.Join(root, "Video", "Film.mkv"), mediaCenterWhenInstalled},
				{filepath.Join(root, "Movies", "Film.MP4"), mediaCenterWhenInstalled},
				{filepath.Join(root, "TV", "Show", "Episode.avi"), mediaCenterWhenInstalled},
				// Only MediaCenter plays these, so it is chosen even when
				// missing, and its availability says why it cannot launch.
				{filepath.Join(root, "Video", "Film.iso"), tracker.MediaCenterLauncherID},
				{filepath.Join(root, "Movies", "Film.m2ts"), tracker.MediaCenterLauncherID},
				// Only fvp plays WebM.
				{filepath.Join(root, "Video", "Clip.webm"), tracker.VideoLauncherID},
			} {
				got, err := helpers.FindLauncher(cfg, mockPl, tc.path)
				require.NoError(t, err, tc.path)
				assert.Equal(t, tc.want, got.ID, tc.path)
			}

			for _, path := range []string{
				filepath.Join(root, "DVD", "Film.iso"),
				filepath.Join(root, "PSX", "Game.cue"),
				filepath.Join(root, "Saturn", "Game.iso"),
			} {
				got, err := helpers.FindLauncher(cfg, mockPl, path)
				if err == nil {
					assert.NotEqual(t, tracker.MediaCenterLauncherID, got.ID, path)
				}
			}
		})
	}
}
