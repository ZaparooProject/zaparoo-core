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

package mister

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/spf13/afero"
)

const (
	misterMenuKey       = "{f12}"
	misterUserButtonKey = "{lctrl+lalt+ralt}"
	misterSaveStateKey  = "{alt+f%d}"
	misterLoadStateKey  = "{f%d}"

	// MiSTer's savestate hotkeys address four slots.
	minSavestateSlot = 1
	maxSavestateSlot = 4

	// Modes of the cheat control. Main resets every cheat on each game load.
	cheatModeOn     = "on"
	cheatModeOff    = "off"
	cheatModeToggle = "toggle"
	cheatModeClear  = "clear"
)

// savestateCores holds the names of cores that declare savestate support, as
// Main reports them in /tmp/RBFNAME. Main's savestate hotkeys do nothing in any
// other core, and F1-F4 are ordinary keys in computer cores, so save_state and
// load_state must not be sent blind.
//
// Every entry but the 2P variants has an "SS" entry in its CONF_STR on the
// core repository's master branch (MiSTer-devel/<Name>_MiSTer). GAMEBOY2P and
// GBA2P ship without a public repository; they are included because Main keeps
// savestate folders for them on a real device. Computer cores that also
// declare "SS" (Apple-II, Oric, TRS-80) are left out for the F1-F4 conflict.
var savestateCores = map[string]struct{}{
	"NES": {}, "SNES": {}, "SMS": {}, "SGB": {}, "N64": {}, "PSX": {},
	"GAMEBOY": {}, "GAMEBOY2P": {}, "GBA": {}, "GBA2P": {},
	"AtariLynx": {}, "WonderSwan": {}, "NGPC": {}, "VirtualBoy": {}, "Loopy": {},
	"Cave": {}, "M72": {}, "TaitoF2": {}, "IGSPGM": {},
}

// errNoSavestates reports save_state or load_state on a core without them.
var errNoSavestates = errors.New("the running core does not support save states")

// coreControls are the controls shared by every FPGA core launcher. They send
// the same hotkeys a keyboard would, through the platform's virtual keyboard.
type coreControls struct {
	pl          platforms.Platform
	readRBFName func() (string, error)
	main        *mainCommands
}

func newCoreControls(pl platforms.Platform) *coreControls {
	return &coreControls{
		pl:          pl,
		readRBFName: mistermain.ReadRBFName,
		main:        newMainCommands(afero.NewOsFs()),
	}
}

func (c *coreControls) controls() map[string]platforms.Control {
	press := func(key string) platforms.ControlFunc {
		return func(context.Context, *config.Instance, platforms.ControlParams) error {
			return c.pl.KeyboardPress(key)
		}
	}
	controls := map[string]platforms.Control{
		platforms.ControlToggleMenu: {Func: press(misterMenuKey)},
		platforms.ControlReset:      {Func: press(misterUserButtonKey)},
		platforms.ControlSaveState:  {Func: c.savestate(misterSaveStateKey)},
		platforms.ControlLoadState:  {Func: c.savestate(misterLoadStateKey)},
		platforms.ControlStop: {Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
			if err := c.pl.StopActiveLauncher(platforms.StopForMenu); err != nil {
				return fmt.Errorf("stop active core: %w", err)
			}
			return nil
		}},
	}
	// The cheat and save controls need the Zaparoo Main fork. They are only
	// listed when the running Main says it has them, and each call checks
	// again in case Main changed since the launchers were built.
	if c.main != nil {
		if c.main.supports(mainCommandCheat) {
			controls[platforms.ControlCheat] = platforms.Control{Func: c.cheat}
		}
		if c.main.supports(mainCommandSave) {
			controls[platforms.ControlSaveRAM] = platforms.Control{Func: c.saveRAM}
		}
	}
	return controls
}

// cheat changes one of the running game's cheats, by the name of its file in
// the cheat zip (the extension may be left off), or turns all of them off. Main
// reports a name it cannot match, and a game with no cheats, only in its own
// log. Hardcore RetroAchievements mode blocks cheats inside Main.
func (c *coreControls) cheat(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
	mode := strings.ToLower(strings.TrimSpace(params.Args[platforms.ControlArgMode]))
	if mode == "" {
		mode = cheatModeOn
	}
	rawName, hasName := params.Args[platforms.ControlArgName]

	var args []string
	switch mode {
	case cheatModeClear:
		if strings.TrimSpace(rawName) != "" {
			return fmt.Errorf("%w: cheat mode %q takes no name", platforms.ErrInvalidControlArg, mode)
		}
		args = []string{cheatModeClear}
	case cheatModeOn, cheatModeOff, cheatModeToggle:
		if !hasName {
			return fmt.Errorf("%w: cheat needs a name", platforms.ErrInvalidControlArg)
		}
		name, err := mainCommandArg(rawName)
		if err != nil {
			return fmt.Errorf("%w: cheat name: %w", platforms.ErrInvalidControlArg, err)
		}
		args = []string{mode, name}
	default:
		return fmt.Errorf("%w: cheat mode %q must be on, off, toggle or clear",
			platforms.ErrInvalidControlArg, mode)
	}
	return c.sendMain(mainCommandCheat, args...)
}

// saveRAM makes the running core write its save, such as battery RAM or a
// memory card, to the SD card without opening the OSD.
func (c *coreControls) saveRAM(context.Context, *config.Instance, platforms.ControlParams) error {
	return c.sendMain(mainCommandSave)
}

func (c *coreControls) sendMain(command string, args ...string) error {
	if err := c.main.send(command, args...); err != nil {
		if errors.Is(err, errMainCommandUnsupported) {
			return fmt.Errorf("%w: %w", err, platforms.ErrControlUnavailable)
		}
		return err
	}
	return nil
}

func (c *coreControls) savestate(keyFormat string) platforms.ControlFunc {
	return func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
		slot, err := platforms.SlotArg(params, minSavestateSlot, maxSavestateSlot)
		if err != nil {
			return fmt.Errorf("save state: %w", err)
		}
		name, err := c.readRBFName()
		if err != nil {
			return fmt.Errorf("read running core: %w", err)
		}
		if _, ok := savestateCores[name]; !ok {
			return fmt.Errorf("%w (%s): %w", errNoSavestates, name, platforms.ErrControlUnavailable)
		}
		return c.pl.KeyboardPress(fmt.Sprintf(keyFormat, slot))
	}
}

// withCoreControls attaches the shared FPGA core controls to every launcher in
// ls that starts a core. Launchers that already define controls keep them.
func withCoreControls(pl platforms.Platform, ls []platforms.Launcher) []platforms.Launcher {
	cc := newCoreControls(pl)
	for i := range ls {
		if ls[i].Launch == nil || len(ls[i].Controls) > 0 {
			continue
		}
		ls[i].Controls = cc.controls()
	}
	return ls
}
