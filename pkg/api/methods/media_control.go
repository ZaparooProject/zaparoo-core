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

package methods

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

func HandleMediaControl(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	var params models.MediaControlParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	media := env.State.ActiveMedia()
	if media == nil {
		return nil, models.ClientErrf("no active media")
	}

	if env.LauncherCache == nil || media.LauncherID == "" {
		return nil, models.ClientErrf("no launcher associated with active media")
	}

	launcher := env.LauncherCache.GetLauncherByID(media.LauncherID)
	if launcher == nil {
		return nil, fmt.Errorf("launcher not found: %s", media.LauncherID)
	}

	if len(launcher.Controls) == 0 {
		return nil, models.ClientErrf("launcher %s: %w", media.LauncherID, zapscript.ErrNoControlCapabilities)
	}

	control, ok := launcher.Controls[params.Action]
	if !ok {
		return nil, models.ClientErrf("action %q not supported by launcher %s", params.Action, media.LauncherID)
	}

	log.Info().Str("action", params.Action).Str("launcher", media.LauncherID).Msg("executing media control action")

	var err error
	switch {
	case control.Func != nil:
		err = control.Func(env.Context, env.Config, platforms.ControlParams{Args: params.Args})
	case control.Script != "":
		exprEnv := zapscript.GetExprEnv(env.Platform, env.Config, env.State, nil, nil)
		err = zapscript.RunControlScript(env.Context, env.Platform, env.Config, env.Database, control.Script, &exprEnv)
	default:
		err = fmt.Errorf("control %q has no implementation", params.Action)
	}
	if err != nil {
		return nil, fmt.Errorf("control action %q failed: %w", params.Action, err)
	}

	return NoContent{}, nil
}
