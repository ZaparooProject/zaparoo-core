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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// holdModeForToken reports whether removing this token should exit media.
// A token's own #tap or #hold trait wins, then the configured mode of the
// reader it came from, then the global readers.scan.mode.
func holdModeForToken(svc *ServiceContext, t *tokens.Token) bool {
	if t == nil {
		return svc.Config.HoldModeEnabled()
	}

	switch t.Traits.ScanMode() {
	case config.ScanModeHold:
		return true
	case config.ScanModeTap:
		return false
	}

	if t.ReaderID != "" {
		if r, ok := svc.State.GetReader(t.ReaderID); ok && r != nil {
			return svc.Config.HoldModeEnabledForReader(r.Metadata().ID, r.Path())
		}
	}

	return svc.Config.HoldModeEnabled()
}

// holdModeForTokenAfterRemoval resolves like holdModeForToken, except that a
// reader which has since disconnected keeps the decision made while it was
// still present.
//
// The exit timer is only armed when hold is enabled, and re-resolving when it
// fires is what catches the mode being turned off during the delay. A reader
// that disappeared in the meantime is lost information rather than a policy
// change: falling back to the global mode there would silently drop its
// per-reader override, so a hold reader on a tap device would arm the timer
// and then never exit the media it launched.
func holdModeForTokenAfterRemoval(svc *ServiceContext, t *tokens.Token) bool {
	if t != nil && t.Traits.ScanMode() == "" && t.ReaderID != "" {
		if _, ok := svc.State.GetReader(t.ReaderID); !ok {
			return true
		}
	}
	return holdModeForToken(svc, t)
}

// withHoldOwnerTraits returns removed carrying the hold owner's traits when
// removed is the token that owns the running media. The removal path only sees
// the token the preprocessor tracked, which never went through the script, so
// it needs the traits the launch was actually made under. The reader ID stays
// the one the token was removed from.
func withHoldOwnerTraits(st *state.State, removed *tokens.Token) tokens.Token {
	if removed == nil {
		return tokens.Token{}
	}

	policyToken := *removed
	if !policyToken.Traits.IsEmpty() {
		return policyToken
	}

	owner := st.GetSoftwareToken()
	if owner != nil && !owner.Traits.IsEmpty() && helpers.TokensEqual(removed, owner) {
		policyToken.Traits = owner.Traits
	}

	return policyToken
}
