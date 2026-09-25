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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/runfailure"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

func (m *manager) executeCommand(
	ctx context.Context, operationType string, raw json.RawMessage,
) operationResult {
	if ctx.Err() != nil {
		return failResult("execution_timeout")
	}

	var params struct {
		Value string `json:"value"`
	}
	if err := decodeParams(raw, &params); err != nil || !validCommandValue(params.Value) {
		return failResult("bad_params")
	}
	// A ZapLink is the one legitimate reason a value carries a scheme, and
	// only for launch: launch.system takes a system ID and mister.script a
	// script name, and neither is ever a URL. What the link resolves to is
	// bounded by commandPolicy, which the token carries, so the indirection
	// widens nothing. Every other scheme is refused here — smb:// and friends
	// reach the installer's fetch path, which writes to the device, and no
	// resolver stands between them and it.
	isLink := operationType == gozapscript.ZapScriptCmdLaunch && httpURLValue(params.Value)
	if !isLink && containsURLScheme(params.Value) {
		return failResult("bad_params")
	}
	command, err := buildStructuralCommand(operationType, params.Value, isLink)
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

	// The device's own launch policies apply to a remote operation the same
	// as to a scan. A remote launch reaches the runner directly rather than
	// through the token queue, so the gate the queue applies has to be asked
	// for here or it does not happen at all.
	if zapscript.IsMediaLaunchingCommand(command.Name) && m.deps.LaunchAdmission != nil {
		if admitErr := m.deps.LaunchAdmission(); admitErr != nil {
			if m.deps.State != nil {
				runfailure.Notify(m.deps.State.Notifications, &tokens.Token{
					Source: tokens.SourceRemote, Commands: []gozapscript.Command{command},
				}, admitErr, nil)
			}
			switch {
			case errors.Is(admitErr, state.ErrLaunchRequiresProfile):
				return failResult("profile_required")
			case errors.Is(admitErr, playtime.ErrLimitReached):
				return failResult("playtime_limit_reached")
			default:
				return failResult("execution_failed")
			}
		}
	}

	token := tokens.Token{
		ScanTime: time.Now(), Source: tokens.SourceRemote, Commands: []gozapscript.Command{command},
		AllowedCommands: commandPolicy,
	}
	if ctx.Err() != nil {
		return failResult("execution_timeout")
	}
	err = m.deps.RunZapScript(
		ctx, token, playlists.PlaylistController{Queue: m.deps.PlaylistQueue}, nil, false)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			return failResult("execution_timeout")
		case errors.Is(err, state.ErrLaunchInProgress), errors.Is(err, platforms.ErrScriptAlreadyRunning):
			return operationResult{Status: "busy"}
		case errors.Is(err, zapscript.ErrFileNotFound):
			return failResult("media_not_found")
		case errors.Is(err, state.ErrRunZapScriptDisabled):
			return failResult("disabled")
		default:
			return failResult("execution_failed")
		}
	}
	if ctx.Err() != nil {
		return failResult("execution_timeout")
	}
	return succeedResult(map[string]any{}, resultLimit)
}

func buildStructuralCommand(name, value string, isLink bool) (gozapscript.Command, error) {
	argument := value
	advanced := make(map[string]string)
	// A ZapLink is passed whole: '?' opens the URL's own query string, not
	// this command's advanced arguments. Splitting there would both corrupt
	// the link and let a remote command set `system` on a URL, which is what
	// sends launch to the installer's fetch path instead of the resolver.
	if index := strings.IndexByte(value, '?'); index >= 0 && !isLink {
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

// urlSchemePattern matches a valid URI scheme followed by a colon at a
// token boundary anywhere in a command value. Single-letter drive prefixes
// are filtered separately so Windows media paths remain valid.
//
//nolint:gochecknoglobals // compiled once
var urlSchemePattern = regexp.MustCompile(`(?i)(^|[^a-z0-9+.-])([a-z][a-z0-9+.-]*):`)

// httpURLValue reports whether value is wholly an http(s) URL, the only shape
// in which a remote command value may carry a scheme at all.
func httpURLValue(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func containsURLScheme(value string) bool {
	for _, match := range urlSchemePattern.FindAllStringSubmatchIndex(value, -1) {
		schemeStart, schemeEnd := match[4], match[5]
		isDrivePath := schemeEnd-schemeStart == 1 && schemeEnd+1 < len(value) &&
			(value[schemeEnd+1] == '/' || value[schemeEnd+1] == '\\') &&
			(schemeStart == 0 || value[schemeStart-1] == '/' || value[schemeStart-1] == '\\')
		if !isDrivePath {
			return true
		}
	}
	return false
}
