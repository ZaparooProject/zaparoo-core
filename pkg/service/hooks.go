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

// appendSystemID adds systemID to ids unless an equal-folding entry is already
// present. Comparison is case-insensitive because system IDs come from user
// config as well as launcher definitions.
//
// Aliases are not expanded here: LookupSystemDefaults resolves both the entry it
// reads and the ID it is given to a canonical system, so every alias of a system
// already resolves to the same entry this canonical ID does.
func appendSystemID(ids []string, systemID string) []string {
	if systemID == "" {
		return ids
	}
	for _, existing := range ids {
		if strings.EqualFold(existing, systemID) {
			return ids
		}
	}
	return append(ids, systemID)
}

// beforeExitTarget is everything a before_exit lookup can key on for one piece
// of outgoing media: the systems it may be configured under, and the launcher
// that started it.
type beforeExitTarget struct {
	systemIDs  []string
	launcherID string
	groups     []string
}

// beforeExitTargetFor resolves the outgoing media's before_exit keys.
//
// System candidates are ordered outward from the media itself, because the lookup
// takes the first that carries a script: the media's own system, then the system
// of the launcher that started it, then the system of a launcher whose ID matches
// the media's system ID. That last shape is the only one that matched before the
// lookup was fixed, and only on platforms where launcher IDs happen to equal
// system IDs, so it is kept for configs that relied on it.
//
// Launchers resolve through the cache rather than Platform.Launchers: the cache
// is the only complete source, it folds case the way every other launcher-ID
// comparison in the codebase does, and it avoids rebuilding the whole launcher
// list, which MiSTer does on every Launchers call.
func beforeExitTargetFor(svc *ServiceContext, media *models.ActiveMedia) beforeExitTarget {
	target := beforeExitTarget{launcherID: media.LauncherID}
	target.systemIDs = appendSystemID(target.systemIDs, media.SystemID)

	if svc.LauncherCache == nil {
		return target
	}
	if media.LauncherID != "" {
		if launcher := svc.LauncherCache.GetLauncherByID(media.LauncherID); launcher != nil {
			target.systemIDs = appendSystemID(target.systemIDs, launcher.SystemID)
			target.groups = launcher.Groups
		}
	}
	if media.SystemID != "" {
		if launcher := svc.LauncherCache.GetLauncherByID(media.SystemID); launcher != nil {
			target.systemIDs = appendSystemID(target.systemIDs, launcher.SystemID)
		}
	}

	return target
}

// beforeExitScript returns the before_exit script that applies to the outgoing
// media, or an empty string when none does.
//
// Scopes resolve narrowest first, first non-empty winning and an empty value
// falling through, the same shape as the scan-mode chain:
//
//  1. a [[launchers.default]] entry naming the outgoing launcher exactly
//  2. a [[systems.default]] entry for the outgoing media's system
//  3. a [[launchers.default]] entry naming one of that launcher's groups
//  4. the global [launchers] before_exit
//
// An exact launcher is narrower than a system; a group spans many launchers
// across many systems, so it is broader than one.
func beforeExitScript(svc *ServiceContext, media *models.ActiveMedia) string {
	target := beforeExitTargetFor(svc, media)

	// An empty launcher ID would match a [[launchers.default]] entry that leaves
	// launcher unset, and media published by a platform tracker rather than by a
	// launch carries no launcher at all.
	hasLauncher := target.launcherID != ""

	if hasLauncher {
		if script := svc.Config.LookupLauncherDefaults(target.launcherID, nil).BeforeExit; script != "" {
			return script
		}
	}

	for _, systemID := range target.systemIDs {
		defaults, ok := svc.Config.LookupSystemDefaults(systemID)
		if ok && defaults.BeforeExit != "" {
			return defaults.BeforeExit
		}
	}

	// Any script left here came from a group entry: the exact-launcher tier above
	// already returned if one named this launcher directly.
	if hasLauncher && len(target.groups) > 0 {
		if script := svc.Config.LookupLauncherDefaults(target.launcherID, target.groups).BeforeExit; script != "" {
			return script
		}
	}

	return svc.Config.LaunchersBeforeExit()
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
