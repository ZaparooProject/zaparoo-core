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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

const NativeAudioLauncherID = "native-audio"

var nativeAudioHooks struct {
	playback           audio.PlaybackManager
	setBackgroundMedia func(*models.ActiveMedia)
}

func SetNativeAudioHooks(playback audio.PlaybackManager, setBackgroundMedia func(*models.ActiveMedia)) {
	nativeAudioHooks.playback = playback
	nativeAudioHooks.setBackgroundMedia = setBackgroundMedia
}

func NativeAudioEnabled() bool {
	return nativeAudioHooks.playback != nil
}

func NativeAudioLauncher() Launcher {
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
		Launch: launchNativeAudio,
		Controls: map[string]Control{
			ControlTogglePause: {Func: nativeAudioControl(ControlTogglePause)},
			ControlPause:       {Func: nativeAudioControl(ControlPause)},
			ControlResume:      {Func: nativeAudioControl(ControlResume)},
			ControlStop:        {Func: nativeAudioControl(ControlStop)},
			ControlFastForward: {Func: nativeAudioControl(ControlFastForward)},
			ControlRewind:      {Func: nativeAudioControl(ControlRewind)},
		},
	}
}

func launchNativeAudio(cfg *config.Instance, path string, opts *LaunchOptions) (*os.Process, error) {
	_ = cfg
	if nativeAudioHooks.playback == nil {
		return nil, errors.New("native audio playback is not initialized")
	}
	slot := MediaSlotPrimary
	if opts != nil {
		var err error
		slot, err = NormalizeMediaSlot(opts.Slot)
		if err != nil {
			return nil, fmt.Errorf("normalize native audio slot: %w", err)
		}
	}

	if err := nativeAudioHooks.playback.Play(slot, path, audio.PlaybackOptions{}); err != nil {
		return nil, fmt.Errorf("play native audio: %w", err)
	}

	if slot == MediaSlotBackground && nativeAudioHooks.setBackgroundMedia != nil {
		media := models.NewActiveMedia("Audio", "Audio", path, audioDisplayName(path), NativeAudioLauncherID)
		nativeAudioHooks.setBackgroundMedia(media)
	}

	return nil, nil //nolint:nilnil // native audio has no OS process to return
}

func nativeAudioControl(action string) ControlFunc {
	return func(_ context.Context, _ *config.Instance, params ControlParams) error {
		if nativeAudioHooks.playback == nil {
			return errors.New("native audio playback is not initialized")
		}
		slot := params.Args["slot"]
		if slot == "" {
			slot = MediaSlotPrimary
		}
		slot, err := NormalizeMediaSlot(slot)
		if err != nil {
			return err
		}

		switch action {
		case ControlTogglePause:
			return nativeAudioHooks.playback.TogglePause(slot)
		case ControlPause:
			return nativeAudioHooks.playback.Pause(slot)
		case ControlResume:
			return nativeAudioHooks.playback.Resume(slot)
		case ControlStop:
			return nativeAudioHooks.playback.Stop(slot)
		case ControlFastForward:
			return nativeAudioHooks.playback.Seek(slot, controlSeekDuration(params.Args, 10*time.Second))
		case ControlRewind:
			return nativeAudioHooks.playback.Seek(slot, -controlSeekDuration(params.Args, 10*time.Second))
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
