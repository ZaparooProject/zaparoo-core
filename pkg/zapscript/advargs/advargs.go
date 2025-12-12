// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

// Package advargs provides type-safe parsing and validation for ZapScript
// advanced arguments using struct tags and the go-playground/validator library.
package advargs

import (
	"strings"

	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
)

// IsActionDetails returns true if the action is "details" (case-insensitive).
func IsActionDetails(action string) bool {
	return strings.EqualFold(action, advargtypes.ActionDetails)
}

// IsActionRun returns true if the action is "run" or empty (case-insensitive).
func IsActionRun(action string) bool {
	return action == "" || strings.EqualFold(action, advargtypes.ActionRun)
}

// IsModeShuffle returns true if the mode is "shuffle" (case-insensitive).
func IsModeShuffle(mode string) bool {
	return strings.EqualFold(mode, advargtypes.ModeShuffle)
}
