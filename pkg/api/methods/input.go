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

package methods

import (
	"errors"
	"fmt"

	zapscriptlib "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

func parseInputMacro(cmdName, macro string) ([]string, error) {
	script, err := zapscriptlib.NewParser("**" + cmdName + ":" + macro).ParseScript()
	if err != nil {
		return nil, fmt.Errorf("invalid input macro: %w", err)
	}
	if len(script.Cmds) == 0 {
		return nil, errors.New("invalid input macro: no commands parsed")
	}
	return script.Cmds[0].Args, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleInputKeyboard(env requests.RequestEnv) (any, error) {
	var params models.InputKeyboardParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	args, err := parseInputMacro(zapscriptlib.ZapScriptCmdInputKeyboard, params.Keys)
	if err != nil {
		return nil, err
	}

	log.Info().Strs("keys", args).Msg("keyboard input via API")

	if err := zapscript.PressKeyboardSequence(env.Platform, args); err != nil {
		return nil, fmt.Errorf("keyboard press failed: %w", err)
	}

	return NoContent{}, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleInputGamepad(env requests.RequestEnv) (any, error) {
	var params models.InputGamepadParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	args, err := parseInputMacro(zapscriptlib.ZapScriptCmdInputGamepad, params.Buttons)
	if err != nil {
		return nil, err
	}

	log.Info().Strs("buttons", args).Msg("gamepad input via API")

	if err := zapscript.PressGamepadSequence(env.Platform, args); err != nil {
		return nil, fmt.Errorf("gamepad press failed: %w", err)
	}

	return NoContent{}, nil
}
