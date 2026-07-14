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
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/rs/zerolog/log"
)

var (
	ErrInputNotAllowed = errors.New("input key not allowed")
	ErrInputBlocked    = errors.New("input key blocked")
)

var defaultDesktopBlockList = []string{
	// Linux: TTY switching
	"{ctrl+alt+f1}", "{ctrl+alt+f2}", "{ctrl+alt+f3}", "{ctrl+alt+f4}",
	"{ctrl+alt+f5}", "{ctrl+alt+f6}", "{ctrl+alt+f7}",
	// Linux: system/shell access
	"{ctrl+alt+delete}", "{ctrl+alt+t}", "{alt+sysrq}", "{super}", "{meta}",
	// Windows: close application
	"{alt+f4}",
	// macOS: launcher/quit
	"{cmd+space}", "{cmd+q}",
}

func defaultInputMode(platformID string) string {
	switch platformID {
	case platformids.Mister, platformids.Mistex, platformids.Batocera,
		platformids.Recalbox, platformids.LibreELEC, platformids.RetroPie,
		platformids.ReplayOS:
		return config.InputModeUnrestricted
	default:
		return config.InputModeCombos
	}
}

func isDesktopPlatform(platformID string) bool {
	switch platformID {
	case platformids.Linux, platformids.Windows, platformids.Mac,
		platformids.SteamOS, platformids.ChimeraOS, platformids.Bazzite:
		return true
	default:
		return false
	}
}

// isSpecialKey returns true for braced keys with more than one inner character
// (e.g. {f1}, {enter}, {ctrl+q}). Single chars like "a" and "{a}" return false.
func isSpecialKey(key string) bool {
	if len(key) < 4 || key[0] != '{' || key[len(key)-1] != '}' {
		return false
	}
	inner := key[1 : len(key)-1]
	return len(inner) > 1
}

func isKeyInList(key string, list []string) bool {
	for _, item := range list {
		if strings.EqualFold(key, item) {
			return true
		}
	}
	return false
}

// checkInputKey returns an error if the key is not permitted under the current
// input config. Precedence: allow list → block list → mode check.
func checkInputKey(cfg *config.Instance, platformID, key string) error {
	// 1. Strict allow mode — overrides everything
	allowList := cfg.InputAllowList()
	if len(allowList) > 0 {
		if !isKeyInList(key, allowList) {
			return fmt.Errorf("%w: %s", ErrInputNotAllowed, key)
		}
		return nil
	}

	// 2. Block list check — nil means not configured (use defaults),
	// empty slice means explicitly cleared (block = [])
	blockList := cfg.InputBlockList()
	if blockList == nil && isDesktopPlatform(platformID) {
		blockList = defaultDesktopBlockList
	}
	if isKeyInList(key, blockList) {
		return fmt.Errorf("%w: %s", ErrInputBlocked, key)
	}

	// 3. Mode check
	mode := cfg.InputMode(defaultInputMode(platformID))
	switch mode {
	case config.InputModeCombos:
		if !isSpecialKey(key) {
			return fmt.Errorf("%w: %s", ErrInputNotAllowed, key)
		}
		return nil
	case config.InputModeUnrestricted:
		return nil
	default:
		log.Warn().Str("mode", mode).Msg("unknown input mode, defaulting to combos")
		if !isSpecialKey(key) {
			return fmt.Errorf("%w: %s", ErrInputNotAllowed, key)
		}
		return nil
	}
}

// DEPRECATED
//
//nolint:gocritic // single-use parameter in command handler
func cmdKey(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}
	legacyCode, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Cmd.Args[0])
	}
	code := keyboardmap.GetLegacyKey(legacyCode)
	if code == "" {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Cmd.Args[0])
	}
	if err := checkInputKey(env.Cfg, pl.ID(), code); err != nil {
		return platforms.CmdResult{}, err
	}
	if err := pl.KeyboardPress(code); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to press keyboard key: %w", err)
	}
	return platforms.CmdResult{}, nil
}

const defaultInterKeyDelay = 100 * time.Millisecond

// keyboardSequencer is an optional interface platforms may implement to handle a
// whole key sequence at once. Implementations get shift-batching, inline token
// interpretation (delay/hold/sigils), and sequence-scoped release-all;
// the per-key fallback loop is used otherwise.
type keyboardSequencer interface {
	KeyboardPressSequence(args []string, interKeyDelay time.Duration) error
}

