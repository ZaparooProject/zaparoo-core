//go:build linux && !android

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

package batocera

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
)

// hotkeygenContextFile is where Batocera's hotkeygen daemon keeps the hotkeys
// of the running emulator. configgen writes it when a game starts and removes
// it when the game ends; without it hotkeygen uses the EmulationStation set.
const hotkeygenContextFile = "/var/run/hotkeygen.context"

const hotkeygenSendTimeout = 5 * time.Second

// hotkeyActions maps controls to the hotkeygen action that triggers them.
// hotkeygen translates an action into whatever key combination or command the
// running emulator uses for it, so one name covers libretro cores and
// standalone emulators alike. Emulators only bind the actions they support.
var hotkeyActions = map[string]string{
	platforms.ControlSaveState:   "save_state",
	platforms.ControlLoadState:   "restore_state",
	platforms.ControlToggleMenu:  "menu",
	platforms.ControlTogglePause: "pause",
	platforms.ControlReset:       "reset",
	platforms.ControlFastForward: "fastforward",
	platforms.ControlRewind:      "rewind",
	platforms.ControlNext:        "next_disk",
}

var (
	errNoEmulatorContext = errors.New("no emulator is running")
	errSlotUnsupported   = errors.New("save state slots are chosen by the emulator, not by slot")
)

// hotkeyContext is the part of hotkeygen's context file Core reads.
type hotkeyContext struct {
	Keys map[string]json.RawMessage `json:"keys"`
	Name string                     `json:"name"`
}

func readHotkeyContext(fs afero.Fs) (*hotkeyContext, error) {
	data, err := afero.ReadFile(fs, hotkeygenContextFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoEmulatorContext, err)
	}
	var ctx hotkeyContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("parse hotkeygen context: %w", err)
	}
	return &ctx, nil
}

// hotkeyControlNames lists every control the hotkey launchers define, in the
// order ControlNames reports them. Which of them the running emulator can
// perform is only known when a control runs.
func hotkeyControlNames() []string {
	names := make([]string, 0, len(hotkeyActions)+1)
	names = append(names, platforms.ControlStop)
	for control := range hotkeyActions {
		names = append(names, control)
	}
	sort.Strings(names)
	return names
}

// sendHotkey asks hotkeygen to fire action.
func sendHotkey(ctx context.Context, action string) error {
	ctx, cancel := context.WithTimeout(ctx, hotkeygenSendTimeout)
	defer cancel()
	//nolint:gosec // action is always a value from the fixed hotkeyActions table
	if err := exec.CommandContext(ctx, "hotkeygen", "--send", action).Run(); err != nil {
		return fmt.Errorf("hotkeygen send %s: %w", action, err)
	}
	return nil
}

// hotkeyControls builds the controls shared by every EmulationStation system
// launcher. A control fails when the running emulator has no hotkey for it,
// because hotkeygen would otherwise accept the action and do nothing.
func hotkeyControls(
	fs afero.Fs,
	send func(context.Context, string) error,
	stop func() error,
) map[string]platforms.Control {
	controls := make(map[string]platforms.Control, len(hotkeyActions)+1)
	for control, action := range hotkeyActions {
		controls[control] = platforms.Control{
			Func: func(ctx context.Context, _ *config.Instance, params platforms.ControlParams) error {
				if params.Args[platforms.ControlArgSlot] != "" {
					return errSlotUnsupported
				}
				hk, err := readHotkeyContext(fs)
				if err != nil {
					return err
				}
				if _, ok := hk.Keys[action]; !ok {
					return fmt.Errorf("%w: %s has no %s hotkey", platforms.ErrControlUnavailable, hk.Name, control)
				}
				return send(ctx, action)
			},
		}
	}
	controls[platforms.ControlStop] = platforms.Control{
		Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
			return stop()
		},
	}
	return controls
}
