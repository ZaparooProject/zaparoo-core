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

package dbtemplate

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedBuildsOnceAndCopiesFilesWithTheirTimes(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	var builds atomic.Int32
	build := func(dir string) error {
		builds.Add(1)
		db := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(db, []byte("database"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "test.db.version"), []byte("33"), 0o600))
		require.NoError(t, os.Chtimes(db, stamp, stamp))
		return nil
	}

	for range 3 {
		dir := t.TempDir()
		Seed(t, t.Name(), dir, build)

		data, err := os.ReadFile(filepath.Join(dir, "test.db")) //nolint:gosec // the test's own temp dir
		require.NoError(t, err)
		assert.Equal(t, "database", string(data))
		info, err := os.Stat(filepath.Join(dir, "test.db"))
		require.NoError(t, err)
		assert.True(t, info.ModTime().Equal(stamp), "modification time is kept, got %v", info.ModTime())

		data, err = os.ReadFile(filepath.Join(dir, "sub", "test.db.version")) //nolint:gosec // the test's own temp dir
		require.NoError(t, err)
		assert.Equal(t, "33", string(data))
	}
	assert.Equal(t, int32(1), builds.Load())
}

func TestSeedFailsWhenBuildLeavesNothing(t *testing.T) {
	t.Parallel()

	_, err := buildTemplate(func(string) error { return nil })
	require.ErrorContains(t, err, "build left no files")
}
