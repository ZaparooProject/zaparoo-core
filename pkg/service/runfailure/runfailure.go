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

// Package runfailure maps the error a ZapScript run ends with onto the stable
// category and safe message every client-facing report of that run shares:
// the run method's error response and the run.failed notification.
package runfailure

import (
	"errors"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
)

// CommandError is the failure of one command in a script. It names the
// command so a report of the run can say which step failed, and unwraps to the
// cause so errors.Is keeps matching the sentinels.
type CommandError struct {
	Err  error
	Name string
}

func (e *CommandError) Error() string {
	return "failed to run zapscript command: " + e.Err.Error()
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

// Notify sends run.failed for a run of t that ended with err. pls is the
// playlist the run was an item of, or nil. The script is the token's own text,
// redacted as token history stores it; a token that carries parsed commands
// instead of text reports those. A script over the length bound is left out:
// it was never run, and redacting it would mean parsing it.
func Notify(ns chan<- models.Notification, t *tokens.Token, err error, pls *playlists.Playlist) {
	category, message := Classify(err)
	payload := &models.RunFailedParams{
		Source:   t.Source,
		ReaderID: t.ReaderID,
		Category: category,
		Message:  message,
	}
	script := t.Text
	if script == "" && len(t.Commands) > 0 {
		parts := make([]string, 0, len(t.Commands))
		for _, cmd := range t.Commands {
			parts = append(parts, cmd.String())
		}
		script = strings.Join(parts, "||")
	}
	if zapscript.ValidateScriptLength(script) == nil {
		payload.Script, _ = zapscript.RedactToken(script, t.Data)
	}
	var cmdErr *CommandError
	if errors.As(err, &cmdErr) {
		payload.Command = cmdErr.Name
	}
	if pls != nil {
		index := pls.Index
		payload.PlaylistID = pls.ID
		payload.PlaylistIndex = &index
	}
	notifications.RunFailed(ns, payload)
}

// Classify returns the category and message for a terminal run error. The
// message never carries a filesystem path or token contents; the cause stays
// with the caller for logging and errors.Is.
func Classify(err error) (category, message string) {
	switch {
	case errors.Is(err, platforms.ErrScriptAlreadyRunning):
		return models.ErrorCategoryBusy, "a script is already running"
	case errors.Is(err, state.ErrLaunchInProgress),
		errors.Is(err, state.ErrMediaLaunchInProgress):
		return models.ErrorCategoryBusy, "another launch is in progress"
	case errors.Is(err, zapscript.ErrFileNotFound),
		errors.Is(err, titles.ErrNoMatch),
		errors.Is(err, titles.ErrLowConfidence):
		return models.ErrorCategoryMediaNotFound, "media not found"
	case errors.Is(err, state.ErrRunZapScriptDisabled):
		return models.ErrorCategoryDisabled, "ZapScript execution is disabled"
	case errors.Is(err, zapscript.ErrScriptTooLong):
		// The length error names only the limit and the size, so its own text
		// is safe and tells the caller which bound was exceeded.
		return models.ErrorCategoryInvalidScript, err.Error()
	case errors.Is(err, zapscript.ErrInvalidScript),
		errors.Is(err, zapscript.ErrUnknownCommand),
		errors.Is(err, zapscript.ErrUnsupportedControlAction),
		errors.Is(err, systemdefs.ErrUnknownSystem),
		errors.Is(err, state.ErrInvalidNextAction):
		return models.ErrorCategoryInvalidScript, "ZapScript is invalid"
	case errors.Is(err, zapscript.ErrCommandBlocked),
		errors.Is(err, zapscript.ErrExecuteNotAllowed),
		errors.Is(err, zapscript.ErrHTTPNotAllowed),
		errors.Is(err, zapscript.ErrRemoteSource),
		errors.Is(err, state.ErrLaunchBlockedByHook),
		errors.Is(err, state.ErrLaunchRequiresProfile):
		return models.ErrorCategoryBlocked, "ZapScript execution was blocked"
	case errors.Is(err, playtime.ErrLimitReached):
		return models.ErrorCategoryPlaytimeLimit, "playtime limit reached"
	default:
		return models.ErrorCategoryExecutionFailed, "ZapScript execution failed"
	}
}
