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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// Argument key names used in advarg struct tags and map lookups.
const (
	KeyWhen      = "when"
	KeyLauncher  = "launcher"
	KeySystem    = "system"
	KeyAction    = "action"
	KeyTags      = "tags"
	KeyMode      = "mode"
	KeyName      = "name"
	KeyPreNotice = "pre_notice"
)

// Action values for the action advanced argument.
const (
	// ActionRun is the default action - launch/play the media.
	ActionRun = "run"
	// ActionDetails shows the media details/info page instead of launching.
	ActionDetails = "details"
)

// ActionValues is the list of valid action values for validation tags.
const ActionValues = ActionRun + " " + ActionDetails

// Mode values for the mode advanced argument.
const (
	// ModeShuffle randomizes playlist order.
	ModeShuffle = "shuffle"
)

// ModeValues is the list of valid mode values for validation tags.
const ModeValues = ModeShuffle

// GlobalArgs contains advanced arguments available to all commands.
type GlobalArgs struct {
	// When controls conditional execution. If non-empty and falsy, command is skipped.
	When string `advarg:"when"`
}

// LaunchArgs contains advanced arguments for the launch command.
type LaunchArgs struct {
	GlobalArgs
	// Launcher overrides the default launcher by ID.
	Launcher string `advarg:"launcher" validate:"omitempty,launcher"` //nolint:revive // custom validator
	// System specifies the target system for path resolution.
	System string `advarg:"system" validate:"omitempty,system"` //nolint:revive // custom validator
	// Action specifies the launch action (run, details).
	Action string `advarg:"action" validate:"omitempty,oneof=run details"`
	// Name is the filename for remote file installation.
	Name string `advarg:"name"`
	// PreNotice is shown before remote file download.
	PreNotice string `advarg:"pre_notice"`
}

// LaunchRandomArgs contains advanced arguments for the launch.random command.
type LaunchRandomArgs struct {
	GlobalArgs
	// Launcher overrides the default launcher by ID.
	Launcher string `advarg:"launcher" validate:"omitempty,launcher"` //nolint:revive // custom validator
	// Action specifies the launch action (run, details).
	Action string `advarg:"action" validate:"omitempty,oneof=run details"`
	// Tags filters results by tag criteria.
	Tags []database.TagFilter `advarg:"tags"`
}

// LaunchSearchArgs contains advanced arguments for the launch.search command.
type LaunchSearchArgs struct {
	GlobalArgs
	// Launcher overrides the default launcher by ID.
	Launcher string `advarg:"launcher" validate:"omitempty,launcher"` //nolint:revive // custom validator
	// Action specifies the launch action (run, details).
	Action string `advarg:"action" validate:"omitempty,oneof=run details"`
	// Tags filters results by tag criteria.
	Tags []database.TagFilter `advarg:"tags"`
}

// LaunchTitleArgs contains advanced arguments for the launch.title command.
type LaunchTitleArgs struct {
	GlobalArgs
	// Launcher overrides the default launcher by ID.
	Launcher string `advarg:"launcher" validate:"omitempty,launcher"` //nolint:revive // custom validator
	// Tags filters results by tag criteria.
	Tags []database.TagFilter `advarg:"tags"`
}

// PlaylistArgs contains advanced arguments for playlist commands.
type PlaylistArgs struct {
	GlobalArgs
	// Mode controls playlist behavior (e.g., "shuffle").
	Mode string `advarg:"mode" validate:"omitempty,oneof=shuffle"`
}

// IsActionDetails returns true if the action is "details" (case-insensitive).
func IsActionDetails(action string) bool {
	return strings.EqualFold(action, ActionDetails)
}

// IsActionRun returns true if the action is "run" or empty (case-insensitive).
func IsActionRun(action string) bool {
	return action == "" || strings.EqualFold(action, ActionRun)
}

// IsModeShuffle returns true if the mode is "shuffle" (case-insensitive).
func IsModeShuffle(mode string) bool {
	return strings.EqualFold(mode, ModeShuffle)
}
