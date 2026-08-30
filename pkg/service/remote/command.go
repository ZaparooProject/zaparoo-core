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

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

func (m *manager) executeCommand(
	ctx context.Context, operationType string, raw json.RawMessage,
) operationResult {
	var params struct {
		Value string `json:"value"`
	}
	if err := decodeParams(raw, &params); err != nil || !validCommandValue(params.Value) {
		return failResult("bad_params")
	}
	// None of the three structural verbs ever legitimately take a URL as
	// their value (a system ID, media path, or script name never is one),
	// and the launch command would install-fetch an http(s) or smb URL onto
	// the device. The Online API rejects any scheme before an operation is
	// created; this repeats that check locally so a bypassed or compromised
	// API still can't reach the fetch path, and gives a clean bad_params
	// error instead of a downstream failure.
	if containsURLScheme(params.Value) {
		return failResult("bad_params")
	}
	command, err := buildStructuralCommand(operationType, params.Value)
	if err != nil {
		return failResult("bad_params")
	}
	// The mister.script ZapScript command itself validates the script name
	// (.sh suffix, no path traversal) and checks it exists under the MiSTer
	// scripts directory, and platform.ForwardCmd already errors on platforms
	// that don't support it. This check exists to surface that failure as
	// the specific "unsupported" code instead of a generic execution_failed.
	if operationType == "mister.script" && !strings.EqualFold(m.deps.Platform.ID(), "mister") {
		return failResult("unsupported")
	}

	token := tokens.Token{
		ScanTime: time.Now(), Source: tokens.SourceRemote, Commands: []gozapscript.Command{command},
	}
	err = m.deps.RunZapScript(
		ctx, token, playlists.PlaylistController{Queue: m.deps.PlaylistQueue}, nil, false)
	if err != nil {
		switch {
		case errors.Is(err, state.ErrLaunchInProgress):
			return operationResult{Status: "busy"}
		case errors.Is(err, zapscript.ErrFileNotFound):
			return failResult("media_not_found")
		case errors.Is(err, state.ErrRunZapScriptDisabled):
			return failResult("disabled")
		default:
			return failResult("execution_failed")
		}
	}
	return succeedResult(map[string]any{}, resultLimit)
}

func buildStructuralCommand(name, value string) (gozapscript.Command, error) {
	argument := value
	advanced := make(map[string]string)
	if index := strings.IndexByte(value, '?'); index >= 0 {
		argument = value[:index]
		query, err := url.ParseQuery(value[index+1:])
		if err != nil {
			return gozapscript.Command{}, fmt.Errorf("parse remote command advanced arguments: %w", err)
		}
		for key, values := range query {
			if key == "" || len(values) != 1 {
				return gozapscript.Command{}, errors.New("invalid remote command advanced argument")
			}
			advanced[key] = values[0]
		}
	}
	if argument == "" {
		return gozapscript.Command{}, errors.New("remote command argument is empty")
	}
	return gozapscript.Command{
		Name: name, Args: []string{argument}, AdvArgs: gozapscript.NewAdvArgs(advanced),
	}, nil
}

func validCommandValue(value string) bool {
	if value == "" || strings.Contains(value, "**") || strings.Contains(value, "||") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// urlSchemePattern matches a URL scheme anywhere in a command value, the
// same pattern the Online API applies to a launch value at creation.
//
//nolint:gochecknoglobals // compiled once
var urlSchemePattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://`)

func containsURLScheme(value string) bool {
	return urlSchemePattern.MatchString(value)
}