// PressKeyboardSequence is shared between ZapScript commands and API handlers.
// interKeyDelay sets the gap between consecutive key presses; pass 0 to use
// the default (100 ms). If the platform implements keyboardSequencer, the full
// args slice is handed off; otherwise falls back to pressing each key
// individually.
func PressKeyboardSequence(pl platforms.Platform, args []string, interKeyDelay time.Duration) error {
	if interKeyDelay == 0 {
		interKeyDelay = defaultInterKeyDelay
	}
	if ks, ok := pl.(keyboardSequencer); ok {
		if err := ks.KeyboardPressSequence(args, interKeyDelay); err != nil {
			return fmt.Errorf("keyboard sequence: %w", err)
		}
		return nil
	}
	for _, name := range args {
		if err := pl.KeyboardPress(name); err != nil {
			return fmt.Errorf("failed to press keyboard key '%s': %w", name, err)
		}
		time.Sleep(interKeyDelay)
	}
	return nil
}

// PressGamepadSequence is shared between ZapScript commands and API handlers.
// interKeyDelay sets the gap between button presses; pass 0 to use the default.
func PressGamepadSequence(pl platforms.Platform, args []string, interKeyDelay time.Duration) error {
	if interKeyDelay == 0 {
		interKeyDelay = defaultInterKeyDelay
	}
	for _, name := range args {
		if err := pl.GamepadPress(name); err != nil {
			return fmt.Errorf("failed to press gamepad button '%s': %w", name, err)
		}
		time.Sleep(interKeyDelay)
	}
	return nil
}

// parseSpeedArg reads the optional ?speed= advanced arg and converts it to a
// time.Duration using parseMacroDuration. Returns 0 (use default) if unset.
//
//nolint:gocritic // single-use parameter in command handler
func parseSpeedArg(env platforms.CmdEnv) time.Duration {
	s := env.Cmd.AdvArgs.Get("speed")
	if s == "" {
		return 0
	}
	d, err := parseMacroDuration(s)
	if err != nil {
		log.Warn().Str("speed", s).Err(err).Msg("invalid speed arg, using default")
		return 0
	}
	return d
}

//nolint:gocritic // single-use parameter in command handler
func cmdKeyboard(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}

	for _, key := range env.Cmd.Args {
		if err := checkInputKey(env.Cfg, pl.ID(), key); err != nil {
			return platforms.CmdResult{}, err
		}
	}

	log.Info().Int("key_count", len(env.Cmd.Args)).Msg("keyboard input")

	if err := PressKeyboardSequence(pl, env.Cmd.Args, parseSpeedArg(env)); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdGamepad(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}

	for _, btn := range env.Cmd.Args {
		if err := checkInputKey(env.Cfg, pl.ID(), btn); err != nil {
			return platforms.CmdResult{}, err
		}
	}

	log.Info().Int("button_count", len(env.Cmd.Args)).Msg("gamepad input")

	if err := PressGamepadSequence(pl, env.Cmd.Args, parseSpeedArg(env)); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdInputText(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}

	for _, key := range env.Cmd.Args {
		if err := checkInputKey(env.Cfg, pl.ID(), key); err != nil {
			return platforms.CmdResult{}, err
		}
	}

	log.Info().Int("char_count", len(env.Cmd.Args)).Msg("text input")

	if err := PressKeyboardSequence(pl, env.Cmd.Args, 0); err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{}, nil
}

//nolint:gocritic // single-use parameter in command handler
func insertCoin(
	pl platforms.Platform,
	env platforms.CmdEnv,
	key string,
) (platforms.CmdResult, error) {
	var amount int

	if len(env.Cmd.Args) == 0 || env.Cmd.Args[0] != "" {
		amount = 1
	} else {
		var err error
		amount, err = strconv.Atoi(env.Cmd.Args[0])
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("invalid amount '%s': %w", env.Cmd.Args[0], err)
		}
	}

	for range amount {
		_ = pl.KeyboardPress(key)
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdCoinP1(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 1: %v", env.Cmd.Args)
	return insertCoin(pl, env, "5")
}

//nolint:gocritic // single-use parameter in command handler
func cmdCoinP2(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 2: %v", env.Cmd.Args)
	return insertCoin(pl, env, "6")
}

//nolint:gocritic // single-use parameter in command handler
func cmdCoinP3(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 3: %v", env.Cmd.Args)
	return insertCoin(pl, env, "7")
}

//nolint:gocritic // single-use parameter in command handler
func cmdCoinP4(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 4: %v", env.Cmd.Args)
	return insertCoin(pl, env, "8")
}
