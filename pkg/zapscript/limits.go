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

package zapscript

import (
	"errors"
	"fmt"
)

// MaxScriptLength bounds ZapScript text accepted from any untrusted source,
// measured in bytes.
//
// Parse cost grows with the length of the text, so an unbounded script is a
// way to occupy the single token worker and, once stored, to make every later
// history read expensive. The limit sits well above anything legitimate: an
// NTAG216, the largest tag in common use, holds 888 bytes.
const MaxScriptLength = 8192

// ErrScriptTooLong is returned for text over MaxScriptLength. Such text is
// rejected before it is parsed or stored.
var ErrScriptTooLong = errors.New("zapscript exceeds maximum length")

// ValidateScriptLength rejects ZapScript text longer than MaxScriptLength.
// Callers must apply it at the point untrusted text enters the system, before
// the text reaches a parser.
func ValidateScriptLength(text string) error {
	if len(text) > MaxScriptLength {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrScriptTooLong, len(text), MaxScriptLength)
	}
	return nil
}

// Limits on how far one run may grow by expanding commands into scripts, as
// include and ZapLinks do. Either can name a script that names itself, so
// without a bound a single scan could hold the token worker forever.
const (
	// MaxScriptExpansions is how many includes and ZapLinks one run may
	// expand in total. It also bounds how deeply they can nest.
	MaxScriptExpansions = 16
	// MaxExpandedCommands is how many commands expansions may add to a run.
	MaxExpandedCommands = 256
	// MaxExpandedBytes is how much script text expansions may read in a run.
	MaxExpandedBytes = 64 << 10
)

// ErrExpansionLimit is returned when expanding a script would take a run past
// one of the expansion limits.
var ErrExpansionLimit = errors.New("script expansion limit reached")

// ExpansionBudget tracks what one run has spent on expansions. A run's
// commands execute one at a time, so it is not safe for concurrent use and
// does not need to be.
type ExpansionBudget struct {
	expansions int
	commands   int
	bytes      int
}

// Spend charges one expansion that adds cmds commands from size bytes of
// script text. Nothing is charged when it would exceed a limit.
func (b *ExpansionBudget) Spend(cmds, size int) error {
	switch {
	case b.expansions >= MaxScriptExpansions:
		return fmt.Errorf("%w: more than %d includes or links", ErrExpansionLimit, MaxScriptExpansions)
	case b.commands+cmds > MaxExpandedCommands:
		return fmt.Errorf("%w: more than %d expanded commands", ErrExpansionLimit, MaxExpandedCommands)
	case b.bytes+size > MaxExpandedBytes:
		return fmt.Errorf("%w: more than %d bytes of expanded script", ErrExpansionLimit, MaxExpandedBytes)
	}
	b.expansions++
	b.commands += cmds
	b.bytes += size
	return nil
}
