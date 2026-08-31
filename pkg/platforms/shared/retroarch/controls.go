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
	"fmt"
	"net"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	commandSaveState   = "SAVE_STATE"
	commandLoadState   = "LOAD_STATE"
	commandToggleMenu  = "MENU_TOGGLE"
	commandTogglePause = "PAUSE_TOGGLE"
	commandReset       = "RESET"
	commandQuit        = "QUIT"
	commandFastForward = "FAST_FORWARD"
	commandRewind      = "REWIND"
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

	return map[string]platforms.Control{
		platforms.ControlSaveState:   control(commandSaveState),
		platforms.ControlLoadState:   control(commandLoadState),
		platforms.ControlToggleMenu:  control(commandToggleMenu),
		platforms.ControlTogglePause: control(commandTogglePause),
		platforms.ControlReset:       control(commandReset),
		platforms.ControlStop:        control(commandQuit),
		platforms.ControlFastForward: control(commandFastForward),
		platforms.ControlRewind:      control(commandRewind),
	}
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
