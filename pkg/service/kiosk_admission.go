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
	"fmt"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// admitKioskLaunch asks the kiosk lock whether a resolved launch may go
// ahead. Every launch that resolves to a system or a media path comes here,
// whatever started it: a card, a mapping, a playlist item, a hook, the API
// or a remote operation. The rule is about what is being launched, so
// nothing about who asked can get a launch past it.
func admitKioskLaunch(
	ctx context.Context, svc *ServiceContext, target platforms.ResolvedLaunch, final bool,
) error {
	lock := svc.State.Kiosk()
	if !lock.Locked() {
		return nil
	}

	systemID := kioskLaunchSystem(target, func(path string) (platforms.Launcher, error) {
		return helpers.FindLauncher(svc.Config, svc.Platform, path)
	})

	profileID := ""
	if active := svc.State.ActiveProfile(); active != nil {
		profileID = active.ProfileID
	}

	err := lock.CheckLaunch(ctx, profileID, systemID, target.Path, final)
	if err == nil {
		return nil
	}
	var refused *kiosk.RefusedError
	if errors.As(err, &refused) {
		notifyKioskRefusal(svc, refused)
	}
	return fmt.Errorf("kiosk: %w", err)
}

// admitKioskMediaInstall refuses a media download asked for by a token the
// lock has bounded. A card that names a URL would otherwise put a file of
// its author's choosing onto a locked device and start it.
func admitKioskMediaInstall(svc *ServiceContext, token *tokens.Token) error {
	lock := svc.State.Kiosk()
	if !lock.Locked() || token.AllowedCommands.Unrestricted() {
		return nil
	}
	profileID := ""
	if active := svc.State.ActiveProfile(); active != nil {
		profileID = active.ProfileID
	}
	refused := &kiosk.RefusedError{ProfileID: profileID, Reason: kiosk.ReasonDownload}
	notifyKioskRefusal(svc, refused)
	return fmt.Errorf("kiosk: %w", refused)
}

func notifyKioskRefusal(svc *ServiceContext, refused *kiosk.RefusedError) {
	log.Warn().
		Str("system", refused.SystemID).
		Str("reason", refused.Reason).
		Str("profile", refused.ProfileID).
		Msg("kiosk: launch refused")
	notifications.KioskLaunchRefused(svc.State.Notifications, &models.KioskLaunchRefusedParams{
		SystemID:  refused.SystemID,
		Path:      refused.Path,
		ProfileID: refused.ProfileID,
		Reason:    refused.Reason,
	})
}

// boundScannedToken narrows a token read from a card to the kiosk command
// policy. A scan that a stored mapping replaces is left alone: mappings can
// only be written while the device is unlocked, so the script that runs is
// one its owner chose, whatever the card itself says.
func boundScannedToken(svc *ServiceContext, token *tokens.Token, hasMapping bool) {
	if hasMapping {
		return
	}
	switch token.Source {
	case tokens.SourceReader, tokens.SourceGMC, tokens.SourceRemote:
		svc.State.Kiosk().BoundToken(token)
	}
}

// kioskSystemPermitted returns the system filter a command uses to narrow a
// pick across every system, or nil when the lock is not enforcing. The
// profile's scope is read once, the first time the command asks.
func kioskSystemPermitted(svc *ServiceContext) func(systemID string) bool {
	lock := svc.State.Kiosk()
	if !lock.Locked() {
		return nil
	}
	scope := sync.OnceValue(func() kiosk.SystemScope {
		profileID := ""
		if active := svc.State.ActiveProfile(); active != nil {
			profileID = active.ProfileID
		}
		return lock.SystemScope(profileID)
	})
	return func(systemID string) bool {
		current := scope()
		return current.Permits(systemID)
	}
}

// kioskLaunchSystem returns the system a launch is judged as. It is the
// system of whatever will actually run the media, never the one the script
// asked for: a card may name any system it likes, and a rule that allowed
// one system would otherwise open every other to a card that claimed it.
//
// A launcher already chosen is what runs. With none chosen the platform
// picks one from the path, so the same lookup is made here. Only when that
// finds nothing, and so nothing can launch, is the script's own system used,
// which leaves a refusal reading the way the card was written.
func kioskLaunchSystem(
	target platforms.ResolvedLaunch,
	findLauncher func(path string) (platforms.Launcher, error),
) string {
	if target.Launcher != nil && target.Launcher.SystemID != "" {
		return target.Launcher.SystemID
	}
	if target.Path != "" {
		if launcher, err := findLauncher(target.Path); err == nil && launcher.SystemID != "" {
			return launcher.SystemID
		}
	}
	return target.SystemID
}
