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

//go:build unix

package methods

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reaper counts what a file takes on disk, which for a small thumbnail is
// whole blocks, not its few bytes.
func TestThumbFileDiskBytes_UsesAllocatedBlocks(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "thumb.webp")
	require.NoError(t, os.WriteFile(path, []byte("tiny"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	want := info.Size()
	if st.Blocks > 0 {
		want = int64(st.Blocks) * 512 //nolint:unconvert // Blocks is not int64 on every unix
	}
	assert.Equal(t, want, thumbFileDiskBytes(info, 0))
}
