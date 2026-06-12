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
	"os"
	"path/filepath"
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
// shared malgo output device. Both playback and the background-media state setter are
// injected so the launcher carries no package-level globals.
func NativeAudioLauncher(
	playback audio.PlaybackManager,
	setBackgroundMedia func(*models.ActiveMedia),
) Launcher {
	return Launcher{
		ID:       NativeAudioLauncherID,
		SystemID: "Audio",
		Folders:  []string{"Audio", "Music"},
		Extensions: []string{
			".wav",
			".mp3",
			".ogg",
			".flac",
		},
		Launch: func(cfg *config.Instance, path string, opts *LaunchOptions) (*os.Process, error) {
			return launchNativeAudio(playback, setBackgroundMedia, cfg, path, opts)
		},
		Controls: map[string]Control{
			ControlTogglePause: {Func: nativeAudioControl(playback, ControlTogglePause)},
			ControlPause:       {Func: nativeAudioControl(playback, ControlPause)},
			ControlResume:      {Func: nativeAudioControl(playback, ControlResume)},
			ControlStop:        {Func: nativeAudioControl(playback, ControlStop)},
			ControlFastForward: {Func: nativeAudioControl(playback, ControlFastForward)},
			ControlRewind:      {Func: nativeAudioControl(playback, ControlRewind)},
		},
	}
}

func launchNativeAudio(
	playback audio.PlaybackManager,
	setBackgroundMedia func(*models.ActiveMedia),
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

	volume := 1.0
	if cfg != nil {
		v := cfg.AudioVolume()
		if v > 0 {
			volume = float64(v) / 100.0
		}
	}

	if err := playback.Play(slot, path, audio.PlaybackOptions{Volume: volume}); err != nil {
		return nil, fmt.Errorf("play native audio: %w", err)
	}

	if slot == mediaslot.Background && setBackgroundMedia != nil {
		media := models.NewActiveMedia("Audio", "Audio", path, audioDisplayName(path), NativeAudioLauncherID)
		setBackgroundMedia(media)
	}

	return nil, nil //nolint:nilnil // native audio has no OS process to return
}

func nativeAudioControl(playback audio.PlaybackManager, action string) ControlFunc {
	return func(_ context.Context, _ *config.Instance, params ControlParams) error {
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
			return playback.Stop(slot)
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

func audioDisplayName(path string) string {
	base := filepath.Base(path)
	return tags.ParseTitleFromFilename(strings.TrimSuffix(base, filepath.Ext(base)), false)
}
