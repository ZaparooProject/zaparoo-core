//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/rs/zerolog/log"
)

// MiSTer MediaCenter (bonna79/MiSTer_MediaCenter) is a video player that runs
// on the ARM and draws to the Linux framebuffer. Only the player binary is
// used: Core does not support the project's own Main fork or menu core, so on
// stock Main it plays through a 32-bit framebuffer over HDMI.

// mediaCenterRenderScale keeps the framebuffer at the output's native size,
// as the player's own script does when the framebuffer is already 32-bit. The
// player scales the video to it on the ARM. A smaller framebuffer breaks its
// picture over HDMI: it takes any frame buffer under 720 lines for a 15 kHz
// CRT mode.
const mediaCenterRenderScale = 100

// mediaCenterTextTimeout bounds the call that puts the console back in text
// mode after playback.
const mediaCenterTextTimeout = 3 * time.Second

// mediaCenterBinary is the player, at the path its install instructions use.
// A variable so tests can point it at a temporary file.
var mediaCenterBinary = filepath.Join(misterconfig.LinuxDir, "mediacenter")

// mediaCenterExtensions are the video files the player opens. Audio and
// images are left to Core's own launchers.
var mediaCenterExtensions = []string{
	".avi", ".divx", ".xvid", ".mkv", ".mp4", ".m4v", ".mov", ".mpg", ".mpeg",
	".vob", ".dat", ".ts", ".m2ts", ".mts", ".ogm", ".ogv", ".wmv", ".asf",
	".iso", ".cue",
}

// executableFile reports whether path is a regular file with an execute bit.
func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func mediaCenterInstalled() bool {
	return executableFile(mediaCenterBinary)
}

func errMediaCenterNotInstalled() error {
	//nolint:wrapcheck // the repair error is the whole message; a wrapper would only prefix it
	return platforms.NewLaunchRepairErrorWithReason(
		platforms.LaunchRepairLauncherNotInstalled,
		map[string]string{platforms.LaunchRepairParamLauncher: "MediaCenter"},
		"MiSTer MediaCenter is not installed",
	)
}

// createMediaCenterLauncher plays video files with MiSTer MediaCenter. It
// shares fvp's folders and is registered before it: while the player is
// installed it is chosen for the files both handle, and when it is missing
// Detected is false, so fvp keeps them.
//
// Detected is only re-read when the launcher list is rebuilt, so the launch
// checks for the player again: if it was removed since, a file fvp can play
// goes to fvp instead of failing. That is also why there is no Availability
// check, which would refuse the launch before it gets that far.
func createMediaCenterLauncher(pl *Platform) platforms.Launcher {
	detected := mediaCenterInstalled()
	return platforms.Launcher{
		ID:         tracker.MediaCenterLauncherID,
		SystemID:   systemdefs.SystemVideo,
		Folders:    []string{"Video", "Movies", "TV"},
		Extensions: mediaCenterExtensions,
		Lifecycle:  platforms.LifecycleTracked,
		Detected:   &detected,
		Launch:     launchMediaCenter(pl),
	}
}

// fvpPlays reports whether fvp handles path's extension.
func fvpPlays(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(fvpExtensions, ext)
}

// buildMediaCenterCommand builds the command that plays one file and exits.
func buildMediaCenterCommand(ctx context.Context, path string) *exec.Cmd {
	//nolint:gosec // G204: the launcher's purpose is to play the media path
	cmd := exec.CommandContext(ctx, mediaCenterBinary, path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Own session for TTY control, as fvp
	}
	// The player first asks Main for a framebuffer at the video's size, which
	// only its own Main provides. Each refused request leaves the screen black
	// for seconds before it falls back to scaling on the ARM, so go straight
	// to the fallback.
	cmd.Env = append(os.Environ(), "MC_ARMSCALE=1")
	terminateOnCancel(cmd)
	return cmd
}

// terminateOnCancel makes a cancelled launcher context ask the process to
// stop with SIGTERM instead of exec's default SIGKILL. A stop or a new launch
// cancels the context at the same moment stopTrackedProcess sends SIGTERM, and
// a SIGKILL then cuts the program's own cleanup short. stopTrackedProcess
// still sends SIGKILL if the process does not exit in time.
func terminateOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
}

// restoreMediaCenterConsole puts the console back in text mode, which the
// player leaves in graphics mode if it does not end normally.
func restoreMediaCenterConsole() {
	ctx, cancel := context.WithTimeout(context.Background(), mediaCenterTextTimeout)
	defer cancel()
	//nolint:gosec // G204: fixed binary and argument
	cmd := exec.CommandContext(ctx, mediaCenterBinary, "--console-text")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
		log.Debug().Err(err).Msg("mediacenter: failed to restore text console")
	}
}

func launchMediaCenter(pl *Platform) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if path == "" {
			return nil, errors.New("no path specified")
		}
		if !mediaCenterInstalled() {
			if fvpPlays(path) {
				log.Info().Str("path", mediaCenterBinary).Msg("MediaCenter player not found, playing with fvp")
				return playWithFvp(pl, path, opts, tracker.MediaCenterLauncherID)
			}
			log.Warn().Str("path", mediaCenterBinary).Msg("MediaCenter player not found")
			return nil, errMediaCenterNotInstalled()
		}

		framebuffer, err := resolveFramebufferMode(opts, mediaCenterRenderScale, "")
		if err != nil {
			return nil, fmt.Errorf("resolve MediaCenter render size: %w", err)
		}

		launcherCtx := pl.launcherManager.GetContext()

		cm, err := setupConsoleEnvironment(launcherCtx, pl)
		if err != nil {
			return nil, err
		}

		if modeErr := applyFramebufferMode(framebuffer, mistermain.VideoModeFormatRGB32); modeErr != nil {
			_ = cm.Close()
			return nil, fmt.Errorf("failed to set MediaCenter render size: %w", modeErr)
		}

		log.Info().Str("path", path).Msg("launching video with MediaCenter")

		var finished func()
		if opts != nil {
			finished = opts.MediaFinished
		}

		consoleRestore := createConsoleRestoreFunc(cm)
		restore := func() {
			restoreMediaCenterConsole()
			consoleRestore()
		}

		return runTrackedProcess(pl, buildMediaCenterCommand(launcherCtx, path), restore, "mediacenter", trackedExit{
			launcherID:    tracker.MediaCenterLauncherID,
			mediaFinished: finished,
		})
	}
}
