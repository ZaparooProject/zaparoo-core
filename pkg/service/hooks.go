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

package service

import (
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

// runHook executes a hook script with the standard playlist from state.
// Returns error if the script fails (for blocking hooks) or nil on success.
// The scanned/launching params provide optional context for the expression env.
func runHook(
	svc *ServiceContext,
	hookName string,
	script string,
	scanned *gozapscript.ExprEnvScanned,
	launching *gozapscript.ExprEnvLaunching,
) error {
	log.Info().Msgf("running %s: %s", hookName, script)

	plsc := playlists.PlaylistController{
		Active: svc.State.GetActivePlaylist(),
		Queue:  svc.PlaylistQueue,
	}

	t := tokens.Token{
		ScanTime: time.Now(),
		Text:     script,
		Source:   tokens.SourceHook,
	}

	hookEnv := zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, scanned, launching)
	return runTokenZapScript(svc, t, plsc, &hookEnv, true)
}
