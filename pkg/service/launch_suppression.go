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
	"fmt"
	"runtime"
	"strings"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/scanmode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

func suppressTapRelaunch(svc *ServiceContext, token *tokens.Token, inHook bool) bool {
	return !inHook && token.Source == tokens.SourceReader &&
		!svc.Config.ReadersScan().AllowRelaunch &&
		scanmode.ForToken(svc.Config, svc.State, token) == config.ScanModeTap
}

// launchMatchesActive compares concrete launch targets, never title similarity.
// Name-based equality can conflate different revisions, regions, or ROM hacks.
func launchMatchesActive(active *models.ActiveMedia, target platforms.ResolvedLaunch) bool {
	if active == nil || active.Path == "" || target.Path == "" {
		return false
	}
	if system, err := systemdefs.GetSystem(active.SystemID); err == nil &&
		system.GetMediaType() != slugs.MediaTypeGame {
		return false
	}
	if opts := target.Options; opts != nil {
		if (opts.Slot != "" && opts.Slot != mediaslot.Primary) ||
			(opts.Action != "" && !strings.EqualFold(opts.Action, "run")) ||
			opts.SetName != "" || opts.SetNameSameDir != "" {
			return false
		}
	}
	systemID := target.SystemID
	if target.Launcher != nil {
		if !strings.EqualFold(target.Launcher.ID, active.LauncherID) {
			return false
		}
		systemID = target.Launcher.SystemID
	}
	if systemID != "" && !strings.EqualFold(systemID, active.SystemID) {
		return false
	}
	currentPath := pathutil.CanonicalMediaPath(active.Path)
	targetPath := pathutil.CanonicalMediaPath(target.Path)
	// URI identifiers may be case-sensitive even on Windows.
	if runtime.GOOS == "windows" && !strings.Contains(targetPath, "://") {
		return strings.EqualFold(currentPath, targetPath)
	}
	return currentPath == targetPath
}

func canDeferGuardCommand(cmd gozapscript.Command) bool {
	if !zapscript.HasResolvedLaunchTarget(cmd.Name) || commandTargetsBackgroundSlot(cmd) {
		return false
	}
	if len(cmd.Args) > 0 {
		arg := strings.ToLower(cmd.Args[0])
		// A ZapLink can expand to arbitrary commands, including stop or a
		// playlist. Keep whole-token confirmation before fetching that script.
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return false
		}
	}
	return true
}

// recheckConfirmedLaunch repeats admission checks that may have changed since
// the worker accepted the scan. Confirmation must not retain an old permission
// to launch after the device is locked or a playtime limit is reached.
func recheckConfirmedLaunch(svc *ServiceContext, command string) error {
	if !svc.State.RunZapScriptEnabled() {
		return state.ErrRunZapScriptDisabled
	}
	if svc.Config.IsCommandBlocked(command) {
		return fmt.Errorf("%w: %s", zapscript.ErrCommandBlocked, command)
	}
	if svc.Config.ProfilesRequireForLaunch() && svc.State.ActiveProfile() == nil {
		return state.ErrLaunchRequiresProfile
	}
	if svc.LimitsManager != nil {
		if _, err := svc.LimitsManager.CheckBeforeLaunch(); err != nil {
			return fmt.Errorf("confirmed launch blocked: %w", err)
		}
	}
	return nil
}

// resolvedLaunchConfirmation leaves staging and re-tap arbitration with the
// reader manager while the worker retains its already-resolved launch target.
// Buffered results let cancellation and shutdown release a worker that left.
type resolvedLaunchConfirmation struct {
	ctx    context.Context
	result chan bool
	token  tokens.Token
}

func confirmResolvedLaunch(ctx context.Context, svc *ServiceContext, token *tokens.Token) (bool, error) {
	// Only scans taken while media was running defer confirmation here. A
	// stop during resolution cancels that scan, just like an already-open prompt.
	if svc.State.ActiveMedia() == nil {
		return false, nil
	}
	if !svc.Config.LaunchGuardEnabled() {
		return true, nil
	}
	request := &resolvedLaunchConfirmation{
		ctx: ctx, result: make(chan bool, 1), token: *token,
	}
	select {
	case svc.ResolvedLaunchGuard <- request:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case confirmed := <-request.result:
		return confirmed, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
