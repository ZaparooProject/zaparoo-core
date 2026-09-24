//go:build !windows

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

package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxSocketPathBytes is the shortest sun_path any supported OS gives us: macOS
// and the BSDs allow 104 bytes including the terminator, Linux 108.
const maxSocketPathBytes = 103

// TempSocketPath returns a path to bind a Unix socket at, inside a directory
// removed when the test ends.
//
// It exists because t.TempDir is unusable for sockets: it builds the directory
// out of the test's own name, which on a macOS runner lands a socket path of
// roughly 105 bytes past the sun_path limit and fails the bind with
// "invalid argument". The limit is a property of the path, so whether a test
// binds at all depends on how long its name happens to be, and a neighbouring
// test with a shorter name passes.
func TempSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zap")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, name)
	require.LessOrEqual(t, len(socket), maxSocketPathBytes,
		"socket path is too long to bind; shorten it rather than relying on the runner's temporary directory")
	return socket
}
