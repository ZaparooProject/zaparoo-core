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

package groovyproxy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartAcceptsZapscriptFromGroovyCore(t *testing.T) {
	lc := net.ListenConfig{}
	coreConn, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:32105")
	if err != nil {
		t.Skipf("Groovy Core GMC port unavailable: %v", err)
	}
	defer func() { _ = coreConn.Close() }()

	proxyPort := 0
	beaconInterval := "10ms"
	defaults := config.BaseDefaults
	defaults.Groovy.GmcProxyPort = &proxyPort
	defaults.Groovy.GmcProxyBeaconInterval = &beaconInterval
	cfg, err := config.NewConfig(t.TempDir(), defaults)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	itq := make(chan tokens.Token)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Start(cfg, st, itq)
	}()
	t.Cleanup(func() {
		st.StopService()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("GMC proxy did not stop after context cancellation")
		}
	})

	buf := make([]byte, 1)
	require.NoError(t, coreConn.SetReadDeadline(time.Now().Add(time.Second)))
	_, coreProxyAddr, err := coreConn.ReadFrom(buf)
	require.NoError(t, err)

	_, err = coreConn.WriteTo([]byte("zapscript:**input.keyboard:{f2}"), coreProxyAddr)
	require.NoError(t, err)

	select {
	case token := <-itq:
		assert.Equal(t, "**input.keyboard:{f2}", token.Text)
		assert.Equal(t, tokens.SourceGMC, token.Source)
		assert.False(t, token.ScanTime.IsZero())
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for GMC zapscript token")
	}
}

func TestStartStopsWhenZapscriptTokenSendBlocked(t *testing.T) {
	lc := net.ListenConfig{}
	coreConn, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:32105")
	if err != nil {
		t.Skipf("Groovy Core GMC port unavailable: %v", err)
	}
	defer func() { _ = coreConn.Close() }()

	proxyPort := 0
	beaconInterval := "10ms"
	defaults := config.BaseDefaults
	defaults.Groovy.GmcProxyPort = &proxyPort
	defaults.Groovy.GmcProxyBeaconInterval = &beaconInterval
	cfg, err := config.NewConfig(t.TempDir(), defaults)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	itq := make(chan tokens.Token)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Start(cfg, st, itq)
	}()

	buf := make([]byte, 1)
	require.NoError(t, coreConn.SetReadDeadline(time.Now().Add(time.Second)))
	_, coreProxyAddr, err := coreConn.ReadFrom(buf)
	require.NoError(t, err)

	_, err = coreConn.WriteTo([]byte("zapscript:**input.keyboard:{f2}"), coreProxyAddr)
	require.NoError(t, err)

	st.StopService()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GMC proxy did not stop while zapscript token send was blocked")
	}
}
