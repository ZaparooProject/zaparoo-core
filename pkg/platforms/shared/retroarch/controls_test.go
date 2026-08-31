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
	"net"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlsEmptyAddress(t *testing.T) {
	t.Parallel()
	assert.Nil(t, Controls(""))
}

func TestControlsSendUDPCommands(t *testing.T) {
	t.Parallel()

	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, listener.Close())
	}()

	commands := map[string]string{
		platforms.ControlSaveState:   commandSaveState,
		platforms.ControlLoadState:   commandLoadState,
		platforms.ControlToggleMenu:  commandToggleMenu,
		platforms.ControlTogglePause: commandTogglePause,
		platforms.ControlReset:       commandReset,
		platforms.ControlStop:        commandQuit,
		platforms.ControlFastForward: commandFastForward,
		platforms.ControlRewind:      commandRewind,
	}
	controls := Controls(listener.LocalAddr().String())
	require.Len(t, controls, len(commands))

	for action, want := range commands {
		control := controls[action]
		require.NotNil(t, control.Func)
		require.NoError(t, control.Func(context.Background(), nil, platforms.ControlParams{}))

		require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))
		buf := make([]byte, 64)
		n, _, readErr := listener.ReadFromUDP(buf)
		require.NoError(t, readErr)
		assert.Equal(t, want, string(buf[:n]))
	}
}
