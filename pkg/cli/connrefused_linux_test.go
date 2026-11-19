//go:build linux

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

package cli

import (
	"net"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// refusedDial dials a loopback port that refuses connections and returns the
// error. The port is bound but never listened on, so Linux refuses the
// connection and nothing else can take the port; a port that is listened on
// and then closed can be handed to another test before the dial reaches it.
func refusedDial(t *testing.T) error {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Close(fd) })
	require.NoError(t, syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}))
	bound, err := syscall.Getsockname(fd)
	require.NoError(t, err)
	inet4, ok := bound.(*syscall.SockaddrInet4)
	require.True(t, ok)

	var dialer net.Dialer
	conn, dialErr := dialer.DialContext(t.Context(), "tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(inet4.Port)))
	if dialErr == nil {
		_ = conn.Close()
	}
	require.Error(t, dialErr, "nothing is listening on a bound socket")
	return dialErr //nolint:wrapcheck // the test needs the error exactly as the dial returned it
}
