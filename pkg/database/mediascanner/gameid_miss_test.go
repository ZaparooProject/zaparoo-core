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

package mediascanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newGameIDMissMockDB() *testhelpers.MockMediaDBI {
	mockDB := &testhelpers.MockMediaDBI{}
	mockDB.On("HasMediaPropertyForPath", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(false, nil)
	return mockDB
}

// reloadGameIDMisses stores what misses recorded and returns the record the
// next index would load.
func reloadGameIDMisses(t *testing.T, misses *gameIDMisses) *gameIDMisses {
	t.Helper()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	require.NoError(t, misses.save(ctx, db.MediaDB, systemdefs.SystemPSX))
	return loadGameIDMisses(ctx, db.MediaDB, systemdefs.SystemPSX, misses.listing)
}

// A disc image with no game ID is read once. The next index trusts the record
// while the file keeps its size and modification time, and reads the file
// again as soon as either changes.
func TestGameIDMisses_UnreadableDiscIsReadOnceUntilItChanges(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "game.iso")
	valid := minimalScannerPSXISO("SCES-01420", "SCES_014.20;1")
	require.NoError(t, os.WriteFile(path, make([]byte, len(valid)), 0o600))
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, stamp, stamp))
	mockDB := newGameIDMissMockDB()

	first := &gameIDMisses{next: map[string]gameIDFileState{}}
	props, unresolved := stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path, first)
	require.Empty(t, props)
	require.False(t, unresolved, "a disc that was read and has no ID is settled")

	// Same size and time, different contents: only a fresh read would find
	// the ID, so finding none shows the record was trusted.
	require.NoError(t, os.WriteFile(path, valid, 0o600))
	require.NoError(t, os.Chtimes(path, stamp, stamp))
	second := reloadGameIDMisses(t, first)
	props, unresolved = stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path, second)
	require.Empty(t, props)
	require.False(t, unresolved)

	later := stamp.Add(time.Hour)
	require.NoError(t, os.Chtimes(path, later, later))
	third := reloadGameIDMisses(t, second)
	props, unresolved = stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path, third)
	require.Len(t, props, 1, "a changed file must be read again")
	assert.Equal(t, "SCES-01420", props[0].Text)
	assert.False(t, unresolved)
	assert.Empty(t, third.next, "an identified disc is no longer a miss")
}

// A disc that could not be opened says nothing about its contents, so it is
// not recorded and the system is reconciled again next time.
func TestGameIDMisses_UnopenableDiscStaysUnresolved(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.iso")
	misses := &gameIDMisses{next: map[string]gameIDFileState{}}

	props, unresolved := stagedPropertiesFromPath(newGameIDMissMockDB(), systemdefs.SystemPSX, path, misses)

	assert.Empty(t, props)
	assert.True(t, unresolved)
	assert.Empty(t, misses.next)
}

// A cue sheet whose track file is missing cannot be read however often it is
// tried, so it is settled against the system's current files and read again
// only when those change.
func TestGameIDMisses_MissingTrackIsSettledUntilTheFilesChange(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "game.cue")
	require.NoError(t, os.WriteFile(path,
		[]byte("FILE \"game.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"), 0o600))
	mockDB := newGameIDMissMockDB()

	first := &gameIDMisses{next: map[string]gameIDFileState{}, listing: "walk-1"}
	props, unresolved := stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path, first)
	require.Empty(t, props)
	require.False(t, unresolved, "a disc naming a missing file is settled while the files stay as they are")
	require.Equal(t, "walk-1", first.next[path].Listing)

	same := reloadGameIDMisses(t, first)
	require.True(t, same.isKnown(path))

	changed := reloadGameIDMisses(t, first)
	changed.listing = "walk-2"
	assert.False(t, changed.isKnown(path), "a change to the system's files must have the disc read again")

	untrusted := &gameIDMisses{next: map[string]gameIDFileState{}}
	_, unresolved = stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path, untrusted)
	assert.True(t, unresolved, "without a trustworthy walk the missing file settles nothing")
}

// A record written by other identification code is not trusted.
func TestGameIDMisses_RecordFromAnotherVersionIsIgnored(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	require.NoError(t, db.MediaDB.SetScanGameIDMisses(ctx, systemdefs.SystemPSX,
		`{"files":{"/games/psx/a.iso":{"size":1,"modTime":2}},"version":"0/older"}`))

	misses := loadGameIDMisses(ctx, db.MediaDB, systemdefs.SystemPSX, "")

	assert.Empty(t, misses.known)
	require.NoError(t, misses.save(ctx, db.MediaDB, systemdefs.SystemPSX))
	stored, err := db.MediaDB.ScanGameIDMisses(ctx, systemdefs.SystemPSX)
	require.NoError(t, err)
	assert.Empty(t, stored, "a record with nothing left in it is removed")
}

// A system holding a disc image with no game ID used to be reconciled by
// every index, because the image was read again each time and never settled.
func TestScanSkip_SystemWithUnidentifiedDiscIsLeftAlone(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	fixture := newScanSkipFixtureFor(t, ".iso", systemdefs.SystemPSX)
	fixture.index(t)
	require.Equal(t, []string{"Alpha Game"}, fixture.titleNames(t, systemdefs.SystemPSX))
	stored, err := fixture.db.MediaDB.ScanGameIDMisses(context.Background(), systemdefs.SystemPSX)
	require.NoError(t, err)
	require.Contains(t, stored, "Alpha Game.iso")

	fixture.breakTitles(t, systemdefs.SystemPSX)
	fixture.index(t)

	assert.Equal(t, []string{"Edited Behind The Scanner"}, fixture.titleNames(t, systemdefs.SystemPSX),
		"an unchanged system must not be reconciled")
}

// A system with no files has no row to reconcile, but used to open and commit
// a transaction on every index to find that out.
func TestScanSkip_EmptySystemIsRecordedAndLeftAlone(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	fixture := newScanSkipFixtureFor(t, ".bin", systemdefs.SystemNES)
	require.NoError(t, os.Remove(filepath.Join(fixture.dirs[systemdefs.SystemNES], "Alpha Game.bin")))
	fixture.index(t)

	marker, err := fixture.db.MediaDB.ScanMarker(context.Background(), systemdefs.SystemNES)
	require.NoError(t, err)
	require.NotEmpty(t, marker, "an empty walk is still a walk the next index can match")

	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.dirs[systemdefs.SystemNES], "Beta Game.bin"), []byte("x"), 0o600))
	fixture.index(t)
	assert.Equal(t, []string{"Beta Game"}, fixture.titleNames(t, systemdefs.SystemNES),
		"a file added to an empty system must be indexed")
}
