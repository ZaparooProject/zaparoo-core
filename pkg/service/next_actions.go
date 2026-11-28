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
	"maps"
	"slices"
	"strings"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/boolutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
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
	// nextActionBlocked separates a configuration denial from a malformed
	// next action so the API reports "blocked" rather than "invalid_script".
	nextActionBlocked
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
		return nextActionBlocked
	}
	switch cmd.Name {
	case gozapscript.ZapScriptCmdLaunch:
		if len(cmd.Args) != 0 || cmd.AdvArgs.IsEmpty() || !onlyLaunchModifierArgs(cmd.AdvArgs) {
			return nextActionNone
		}
		args, ok := resolveLaunchModifierArgs(svc, cmd.AdvArgs)
		if !ok {
			return nextActionInvalid
		}
		sticky := svc.State.ArmLaunchModifier(&state.LaunchModifier{
			Source:    *token,
			Args:      args,
			Sticky:    launchModifierCanStick(svc, token),
			CreatedAt: time.Now(),
		})
		if sticky {
			log.Info().Interface("args", args).Str("readerID", token.ReaderID).
				Msg("armed launch modifier while its card stays on the reader")
		} else {
			log.Info().Interface("args", args).Msg("armed one-shot launch modifier")
		}
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

// launcherIDExists reports whether a launcher ID resolves, matching the
// case-insensitive comparison the advanced argument validator uses when the
// override is later applied to a launch command.
func launcherIDExists(svc *ServiceContext, launcherID string) bool {
	for _, id := range zapscript.GetLauncherIDs(svc.Platform, svc.Config) {
		if strings.EqualFold(id, launcherID) {
			return true
		}
	}
	return false
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

// launchModifierKeys are the advanced arguments a launch card with no path
// can carry as a modifier. The others describe one particular piece of media,
// so they make no sense applied to whatever is scanned next.
var launchModifierKeys = []gozapscript.Key{
	gozapscript.KeyLauncher,
	gozapscript.KeySetName,
	gozapscript.KeySetNameSameDir,
	gozapscript.KeyAction,
}

// launchModifierGroups are the keys a modifier sets together. The set_name
// pair describes one launch setup, so a launch never takes one half from one
// modifier card and the other half from another.
var launchModifierGroups = [][]gozapscript.Key{
	{gozapscript.KeyLauncher},
	{gozapscript.KeySetName, gozapscript.KeySetNameSameDir},
	{gozapscript.KeyAction},
}

func isLaunchModifierKey(key gozapscript.Key) bool {
	return slices.Contains(launchModifierKeys, key)
}

func onlyLaunchModifierArgs(args gozapscript.AdvArgs) bool {
	only := true
	args.Range(func(key gozapscript.Key, _ string) bool {
		if !isLaunchModifierKey(key) {
			only = false
			return false
		}
		return true
	})
	return only
}

// resolveLaunchModifierArgs evaluates and validates a modifier's arguments.
// Bad values are rejected here rather than when they are applied, otherwise
// the error only surfaces on the next card scanned, blamed on that card
// instead of this one.
func resolveLaunchModifierArgs(svc *ServiceContext, raw gozapscript.AdvArgs) (map[string]string, bool) {
	args := make(map[string]string)
	valid := true
	raw.Range(func(key gozapscript.Key, value string) bool {
		resolved, err := evalNextActionArg(svc, value)
		resolved = strings.TrimSpace(resolved)
		if err != nil || resolved == "" {
			log.Warn().Err(err).Str("key", string(key)).Msg("failed to evaluate launch modifier")
			valid = false
			return false
		}
		switch key {
		case gozapscript.KeyLauncher:
			if !launcherIDExists(svc, resolved) {
				log.Warn().Str("launcher", resolved).Msg("launch modifier launcher not found")
				valid = false
				return false
			}
		case gozapscript.KeyAction:
			if !gozapscript.IsActionRun(resolved) && !gozapscript.IsActionDetails(resolved) {
				log.Warn().Str("action", resolved).Msg("launch modifier action not supported")
				valid = false
				return false
			}
		case gozapscript.KeySetNameSameDir:
			if _, ok := boolutil.Parse(resolved); !ok {
				log.Warn().Str("set_name_same_dir", resolved).Msg("launch modifier set_name_same_dir is not yes or no")
				valid = false
				return false
			}
		default:
		}
		args[string(key)] = resolved
		return true
	})
	return args, valid
}

// launchModifierCanStick reports whether a modifier card can stay in effect
// while it rests on its reader: the token resolves to hold mode and its
// reader reports removal, so lifting the card is seen.
func launchModifierCanStick(svc *ServiceContext, token *tokens.Token) bool {
	if token.Source != tokens.SourceReader || token.ReaderID == "" {
		return false
	}
	if !holdModeForToken(svc, token) {
		return false
	}
	r, ok := svc.State.GetReader(token.ReaderID)
	return ok && readers.HasCapability(r, readers.CapabilityRemovable)
}

// applyLaunchModifiers returns args with the modifiers applied in order, so a
// later modifier wins. Each key group is taken whole from the last modifier
// that sets any key in it, and a modifier's group replaces the launch's own
// values for that group.
func applyLaunchModifiers(args gozapscript.AdvArgs, modifiers []state.LaunchModifier) gozapscript.AdvArgs {
	if len(modifiers) == 0 {
		return args
	}
	merged := maps.Clone(args.Raw())
	if merged == nil {
		merged = make(map[string]string)
	}
	for _, group := range launchModifierGroups {
		var winner map[string]string
		for i := range modifiers {
			for _, key := range group {
				if _, ok := modifiers[i].Args[string(key)]; ok {
					winner = modifiers[i].Args
					break
				}
			}
		}
		if winner == nil {
			continue
		}
		for _, key := range group {
			if value, ok := winner[string(key)]; ok {
				merged[string(key)] = value
			} else {
				delete(merged, string(key))
			}
		}
	}
	return gozapscript.NewAdvArgs(merged)
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
