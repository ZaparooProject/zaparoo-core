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

package mistermain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recentBinaryEntry builds one raw 1536-byte MiSTer recent-file record:
// 1024 bytes directory, 256 bytes name, 256 bytes label, NUL-padded.
func recentBinaryEntry(directory, name, label string) []byte {
	entry := make([]byte, 1024+256+256)
	copy(entry[:1024], directory)
	copy(entry[1024:1280], name)
	copy(entry[1280:1536], label)
	return entry
}

func TestReadRecent_MultipleEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test_recent.cfg")
	data := append(
		recentBinaryEntry("games/NES", "Game1.nes", "Game 1"),
		recentBinaryEntry("games/SNES", "Game2.sfc", "Game 2")...,
	)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	entries, err := ReadRecent(path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, RecentEntry{Directory: "games/NES", Name: "Game1.nes", Label: "Game 1"}, entries[0])
	assert.Equal(t, RecentEntry{Directory: "games/SNES", Name: "Game2.sfc", Label: "Game 2"}, entries[1])
}

func TestReadRecent_StopsAtZeroedRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test_recent.cfg")
	data := append(
		recentBinaryEntry("games/NES", "Game1.nes", "Game 1"),
		make([]byte, 1024+256+256)...,
	)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	entries, err := ReadRecent(path)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a zeroed (cleared) slot must not be read as a record")
}

// A recent file caught mid-write by MiSTer (MakeFile truncates before
// rewriting) can end with a partial final record. That must be silently
// discarded, not returned as a garbage entry or treated as a read error.
func TestReadRecent_DiscardsTruncatedFinalRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test_recent.cfg")
	full := recentBinaryEntry("games/NES", "Game1.nes", "Game 1")
	truncated := full[:900] // less than one full 1536-byte record
	data := append(append([]byte{}, full...), truncated...)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	entries, err := ReadRecent(path)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the truncated trailing record must be discarded, not returned")
	assert.Equal(t, "Game1.nes", entries[0].Name)
}

func TestReadRecent_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := ReadRecent(filepath.Join(t.TempDir(), "missing.cfg"))
	require.Error(t, err)
}

func TestReadRecent_EmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.cfg")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	entries, err := ReadRecent(path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
