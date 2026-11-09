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

package platforms

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
)

const NativeAudioLauncherID = "native-audio"

// NativeAudioLauncher returns the launcher that plays audio files in-process via the
// shared malgo output device. Playback and the media state hooks are injected so the
// launcher carries no package-level globals. stopPrimaryMedia wraps an explicit stop
// of the primary slot: native audio has no OS process, so no platform tracker notices
// the stop and the drain callback only fires for tracks that end on their own, which
// leaves the service to run the stop it is handed and clear the active media around it.
// resumeAt, when not nil, returns how far into a primary track playback got last time
// and how long the track was then.
func NativeAudioLauncher(
	playback audio.PlaybackManager,
	setBackgroundMedia func(*models.ActiveMedia),
	stopPrimaryMedia func(ctx context.Context, stop func() error) error,
	resumeAt func(path string) (position, length time.Duration),
) Launcher {
	controls := map[string]Control{
		ControlTogglePause: {Func: nativeAudioControl(playback, nil, nil, ControlTogglePause)},
		ControlPause:       {Func: nativeAudioControl(playback, nil, nil, ControlPause)},
		ControlResume:      {Func: nativeAudioControl(playback, nil, nil, ControlResume)},
		ControlStop:        {Func: nativeAudioControl(playback, setBackgroundMedia, stopPrimaryMedia, ControlStop)},
		ControlFastForward: {Func: nativeAudioControl(playback, nil, nil, ControlFastForward)},
		ControlRewind:      {Func: nativeAudioControl(playback, nil, nil, ControlRewind)},
	}
	// Background media is published by the launch itself rather than by the
	// launch pipeline, so it carries the control names from here.
	controlNames := ControlNames(&Launcher{Controls: controls})
	return Launcher{
		ID:       NativeAudioLauncherID,
		SystemID: "Audio",
		Folders:  []string{"Audio", "Music"},
		// Playlist files are listed with audio so they can be browsed and
		// picked; launching one runs it as a playlist instead.
		Extensions: slices.Concat(audio.SupportedExtensions, audio.PlaylistExtensions),
		Launch: func(cfg *config.Instance, path string, opts *LaunchOptions) (*os.Process, error) {
			return launchNativeAudio(playback, setBackgroundMedia, resumeAt, controlNames, cfg, path, opts)
		},
		Controls: controls,
	}
}

// MusicVolumeGain converts a configured music volume level (0-200) to the
// playback gain native audio plays at. 0 is silent.
func MusicVolumeGain(cfg *config.Instance) float64 {
	if cfg == nil {
		return 1.0
	}
	return float64(cfg.AudioMusicVolume()) / 100.0
}

func launchNativeAudio(
	playback audio.PlaybackManager,
	setBackgroundMedia func(*models.ActiveMedia),
	resumeAt func(path string) (position, length time.Duration),
	controlNames []string,
	cfg *config.Instance,
	path string,
	opts *LaunchOptions,
) (*os.Process, error) {
	if playback == nil {
		return nil, errors.New("native audio playback is not initialized")
	}

	slot := mediaslot.Primary
	if opts != nil {
		var err error
		slot, err = mediaslot.Normalize(opts.Slot)
		if err != nil {
			return nil, fmt.Errorf("normalize native audio slot: %w", err)
		}
	}

	playOpts := audio.PlaybackOptions{Volume: MusicVolumeGain(cfg)}
	if opts != nil {
		playOpts.Stream = opts.AudioStream
	}

	if slot == mediaslot.Primary && resumeAt != nil {
		playOpts.StartAt, playOpts.StartAtDuration = resumeAt(path)
	}
	if err := playback.Play(slot, path, playOpts); err != nil {
		return nil, fmt.Errorf("play native audio: %w", err)
	}

	if slot == mediaslot.Background && setBackgroundMedia != nil {
		name := audioDisplayName(path)
		if opts != nil && opts.AudioStream.Name() != "" {
			name = opts.AudioStream.Name()
		}
		media := models.NewActiveMedia("Audio", "Audio", path, name, NativeAudioLauncherID)
		media.LauncherControls = slices.Clone(controlNames)
		setBackgroundMedia(media)
	}

	return nil, nil //nolint:nilnil // native audio has no OS process to return
}

func nativeAudioControl(
	playback audio.PlaybackManager,
	setBackgroundMedia func(*models.ActiveMedia),
	stopPrimaryMedia func(ctx context.Context, stop func() error) error,
	action string,
) ControlFunc {
	return func(ctx context.Context, _ *config.Instance, params ControlParams) error {
		if playback == nil {
			return errors.New("native audio playback is not initialized")
		}
		rawSlot := params.Args[string(gozapscript.KeySlot)]
		if rawSlot == "" {
			rawSlot = mediaslot.Primary
		}
		slot, err := mediaslot.Normalize(rawSlot)
		if err != nil {
			return fmt.Errorf("normalize slot: %w", err)
		}

		switch action {
		case ControlTogglePause:
			return playback.TogglePause(slot)
		case ControlPause:
			return playback.Pause(slot)
		case ControlResume:
			return playback.Resume(slot)
		case ControlStop:
			stop := func() error {
				if stopErr := playback.Stop(slot); stopErr != nil {
					return fmt.Errorf("stop native audio: %w", stopErr)
				}
				return nil
			}
			if slot == mediaslot.Background {
				if err := stop(); err != nil {
					return err
				}
				// Launching set the background media, so stopping clears it. The
				// background playlist is service state and stays with the caller.
				if setBackgroundMedia != nil {
					setBackgroundMedia(nil)
				}
				return nil
			}
			if stopPrimaryMedia == nil {
				return stop()
			}
			return stopPrimaryMedia(ctx, stop)
		case ControlFastForward:
			return playback.Seek(slot, controlSeekDuration(params.Args, 10*time.Second))
		case ControlRewind:
			return playback.Seek(slot, -controlSeekDuration(params.Args, 10*time.Second))
		default:
			return fmt.Errorf("unsupported native audio control: %s", action)
		}
	}
}

func controlSeekDuration(args map[string]string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(args["seconds"])
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func audioDisplayName(location string) string {
	if audio.IsStreamURL(location) {
		u, err := url.Parse(location)
		if err != nil || u.Host == "" {
			return location
		}
		// A stream is named by the last part of its path, as a file is,
		// or by its host when the path names nothing.
		base := pathpkg.Base(u.Path)
		if base == "/" || base == "." {
			return u.Hostname()
		}
		return tags.ParseTitleFromFilename(strings.TrimSuffix(base, pathpkg.Ext(base)), false)
	}
	base := filepath.Base(location)
	return tags.ParseTitleFromFilename(strings.TrimSuffix(base, filepath.Ext(base)), false)
}
