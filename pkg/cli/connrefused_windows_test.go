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
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is the platform the check was wrong on. A refused connect here returns
// the Winsock error, while syscall.ECONNREFUSED is a placeholder in the
// synthetic APPLICATION_ERROR range that no socket call produces, and Errno.Is
// maps nothing between them. Comparing against only the POSIX name therefore
// never matched, so "Core is not running" logged at Error and was reported.
//
// The test dials for real rather than building the error by hand, because the
// error a dial actually returns is the thing that has to match.
func TestIsConnectionRefused_MatchesTheWinsockError(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	var dialer net.Dialer
	_, dialErr := dialer.DialContext(t.Context(), "tcp", addr)
	require.Error(t, dialErr, "nothing should be listening on a closed port")

	require.NotErrorIs(t, dialErr, syscall.ECONNREFUSED,
		"the POSIX name alone is what used to be compared, and it does not match here")
	assert.True(t, isConnectionRefused(dialErr))
	assert.True(t, isConnectionRefused(fmt.Errorf("calling update.status: %w", dialErr)))

	assert.False(t, isConnectionRefused(errors.New("something else")))
	assert.False(t, isConnectionRefused(nil))
}
