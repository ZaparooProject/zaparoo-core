//go:build linux && !android

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

package gamehandoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nextEvent(t *testing.T, events <-chan Kind) Kind {
	t.Helper()
	select {
	case kind, ok := <-events:
		require.True(t, ok, "channel closed")
		return kind
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
		return 0
	}
}

func TestWatch_ReportsOpenAndClose(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.md")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	events, err := Watch(t.Context(), path)
	require.NoError(t, err)

	f, err := os.Open(path) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, Open, nextEvent(t, events))

	require.NoError(t, f.Close())
	assert.Equal(t, Close, nextEvent(t, events))
}

func TestWatch_IgnoresWritesAndCountsEachOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.md")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	events, err := Watch(t.Context(), path)
	require.NoError(t, err)

	// A write open is not a read of the game.
	w, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, Open, nextEvent(t, events))
	require.NoError(t, w.Close())

	for range 2 {
		r, err := os.Open(path) //nolint:gosec // test path
		require.NoError(t, err)
		assert.Equal(t, Open, nextEvent(t, events))
		require.NoError(t, r.Close())
		assert.Equal(t, Close, nextEvent(t, events))
	}
}

func TestWatch_ClosesChannelWhenContextEnds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.md")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	events, err := Watch(ctx, path)
	require.NoError(t, err)

	cancel()

	select {
	case _, ok := <-events:
		assert.False(t, ok)
	case <-time.After(5 * time.Second):
		t.Fatal("channel was not closed")
	}
}

func TestWatch_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := Watch(t.Context(), filepath.Join(t.TempDir(), "absent.md"))
	require.Error(t, err)
}

func TestWatch_ReportsReads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.md")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	events, err := Watch(t.Context(), path)
	require.NoError(t, err)

	_, err = os.ReadFile(path) //nolint:gosec // test path
	require.NoError(t, err)

	got := []Kind{nextEvent(t, events), nextEvent(t, events), nextEvent(t, events)}
	assert.Equal(t, []Kind{Open, Access, Close}, got)
}
