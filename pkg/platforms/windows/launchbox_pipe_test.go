//go:build windows

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

package windows

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectFakePlugin attaches one end of an in-memory pipe as the plugin
// connection and returns the plugin's end.
func connectFakePlugin(t *testing.T, server *LaunchBoxPipeServer) net.Conn {
	t.Helper()
	core, plugin := net.Pipe()
	t.Cleanup(func() {
		_ = core.Close()
		_ = plugin.Close()
	})
	server.connMu.Lock()
	server.conn = core
	server.writer = bufio.NewWriter(core)
	server.connMu.Unlock()
	return plugin
}

// waitErr runs fn and fails the test if it does not return promptly.
func waitErr(t *testing.T, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "call blocked on a plugin that is not reading")
		return nil
	}
}

// A plugin that stops reading, such as LaunchBox showing a modal dialog, must
// not hold the connection lock forever. Every later launch, stop and
// connection check would queue behind the stuck write (#1895).
func TestLaunchBoxPipeServer_StalledPluginDoesNotBlockCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		send func(s *LaunchBoxPipeServer) error
		name string
	}{
		{name: "launch", send: func(s *LaunchBoxPipeServer) error { return s.LaunchGame("game-1") }},
		{name: "ping", send: func(s *LaunchBoxPipeServer) error { return s.sendPing() }},
		{name: "platforms", send: func(s *LaunchBoxPipeServer) error { return s.RequestPlatforms() }},
		{name: "stop", send: func(s *LaunchBoxPipeServer) error {
			return s.StopGame(context.Background(), "game-1")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := NewLaunchBoxPipeServer()
			server.writeTimeout = 20 * time.Millisecond
			server.setHandshake("1.3.1", launchBoxStopProtocolVersion)
			connectFakePlugin(t, server)

			require.Error(t, waitErr(t, func() error { return tt.send(server) }))

			// The broken connection is dropped, so the next caller is told
			// the plugin is gone instead of waiting on it.
			assert.False(t, server.IsConnected())
			err := waitErr(t, func() error { return server.LaunchGame("game-2") })
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not connected")
		})
	}
}

func TestLaunchBoxPipeServer_LaunchGameWritesOneCommandLine(t *testing.T) {
	t.Parallel()

	server := NewLaunchBoxPipeServer()
	plugin := connectFakePlugin(t, server)

	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(plugin)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	require.NoError(t, waitErr(t, func() error { return server.LaunchGame("game-1") }))

	var line string
	select {
	case line = <-lines:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "plugin received no command")
	}
	var cmd pluginCommand
	require.NoError(t, json.Unmarshal([]byte(line), &cmd))
	assert.Equal(t, pluginCommand{Command: "Launch", ID: "game-1"}, cmd)
	assert.True(t, server.IsConnected())
}
