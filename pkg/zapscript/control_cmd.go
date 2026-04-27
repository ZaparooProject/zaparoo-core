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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

var (
	ErrNoActiveMedia         = errors.New("no active media")
	ErrNoLauncher            = errors.New("no launcher associated with active media")
	ErrNoControlCapabilities = errors.New("no control capabilities")
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

	if env.ExprEnv == nil || !env.ExprEnv.MediaPlaying {
		return platforms.CmdResult{}, ErrNoActiveMedia
	}

	launcherID := env.ExprEnv.ActiveMedia.LauncherID
	if launcherID == "" {
		return platforms.CmdResult{}, ErrNoLauncher
	}

	var launcher *platforms.Launcher
	for _, l := range pl.Launchers(env.Cfg) {
		if l.ID == launcherID {
			launcher = &l
			break
		}
	}
	if launcher == nil {
		return platforms.CmdResult{}, fmt.Errorf("launcher not found: %s", launcherID)
	}

	if len(launcher.Controls) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("launcher %s: %w", launcherID, ErrNoControlCapabilities)
	}

	control, ok := launcher.Controls[action]
	if !ok {
		return platforms.CmdResult{}, fmt.Errorf("action %q not supported by launcher %s", action, launcherID)
	}

	// Build control params from advargs, stripping the global "when" key
	var args map[string]string
	raw := env.Cmd.AdvArgs.Raw()
	if len(raw) > 0 {
		args = make(map[string]string, len(raw))
		for k, v := range raw {
			if k == string(zapscript.KeyWhen) {
				continue
			}
			args[k] = v
		}
		if len(args) == 0 {
			args = nil
		}
	}

	log.Info().Str("action", action).Str("launcher", launcherID).Msg("executing control command")

	var err error
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
		return platforms.CmdResult{}, fmt.Errorf("control action %q failed: %w", action, err)
	}

	return platforms.CmdResult{}, nil
}
