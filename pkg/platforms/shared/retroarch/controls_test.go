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

func TestDiscControls(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ action, command string }{
		{platforms.ControlToggleTray, "DISK_EJECT_TOGGLE"},
		{platforms.ControlNext, "DISK_NEXT"},
		{platforms.ControlPrevious, "DISK_PREV"},
	} {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			for _, status := range []string{
				"GET_STATUS PLAYING DOSBox-pure,game", "GET_STATUS PAUSED DOSBox-pure,game",
			} {
				listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
				require.NoError(t, err)
				t.Cleanup(func() { _ = listener.Close() })
				require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
				done := make(chan error, 1)
				go func() {
					control := Controls(listener.LocalAddr().String())[tt.action]
					done <- control.Func(t.Context(), nil, platforms.ControlParams{})
				}()
				buf := make([]byte, 128)
				n, peer, err := listener.ReadFromUDP(buf)
				require.NoError(t, err)
				assert.Equal(t, "GET_STATUS", string(buf[:n]))
				_, err = listener.WriteToUDP([]byte(status), peer)
				require.NoError(t, err)
				n, _, err = listener.ReadFromUDP(buf)
				require.NoError(t, err)
				assert.Equal(t, tt.command, string(buf[:n]))
				require.NoError(t, <-done)
			}
		})
	}
}

func TestDiscControlsUnavailable(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"GET_STATUS CONTENTLESS", "GET_STATUS ERROR", "garbage", "cancel", "timeout"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
			require.NoError(t, err)
			defer func() { _ = listener.Close() }()
			require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				control := Controls(listener.LocalAddr().String())[platforms.ControlNext]
				done <- control.Func(ctx, nil, platforms.ControlParams{})
			}()
			buf := make([]byte, 128)
			n, peer, err := listener.ReadFromUDP(buf)
			require.NoError(t, err)
			assert.Equal(t, "GET_STATUS", string(buf[:n]))
			switch status {
			case "cancel":
				cancel()
			case "timeout":
				// A silent listener must not be mistaken for a working NCI.
			default:
				_, err = listener.WriteToUDP([]byte(status), peer)
				require.NoError(t, err)
			}
			require.Error(t, <-done)
			// The sender has returned: no disc command should be queued.
			require.NoError(t, listener.SetReadDeadline(time.Now().Add(10*time.Millisecond)))
			_, _, err = listener.ReadFromUDP(buf)
			require.Error(t, err)
		})
	}
}

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
	require.Len(t, controls, len(commands)+3)

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
