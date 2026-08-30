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
	"context"
	"errors"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
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
	return runHookWithContext(svc.State.GetContext(), svc, hookName, script, scanned, launching)
}

func runHookWithContext(
	ctx context.Context,
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
	return runTokenZapScriptWithContext(ctx, svc, t, plsc, &hookEnv, true)
}

// beforeExitHookTimeout bounds a before_exit script so a hook containing a
// delay cannot stall an exit indefinitely. A var so tests can shorten it.
var beforeExitHookTimeout = 30 * time.Second

// appendSystemAndAliases adds systemID and its aliases to ids, skipping any
// already present. Comparison is case-insensitive because system IDs come from
// user config as well as launcher definitions.
func appendSystemAndAliases(ids []string, systemID string) []string {
	if systemID == "" {
		return ids
	}
	add := func(id string) []string {
		for _, existing := range ids {
			if strings.EqualFold(existing, id) {
				return ids
			}
		}
		return append(ids, id)
	}
	ids = add(systemID)
	if system, err := systemdefs.LookupSystem(systemID); err == nil {
		for _, alias := range system.Aliases {
			ids = add(alias)
		}
	}
	return ids
}

// beforeExitSystemIDs returns the candidate system IDs to look up before_exit
// defaults for, widest match first: the media's own system, then the system of
// the launcher that started it, then a launcher whose ID matches the media's
// system ID.
//
// The last case is the only shape that ever matched before, and it only holds
// on platforms where launcher IDs happen to equal system IDs. It is kept so
// existing configs that relied on it keep working.
func beforeExitSystemIDs(svc *ServiceContext, media *models.ActiveMedia) []string {
	var ids []string
	ids = appendSystemAndAliases(ids, media.SystemID)

	launchers := svc.Platform.Launchers(svc.Config)
	for i := range launchers {
		if launchers[i].ID == media.LauncherID {
			ids = appendSystemAndAliases(ids, launchers[i].SystemID)
			break
		}
	}
	for i := range launchers {
		if launchers[i].ID == media.SystemID {
			ids = appendSystemAndAliases(ids, launchers[i].SystemID)
			break
		}
	}

	return ids
}

// beforeExitScript returns the before_exit script configured for the outgoing
// media's system, or an empty string when none applies.
func beforeExitScript(svc *ServiceContext, media *models.ActiveMedia) string {
	for _, systemID := range beforeExitSystemIDs(svc, media) {
		defaults, ok := svc.Config.LookupSystemDefaults(systemID)
		if ok && defaults.BeforeExit != "" {
			return defaults.BeforeExit
		}
	}
	return ""
}

// runBeforeExitHook runs the outgoing primary media's before_exit script.
//
// Failures only log: the hook never aborts the launch or stop that triggered
// it. A user script must not be able to defeat a playtime limit, and by the
// time a launch preempts running media the decision is already made.
func runBeforeExitHook(svc *ServiceContext) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic running before_exit script")
		}
	}()

	media := svc.State.ActiveMedia()
	if media == nil {
		return
	}
	script := beforeExitScript(svc, media)
	if script == "" {
		return
	}

	ctx, cancel := context.WithTimeout(svc.State.GetContext(), beforeExitHookTimeout)
	defer cancel()

	err := runHookWithContext(ctx, svc, "before_exit", script, nil, nil)
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn().Dur("timeout", beforeExitHookTimeout).
			Msg("before_exit script timed out, continuing with exit")
	default:
		logHookError(err, "before_exit")
	}
}

// hookErrorBlocks reports whether a hook's error should block its caller,
// such as skipping a scan, keeping media running on removal, or failing a
// launch. A disabled "run ZapScript" setting is the prior silent no-op, not
// a hook failure, so callers must not treat it as one.
func hookErrorBlocks(err error) bool {
	return err != nil && !errors.Is(err, state.ErrRunZapScriptDisabled)
}
