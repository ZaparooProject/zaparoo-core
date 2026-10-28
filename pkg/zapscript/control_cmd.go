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

package zapscript

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

var (
	ErrNoActiveMedia            = errors.New("no active media")
	ErrNoLauncher               = errors.New("no launcher associated with active media")
	ErrNoControlCapabilities    = errors.New("no control capabilities")
	ErrUnsupportedControlAction = errors.New("not supported by launcher")
	ErrNoLauncherCache          = errors.New("launcher cache not available")
)

//nolint:gocritic // single-use parameter in command handler
func cmdControl(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	action := env.Cmd.Args[0]
	if action == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	slot, routed := controlMediaSlot(&env)
	launcherID, err := controlLauncherID(&env, slot)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// Resolve through the launcher cache, not pl.Launchers: launchers built at
	// service startup rather than by the platform, such as native audio, only
	// exist in the cache. The API control path resolves the same way.
	if env.LauncherCache == nil {
		return platforms.CmdResult{}, ErrNoLauncherCache
	}
	launcher := env.LauncherCache.GetLauncherByID(launcherID)
	if launcher == nil {
		return platforms.CmdResult{}, fmt.Errorf("launcher not found: %s", launcherID)
	}

	if len(launcher.Controls) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("launcher %s: %w", launcherID, ErrNoControlCapabilities)
	}

	control, ok := launcher.Controls[action]
	if !ok {
		return platforms.CmdResult{}, fmt.Errorf("action %q %w %s", action, ErrUnsupportedControlAction, launcherID)
	}

	args := controlArgs(&env, launcherID, slot, routed)

	// Readiness gates the primary media only. Background audio has no load
	// to wait for, and waiting would hold it on a game that is still loading.
	if slot == mediaslot.Primary && env.WaitForMediaReady != nil {
		ctx := env.LauncherCtx
		if ctx == nil {
			ctx = env.ServiceCtx
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if err = env.WaitForMediaReady(ctx); err != nil {
			return platforms.CmdResult{}, fmt.Errorf("wait for media ready: %w", err)
		}
	}

	log.Info().Str("action", action).Str("launcher", launcherID).Str("slot", slot).
		Msg("executing control command")

	cancelStop := func() {}
	if action == platforms.ControlStop && slot == mediaslot.Primary {
		cancelStop = env.RecordStop(apimodels.MediaStopReasonStopped)
	}

	switch {
	case control.Func != nil:
		ctx := env.LauncherCtx
		if ctx == nil {
			ctx = context.Background()
		}
		err = control.Func(ctx, env.Cfg, platforms.ControlParams{Args: args})
	case control.Script != "":
		ctx := env.ServiceCtx
		if ctx == nil {
			ctx = env.LauncherCtx
		}
		if ctx == nil {
			ctx = context.Background()
		}
		err = RunControlScript(ctx, pl, env.Cfg, env.Database, control.Script, env.ExprEnv)
	default:
		err = fmt.Errorf("control %q has no implementation", action)
	}
	if err != nil {
		cancelStop()
		return platforms.CmdResult{}, fmt.Errorf("control action %q failed: %w", action, err)
	}

	// Stopping the background slot closes the playlist driving it too, the
	// same as the stop command and the media.control API.
	if action == platforms.ControlStop && slot == mediaslot.Background {
		clearUpdate := &playlists.Playlist{Slot: mediaslot.Background, Clear: true}
		if err = queuePlaylistUpdate(&env, clearUpdate); err != nil {
			return platforms.CmdResult{}, err
		}
		return platforms.CmdResult{PlaylistChanged: true}, nil
	}

	return platforms.CmdResult{}, nil
}

// controlMediaSlot reports which media slot a control command targets. The
// slot argument is shared with save state controls, so only a value naming a
// media slot routes the command; any other value, such as a save state
// number, is left for the primary launcher. routed reports whether the slot
// argument was consumed as a media slot.
func controlMediaSlot(env *platforms.CmdEnv) (slot string, routed bool) {
	raw, ok := env.Cmd.AdvArgs.Raw()[string(zapscript.KeySlot)]
	if !ok {
		return mediaslot.Primary, false
	}
	normalized, err := mediaslot.Normalize(raw)
	if err != nil {
		return mediaslot.Primary, false
	}
	return normalized, true
}

// controlLauncherID returns the launcher of the media playing in slot.
func controlLauncherID(env *platforms.CmdEnv, slot string) (string, error) {
	if slot == mediaslot.Background {
		var media *apimodels.ActiveMedia
		if env.BackgroundMedia != nil {
			media = env.BackgroundMedia()
		}
		if media == nil {
			return "", ErrNoActiveMedia
		}
		if media.LauncherID == "" {
			return "", ErrNoLauncher
		}
		return media.LauncherID, nil
	}
	if env.ExprEnv == nil || !env.ExprEnv.MediaPlaying {
		return "", ErrNoActiveMedia
	}
	if env.ExprEnv.ActiveMedia.LauncherID == "" {
		return "", ErrNoLauncher
	}
	return env.ExprEnv.ActiveMedia.LauncherID, nil
}

// controlArgs builds the control params from the advanced args. The global
// "when" key is dropped, and a slot argument that routed the command is
// passed on only to native audio, which plays both slots and needs to know
// which one to act on.
func controlArgs(env *platforms.CmdEnv, launcherID, slot string, routed bool) map[string]string {
	raw := env.Cmd.AdvArgs.Raw()
	args := make(map[string]string, len(raw))
	for k, v := range raw {
		if k == string(zapscript.KeyWhen) {
			continue
		}
		if routed && k == string(zapscript.KeySlot) {
			continue
		}
		args[k] = v
	}
	if routed && launcherID == platforms.NativeAudioLauncherID {
		args[string(zapscript.KeySlot)] = slot
	}
	if len(args) == 0 {
		return nil
	}
	return args
}
