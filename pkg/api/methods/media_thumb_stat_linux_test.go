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

package methods

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A replacement that keeps a file's size and modification time, as
// rsync -t and archive extraction do, still changes its key through the
// status change time.
func TestFileThumbSourceKey_ChangesWhenOnlyTheChangeTimeDoes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "boxart.png")
	require.NoError(t, os.WriteFile(path, []byte("art"), 0o600))
	before, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("new"), 0o600))
	require.NoError(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Size(), after.Size())
	require.True(t, before.ModTime().Equal(after.ModTime()))
	if thumbFileChangeNanos(after) == thumbFileChangeNanos(before) {
		t.Skip("the temp filesystem's change time did not advance")
	}

	assert.NotEqual(t, fileThumbSourceKey(path, before), fileThumbSourceKey(path, after))
}
