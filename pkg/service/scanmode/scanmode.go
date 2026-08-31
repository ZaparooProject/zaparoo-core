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

// Package scanmode resolves the scan mode that applies to a token: its own
// #tap or #hold trait, then the configured mode of the reader it came from,
// then the global readers.scan.mode.
//
// It sits below both the service loop that acts on the answer and the API
// method that reports it, so a client is never told a policy other than the
// one removal will apply.
package scanmode

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// ForToken returns the scan mode in force for t, always "tap" or "hold".
func ForToken(cfg *config.Instance, st *state.State, t *tokens.Token) string {
	if t == nil {
		return cfg.GlobalScanMode()
	}

	if mode := t.Traits.ScanMode(); mode != "" {
		return mode
	}

	if t.ReaderID != "" {
		if r, ok := st.GetReader(t.ReaderID); ok && r != nil {
			return cfg.ScanModeForReader(r.Metadata().ID, r.Path())
		}
	}

	return cfg.GlobalScanMode()
}

// ForTokenAfterRemoval resolves like ForToken, except that a reader which has
// since disconnected keeps the decision made while it was still present.
//
// The exit timer is only armed when hold is enabled, and re-resolving when it
// fires is what catches the mode being turned off during the delay. A reader
// that disappeared in the meantime is lost information rather than a policy
// change: falling back to the global mode there would silently drop its
// per-reader override, so a hold reader on a tap device would arm the timer
// and then never exit the media it launched.
//
// The API reports this same answer, so `readers` never claims tap for an owner
// whose removal will exit.
func ForTokenAfterRemoval(cfg *config.Instance, st *state.State, t *tokens.Token) string {
	if t != nil && t.Traits.ScanMode() == "" && t.ReaderID != "" {
		if _, ok := st.GetReader(t.ReaderID); !ok {
			return config.ScanModeHold
		}
	}
	return ForToken(cfg, st, t)
}

// HoldForToken reports whether removing t should exit media.
func HoldForToken(cfg *config.Instance, st *state.State, t *tokens.Token) bool {
	return ForToken(cfg, st, t) == config.ScanModeHold
}

// HoldForTokenAfterRemoval reports whether an armed exit timer should still
// exit media when it fires.
func HoldForTokenAfterRemoval(cfg *config.Instance, st *state.State, t *tokens.Token) bool {
	return ForTokenAfterRemoval(cfg, st, t) == config.ScanModeHold
}
