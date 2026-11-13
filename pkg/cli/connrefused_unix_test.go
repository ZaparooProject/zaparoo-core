//go:build !linux && !windows

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
	"testing"

	"github.com/stretchr/testify/require"
)

// refusedDial dials a loopback port that refuses connections and returns the
// error. A bound socket that is not listening does not refuse a connection
// here the way it does on Linux, so the port is one that was listened on and
// closed. Another test can be handed that port before the dial reaches it, in
// which case the dial succeeds and a new port is tried.
func refusedDial(t *testing.T) error {
	t.Helper()
	var lc net.ListenConfig
	var dialer net.Dialer
	for range 20 {
		listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := listener.Addr().String()
		require.NoError(t, listener.Close())

		conn, dialErr := dialer.DialContext(t.Context(), "tcp", addr)
		if dialErr != nil {
			return dialErr //nolint:wrapcheck // the test needs the error exactly as the dial returned it
		}
		_ = conn.Close()
	}
	require.FailNow(t, "every closed port was taken by something else before the dial")
	return nil
}
