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
	"fmt"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

type nextActionResult int

const (
	nextActionNone nextActionResult = iota
	nextActionArmed
	nextActionInvalid
)

func handleNextActionPreflight(svc *ServiceContext, token *tokens.Token, script *gozapscript.Script) nextActionResult {
	if !svc.State.RunZapScriptEnabled() {
		return nextActionNone
	}
	if len(script.Cmds) != 1 {
		return nextActionNone
	}

	cmd := script.Cmds[0]
	if svc.Config.IsCommandBlocked(cmd.Name) {
		return nextActionInvalid
	}
	switch cmd.Name {
	case gozapscript.ZapScriptCmdLaunch:
		launcherID := strings.TrimSpace(cmd.AdvArgs.Get(gozapscript.KeyLauncher))
		if launcherID == "" || len(cmd.Args) != 0 || hasNonLauncherAdvArg(cmd.AdvArgs) {
			return nextActionNone
		}
		resolvedLauncherID, err := evalNextActionArg(svc, launcherID)
		if err != nil || strings.TrimSpace(resolvedLauncherID) == "" {
			log.Warn().Err(err).Msg("failed to evaluate launch override")
			return nextActionInvalid
		}
		svc.State.SetPendingLaunchOverride(&state.PendingLaunchOverride{
			LauncherID: strings.TrimSpace(resolvedLauncherID),
			Source:     *token,
			CreatedAt:  time.Now(),
		})
		log.Info().Str("launcher", strings.TrimSpace(resolvedLauncherID)).Msg("armed one-shot launch override")
		return nextActionArmed
	case gozapscript.ZapScriptCmdWrite:
		if len(cmd.Args) != 1 || strings.TrimSpace(cmd.Args[0]) == "" || !cmd.AdvArgs.IsEmpty() {
			return nextActionInvalid
		}
		payload, err := evalNextActionArg(svc, cmd.Args[0])
		if err != nil || strings.TrimSpace(payload) == "" {
			log.Warn().Err(err).Msg("failed to evaluate write payload")
			return nextActionInvalid
		}
		svc.State.SetPendingWrite(&state.PendingWrite{
			Payload:   payload,
			Source:    *token,
			CreatedAt: time.Now(),
		})
		log.Info().Msg("armed next-card write")
		return nextActionArmed
	default:
		return nextActionNone
	}
}

func evalNextActionArg(svc *ServiceContext, value string) (string, error) {
	env := zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, nil, nil)
	reader := gozapscript.NewParser(value)
	output, err := reader.EvalExpressions(env)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate next-action argument: %w", err)
	}
	return output, nil
}

func hasNonLauncherAdvArg(args gozapscript.AdvArgs) bool {
	hasNonLauncher := false
	args.Range(func(key gozapscript.Key, _ string) bool {
		if key != gozapscript.KeyLauncher {
			hasNonLauncher = true
			return false
		}
		return true
	})
	return hasNonLauncher
}

func shouldApplyLaunchOverride(token *tokens.Token, inHookContext bool, cmdName string) bool {
	return token.Source == tokens.SourceReader && !inHookContext && launchOverrideEligible(cmdName)
}

func launchOverrideEligible(cmdName string) bool {
	switch cmdName {
	case gozapscript.ZapScriptCmdLaunch,
		gozapscript.ZapScriptCmdLaunchRandom,
		gozapscript.ZapScriptCmdLaunchSearch,
		gozapscript.ZapScriptCmdLaunchTitle,
		gozapscript.ZapScriptCmdLaunchLast,
		gozapscript.ZapScriptCmdRandom:
		return true
	default:
		return false
	}
}
