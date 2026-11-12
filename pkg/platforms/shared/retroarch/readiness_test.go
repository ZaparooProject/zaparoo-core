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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveStatus answers each GET_STATUS datagram with the next reply, repeating
// the last one, and returns the address it listens on.
func serveStatus(t *testing.T, replies ...string) string {
	t.Helper()
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		buf := make([]byte, 128)
		for i := 0; ; i++ {
			_, peer, err := listener.ReadFromUDP(buf)
			if err != nil {
				return
			}
			reply := replies[min(i, len(replies)-1)]
			_, _ = listener.WriteToUDP([]byte(reply), peer)
		}
	}()
	return listener.LocalAddr().String()
}

func TestWaitForContent_ReadyWhenPlaying(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t, "GET_STATUS PLAYING super_nes,Game,crc32=1234abcd")

	err := waitForContent(addr, clockwork.NewFakeClock())(t.Context(), nil, nil)

	require.NoError(t, err)
}

func TestWaitForContent_PreviousGameIsNotReady(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t,
		"GET_STATUS PLAYING super_nes,Old,crc32=11111111",
		"GET_STATUS PLAYING super_nes,Old,crc32=11111111",
		"GET_STATUS PLAYING super_nes,New,crc32=22222222",
	)
	clock := clockwork.NewFakeClock()
	media := models.NewActiveMedia("SNES", "SNES", "/roms/snes/Other.sfc", "Other", "RetroArch")
	done := make(chan error, 1)
	go func() { done <- waitForContent(addr, clock)(t.Context(), nil, media) }()

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(readyPollInterval)
	}

	require.NoError(t, <-done)
}

func TestWaitForContent_LaunchedGameByNameIsReadyAtOnce(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t, "GET_STATUS PLAYING super_nes,Game,crc32=1234abcd")
	media := models.NewActiveMedia("SNES", "SNES", "/roms/snes/game.sfc", "Game", "RetroArch")

	require.NoError(t, waitForContent(addr, clockwork.NewFakeClock())(t.Context(), nil, media))
}

func TestWaitForContent_PausedCountsAsLoaded(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t, "GET_STATUS PAUSED super_nes,Game,crc32=1234abcd")

	require.NoError(t, waitForContent(addr, clockwork.NewFakeClock())(t.Context(), nil, nil))
}

func TestWaitForContent_WaitsForContent(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t,
		"GET_STATUS CONTENTLESS",
		"GET_STATUS CONTENTLESS",
		"GET_STATUS PLAYING super_nes,Game,crc32=1234abcd",
	)
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- waitForContent(addr, clock)(t.Context(), nil, nil) }()

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(readyPollInterval)
	}

	require.NoError(t, <-done)
}

func TestWaitForContent_NoContentIsNotUnavailable(t *testing.T) {
	t.Parallel()

	addr := serveStatus(t, "GET_STATUS CONTENTLESS")
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- waitForContent(addr, clock)(ctx, nil, nil) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWaitForContent_NothingListeningIsUnavailable(t *testing.T) {
	t.Parallel()

	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	addr := listener.LocalAddr().String()
	require.NoError(t, listener.Close())

	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- waitForContent(addr, clock)(ctx, nil, nil) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, platforms.ErrReadinessUnavailable)
}

func TestNewLauncher_ReadinessNeedsNetworkCommands(t *testing.T) {
	t.Parallel()

	core := CoreLaunch{ID: "RetroArchSNES", SystemID: "SNES", Core: "snes9x_libretro.so"}

	withNCI := NewLauncher(Options{NetworkCmdAddr: "127.0.0.1:55355"}, core)
	assert.NotNil(t, withNCI.WaitForReady)

	withoutNCI := NewLauncher(Options{}, core)
	assert.Nil(t, withoutNCI.WaitForReady, "no network command interface to ask")
}
