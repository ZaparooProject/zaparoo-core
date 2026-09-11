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

package retroarch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	commandSaveState      = "SAVE_STATE"
	commandLoadState      = "LOAD_STATE"
	commandToggleMenu     = "MENU_TOGGLE"
	commandTogglePause    = "PAUSE_TOGGLE"
	commandReset          = "RESET"
	commandQuit           = "QUIT"
	commandFastForward    = "FAST_FORWARD"
	commandRewind         = "REWIND"
	commandToggleDiscTray = "DISK_EJECT_TOGGLE"
	commandNextDisc       = "DISK_NEXT"
	commandPreviousDisc   = "DISK_PREV"
)

// Controls returns RetroArch network-command controls for addr.
func Controls(addr string) map[string]platforms.Control {
	if addr == "" {
		return nil
	}

	control := func(command string) platforms.Control {
		return platforms.Control{
			Func: func(ctx context.Context, _ *config.Instance, _ platforms.ControlParams) error {
				return sendCommand(ctx, addr, command)
			},
		}
	}

	discControl := func(command string) platforms.Control {
		return platforms.Control{
			Func: func(ctx context.Context, _ *config.Instance, _ platforms.ControlParams) error {
				return sendDiscCommand(ctx, addr, command)
			},
		}
	}

	return map[string]platforms.Control{
		platforms.ControlSaveState:   control(commandSaveState),
		platforms.ControlLoadState:   control(commandLoadState),
		platforms.ControlToggleMenu:  control(commandToggleMenu),
		platforms.ControlTogglePause: control(commandTogglePause),
		platforms.ControlReset:       control(commandReset),
		platforms.ControlStop:        control(commandQuit),
		platforms.ControlFastForward: control(commandFastForward),
		platforms.ControlRewind:      control(commandRewind),
		platforms.ControlToggleTray:  discControl(commandToggleDiscTray),
		platforms.ControlNext:        discControl(commandNextDisc),
		platforms.ControlPrevious:    discControl(commandPreviousDisc),
	}
}

// sendDiscCommand checks that the NCI has content before sending a hotkey.
// RetroArch does not acknowledge disk hotkeys or expose the core's disk API,
// so a successful send cannot confirm that the core changed discs. Never retry
// these non-idempotent commands after an ambiguous network failure.
func sendDiscCommand(ctx context.Context, addr, command string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return fmt.Errorf("dial retroarch network command interface: %w", err)
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set retroarch command deadline: %w", err)
	}
	if _, err = conn.Write([]byte("GET_STATUS")); err != nil {
		return fmt.Errorf("query retroarch network command interface: %w", err)
	}
	var buf [4096]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return fmt.Errorf("retroarch network command interface unavailable: %w", err)
	}
	status := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(status, "GET_STATUS PLAYING ") && !strings.HasPrefix(status, "GET_STATUS PAUSED ") {
		return errors.New("retroarch has no active content or returned an invalid status")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("retroarch disc control canceled: %w", err)
	}
	if _, err := conn.Write([]byte(command)); err != nil {
		return fmt.Errorf("send retroarch command %s: %w", command, err)
	}
	return nil
}

func sendCommand(ctx context.Context, addr, command string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return fmt.Errorf("dial retroarch network command interface: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.Write([]byte(command)); err != nil {
		return fmt.Errorf("send retroarch command %s: %w", command, err)
	}
	return nil
}
