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

package database_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flagSet = map[database.MediaUserFlag]bool

// newDiscSetTestDB indexes a three-disc PSX set and returns its discs in path
// order.
func newDiscSetTestDB(t *testing.T) (*database.Database, []database.DiscSetMember) {
	t.Helper()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	dir := filepath.Join("roms", "PSX", "Game")
	paths := []string{
		filepath.Join(dir, "Game (Disc 1).chd"),
		filepath.Join(dir, "Game (Disc 2).chd"),
		filepath.Join(dir, "Game (Disc 3).chd"),
	}
	scantest.IndexMediaPaths(t, mediaDB, "PSX", paths...)
	rows, err := mediaDB.GetMediaBySystemID("PSX")
	require.NoError(t, err)
	byPath := make(map[string]int64, len(rows))
	for i := range rows {
		byPath[rows[i].Path] = rows[i].DBID
	}
	discs := make([]database.DiscSetMember, 0, len(paths))
	for _, path := range paths {
		require.NotZero(t, byPath[path], "disc %s is indexed", path)
		discs = append(discs, database.DiscSetMember{Path: path, MediaDBID: byPath[path]})
	}
	return &database.Database{MediaDB: mediaDB, UserDB: userDB}, discs
}

func discStored(t *testing.T, db *database.Database, path string) flagSet {
	t.Helper()
	row, _, err := db.UserDB.GetMediaUserData("PSX", path)
	require.NoError(t, err)
	got := make(flagSet)
	for _, flag := range database.MediaUserFlags {
		if row.Flag(flag) {
			got[flag] = true
		}
	}
	return got
}

// requireSetInStep checks that MediaDB shows exactly what UserDB holds on
// every disc.
func requireSetInStep(t *testing.T, db *database.Database, discs []database.DiscSetMember) {
	t.Helper()
	for _, disc := range discs {
		require.Equal(t, discStored(t, db, disc.Path), projectedUserTags(t, db, disc.MediaDBID), disc.Path)
	}
}

func applyToSet(
	t *testing.T, db *database.Database, discs []database.DiscSetMember, launch int, changes flagSet,
) flagSet {
	t.Helper()
	written, err := database.ApplyMediaUserFlagsToSet(
		context.Background(), db, "PSX", discs, discs[launch].Path, changes,
	)
	require.NoError(t, err)
	return written
}

func TestApplyMediaUserFlagsToSetAddsToTheLaunchDisc(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)

	written := applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagFavorite: true})
	assert.Equal(t, flagSet{database.MediaUserFlagFavorite: true}, written)
	assert.Empty(t, discStored(t, db, discs[0].Path))
	assert.Equal(t, flagSet{database.MediaUserFlagFavorite: true}, discStored(t, db, discs[1].Path))
	assert.Empty(t, discStored(t, db, discs[2].Path))
	requireSetInStep(t, db, discs)
}

func TestApplyMediaUserFlagsToSetLeavesAFlagASiblingHolds(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	applyToSet(t, db, discs, 2, flagSet{database.MediaUserFlagFavorite: true})
	before, err := db.UserDB.ListMediaUserFlagTimes(database.MediaUserFlagFavorite)
	require.NoError(t, err)
	require.Len(t, before, 1)
	revision, _, err := db.UserDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)

	written := applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagFavorite: true})
	assert.Empty(t, written, "the set already is a favorite")
	assert.Empty(t, discStored(t, db, discs[0].Path), "the launch disc gets no copy")
	after, err := db.UserDB.ListMediaUserFlagTimes(database.MediaUserFlagFavorite)
	require.NoError(t, err)
	assert.Equal(t, before, after, "when the set became a favorite is kept")
	afterRevision, _, err := db.UserDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	assert.Equal(t, revision, afterRevision, "nothing was written")
}

func TestApplyMediaUserFlagsToSetRemovesFromEveryDisc(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	// Library sync can leave a flag on more than one disc.
	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagFavorite: true, database.MediaUserFlagPlayLater: true})
	_, err := database.ApplyMediaUserFlags(context.Background(), db, "PSX", discs[2].Path, discs[2].MediaDBID,
		flagSet{database.MediaUserFlagFavorite: true})
	require.NoError(t, err)

	written := applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagFavorite: false})
	assert.Equal(t, flagSet{database.MediaUserFlagFavorite: false}, written)
	assert.Equal(t, flagSet{database.MediaUserFlagPlayLater: true}, discStored(t, db, discs[0].Path))
	assert.Empty(t, discStored(t, db, discs[1].Path))
	assert.Empty(t, discStored(t, db, discs[2].Path))
	requireSetInStep(t, db, discs)

	// Removing a flag no disc holds changes nothing.
	assert.Empty(t, applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagLiked: false}))
}

func TestApplyMediaUserFlagsToSetClearsOppositesOnEveryDisc(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[0].Path, database.MediaUserFlagLiked, true))
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[1].Path, database.MediaUserFlagFavorite, true))
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[2].Path, database.MediaUserFlagLiked, true))

	// Disliking the set takes back every like and favorite.
	applyToSet(t, db, discs, 2, flagSet{database.MediaUserFlagDisliked: true})
	assert.Empty(t, discStored(t, db, discs[0].Path))
	assert.Empty(t, discStored(t, db, discs[1].Path))
	assert.Equal(t, flagSet{database.MediaUserFlagDisliked: true}, discStored(t, db, discs[2].Path))
	requireSetInStep(t, db, discs)

	// Liking it again takes back the dislike, wherever it sits.
	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagLiked: true})
	assert.Equal(t, flagSet{database.MediaUserFlagLiked: true}, discStored(t, db, discs[0].Path))
	assert.Empty(t, discStored(t, db, discs[2].Path))

	// A favorite clears a sibling's dislike too.
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[1].Path, database.MediaUserFlagDisliked, true))
	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagFavorite: true})
	assert.Equal(t, flagSet{database.MediaUserFlagLiked: true, database.MediaUserFlagFavorite: true},
		discStored(t, db, discs[0].Path))
	assert.Empty(t, discStored(t, db, discs[1].Path))
	requireSetInStep(t, db, discs)
}

func TestApplyMediaUserFlagsToSetHidesOnlyTheLaunchDisc(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	_, err := database.ApplyMediaUserFlags(context.Background(), db, "PSX", discs[0].Path, discs[0].MediaDBID,
		flagSet{database.MediaUserFlagHidden: true})
	require.NoError(t, err)

	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagHidden: true})
	assert.Equal(t, flagSet{database.MediaUserFlagHidden: true}, discStored(t, db, discs[0].Path))
	assert.Equal(t, flagSet{database.MediaUserFlagHidden: true}, discStored(t, db, discs[1].Path))
	assert.Empty(t, discStored(t, db, discs[2].Path))

	// Unhiding does not reach the other discs either.
	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagHidden: false})
	assert.Equal(t, flagSet{database.MediaUserFlagHidden: true}, discStored(t, db, discs[0].Path))
	assert.Empty(t, discStored(t, db, discs[1].Path))
	requireSetInStep(t, db, discs)
}

func TestApplyMediaUserFlagsToSetRefusesBadRequests(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	ctx := context.Background()

	_, err := database.ApplyMediaUserFlagsToSet(ctx, db, "PSX", discs, discs[0].Path,
		flagSet{database.MediaUserFlagLiked: true, database.MediaUserFlagDisliked: true})
	require.ErrorIs(t, err, database.ErrMediaUserFlagConflict)
	_, err = database.ApplyMediaUserFlagsToSet(ctx, db, "PSX", discs, discs[0].Path, flagSet{"bogus": true})
	require.Error(t, err)
	_, err = database.ApplyMediaUserFlagsToSet(ctx, db, "PSX", discs,
		filepath.Join("roms", "PSX", "Other.chd"), flagSet{database.MediaUserFlagLiked: true})
	require.Error(t, err)
	for _, disc := range discs {
		assert.Empty(t, discStored(t, db, disc.Path))
	}
}

func TestApplyMediaUserFlagsToSetWritesUnindexedDiscsToUserDBOnly(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	unindexed := []database.DiscSetMember{
		{Path: filepath.Join("roms", "PSX", "Gone", "Gone (Disc 1).chd")},
		{Path: filepath.Join("roms", "PSX", "Gone", "Gone (Disc 2).chd")},
	}
	// The media database is busy, but nothing here needs it.
	coordinator, err := database.GetMediaDBWriteCoordinator(db.MediaDB)
	require.NoError(t, err)
	lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)
	defer lease.Release()

	written := applyToSet(t, db, unindexed, 1, flagSet{database.MediaUserFlagFavorite: true})
	assert.Empty(t, written, "no MediaDB tag changed")
	assert.Equal(t, flagSet{database.MediaUserFlagFavorite: true}, discStored(t, db, unindexed[1].Path))
	_, found, err := db.UserDB.GetDeviceState(database.DeviceStateKeyMediaUserFlagWrite)
	require.NoError(t, err)
	assert.False(t, found, "no projection, no marker")
	for _, disc := range discs {
		assert.Empty(t, projectedUserTags(t, db, disc.MediaDBID))
	}
}

func TestApplyMediaUserFlagsToSetRefusedDuringLongMediaWrite(t *testing.T) {
	t.Parallel()
	db, discs := newDiscSetTestDB(t)
	coordinator, err := database.GetMediaDBWriteCoordinator(db.MediaDB)
	require.NoError(t, err)
	lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)

	_, err = database.ApplyMediaUserFlagsToSet(context.Background(), db, "PSX", discs, discs[0].Path,
		flagSet{database.MediaUserFlagFavorite: true})
	require.ErrorIs(t, err, database.ErrMediaWriteConflict)
	for _, disc := range discs {
		assert.Empty(t, discStored(t, db, disc.Path), "a refused write must not reach UserDB")
	}

	lease.Release()
	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagFavorite: true})
	requireSetInStep(t, db, discs)
}

// batchMarkerProbeUserDB records whether the flag write marker was saved when
// the batch reached UserDB.
type batchMarkerProbeUserDB struct {
	database.UserDBI
	savedBeforeWrite *bool
}

func (m *batchMarkerProbeUserDB) SetMediaUserFlags(writes []database.MediaUserFlagWrite) error {
	_, found, err := m.GetDeviceState(database.DeviceStateKeyMediaUserFlagWrite)
	if err != nil {
		return err //nolint:wrapcheck // test passthrough
	}
	*m.savedBeforeWrite = found
	return m.UserDBI.SetMediaUserFlags(writes) //nolint:wrapcheck // test passthrough
}

// failingTagsMediaDB fails every MediaDB tag write, as a process killed
// after the UserDB write would leave the projection behind.
type failingTagsMediaDB struct {
	database.MediaDBI
}

func (failingTagsMediaDB) UpdateMediaTags(
	context.Context, int64, []database.MediaTagRef, []database.MediaTagRef,
) error {
	return errors.New("killed")
}

func TestApplyMediaUserFlagsToSetMarksTheWriteUntilProjected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, discs := newDiscSetTestDB(t)
	realUserDB := db.UserDB
	saved := false
	db.UserDB = &batchMarkerProbeUserDB{UserDBI: realUserDB, savedBeforeWrite: &saved}
	marker := func() bool {
		t.Helper()
		_, found, err := realUserDB.GetDeviceState(database.DeviceStateKeyMediaUserFlagWrite)
		require.NoError(t, err)
		return found
	}

	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagLiked: true})
	assert.True(t, saved, "the marker is saved before UserDB changes")
	assert.False(t, marker(), "a projected write leaves nothing saved")

	// A marker left by an earlier write survives a later one.
	require.NoError(t, realUserDB.SetDeviceState(database.DeviceStateKeyMediaUserFlagWrite, "1"))
	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagPlayLater: true})
	assert.True(t, marker())
	require.NoError(t, realUserDB.DeleteDeviceState(database.DeviceStateKeyMediaUserFlagWrite))

	// A write that stops between UserDB and MediaDB keeps its marker, and
	// the reconcile on the next start brings every disc in line.
	broken := &database.Database{UserDB: realUserDB, MediaDB: failingTagsMediaDB{MediaDBI: db.MediaDB}}
	_, err := database.ApplyMediaUserFlagsToSet(ctx, broken, "PSX", discs, discs[2].Path,
		flagSet{database.MediaUserFlagDisliked: true})
	require.Error(t, err)
	assert.True(t, marker(), "the failed projection leaves the marker")
	assert.Equal(t, flagSet{database.MediaUserFlagDisliked: true}, discStored(t, db, discs[2].Path))
	assert.Equal(t, flagSet{database.MediaUserFlagLiked: true}, projectedUserTags(t, db, discs[0].MediaDBID),
		"MediaDB is behind")

	require.NoError(t, database.ReconcileMediaUserData(ctx, db))
	assert.False(t, marker(), "the reconcile clears the marker it answered")
	requireSetInStep(t, db, discs)
}

func TestApplyMediaUserFlagsToSetRetryProjectsAfterAFailedProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, discs := newDiscSetTestDB(t)
	applyToSet(t, db, discs, 0, flagSet{database.MediaUserFlagLiked: true})

	broken := &database.Database{UserDB: db.UserDB, MediaDB: failingTagsMediaDB{MediaDBI: db.MediaDB}}
	_, err := database.ApplyMediaUserFlagsToSet(ctx, broken, "PSX", discs, discs[2].Path,
		flagSet{database.MediaUserFlagDisliked: true})
	require.Error(t, err)
	assert.Equal(t, flagSet{database.MediaUserFlagLiked: true}, projectedUserTags(t, db, discs[0].MediaDBID),
		"MediaDB is behind")

	// The same request again finds UserDB already holding it. It still
	// brings MediaDB in line, with no restart in between.
	written, err := database.ApplyMediaUserFlagsToSet(ctx, db, "PSX", discs, discs[2].Path,
		flagSet{database.MediaUserFlagDisliked: true})
	require.NoError(t, err)
	assert.Equal(t, map[database.MediaUserFlag]bool{
		database.MediaUserFlagLiked:    false,
		database.MediaUserFlagDisliked: true,
	}, written)
	requireSetInStep(t, db, discs)
}

func TestApplyMediaUserFlagsToSetTellsTheListenerPerDisc(t *testing.T) {
	t.Parallel()
	db, changes := notifyingDB(t)
	dir := filepath.Join("roms", "PSX", "Game")
	discs := []database.DiscSetMember{
		{Path: filepath.Join(dir, "Game (Disc 1).chd")},
		{Path: filepath.Join(dir, "Game (Disc 2).chd")},
	}
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[0].Path, database.MediaUserFlagFavorite, true))
	require.NoError(t, db.UserDB.SetMediaUserFlag("PSX", discs[1].Path, database.MediaUserFlagFavorite, true))

	// Only discs that change are reported, each once.
	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagPlayLater: true})
	require.Len(t, *changes, 1)
	assert.Equal(t, discs[1].Path, (*changes)[0].path)
	assert.Equal(t, []database.TagInfo{{Type: "user", Tag: "favorite"}, {Type: "user", Tag: "playlater"}},
		(*changes)[0].tags)

	*changes = nil
	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagFavorite: false})
	require.Len(t, *changes, 2)
	got := map[string][]database.TagInfo{(*changes)[0].path: (*changes)[0].tags, (*changes)[1].path: (*changes)[1].tags}
	assert.Equal(t, map[string][]database.TagInfo{
		discs[0].Path: {},
		discs[1].Path: {{Type: "user", Tag: "playlater"}},
	}, got)

	*changes = nil
	applyToSet(t, db, discs, 1, flagSet{database.MediaUserFlagFavorite: false})
	assert.Empty(t, *changes, "an edit that changes nothing says nothing")
}

// Single-disc edits and set edits share one lock, so no disc ends with a
// projection that differs from what UserDB holds, and the set never holds
// both a dislike and a like or favorite on one disc.
func TestApplyMediaUserFlagsToSetConcurrentEditsConverge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, discs := newDiscSetTestDB(t)
	requests := []flagSet{
		{database.MediaUserFlagLiked: true},
		{database.MediaUserFlagDisliked: true},
		{database.MediaUserFlagFavorite: true},
		{database.MediaUserFlagPlayLater: true},
		{database.MediaUserFlagFavorite: false},
		{database.MediaUserFlagDisliked: false},
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range 25 {
				changes := requests[(worker+i)%len(requests)]
				disc := discs[(worker+i)%len(discs)]
				var err error
				if worker%2 == 0 {
					_, err = database.ApplyMediaUserFlagsToSet(ctx, db, "PSX", discs, disc.Path, changes)
				} else {
					_, err = database.ApplyMediaUserFlags(ctx, db, "PSX", disc.Path, disc.MediaDBID, changes)
				}
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()
	requireSetInStep(t, db, discs)
	for _, disc := range discs {
		stored := discStored(t, db, disc.Path)
		assert.False(t, stored[database.MediaUserFlagDisliked] &&
			(stored[database.MediaUserFlagLiked] || stored[database.MediaUserFlagFavorite]), disc.Path)
	}
}

func TestPlanDiscSetFlagWritesOrdersClearsFirst(t *testing.T) {
	t.Parallel()
	discs := []database.DiscSetMember{{Path: "a"}, {Path: "b"}}
	current := map[database.MediaUserRef]database.MediaUserData{
		{SystemID: "PSX", Path: "b"}: {IsDisliked: true},
	}
	writes := database.PlanDiscSetFlagWrites("PSX", discs, 0, current, flagSet{database.MediaUserFlagLiked: true})
	assert.Equal(t, []database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: "b", Flag: database.MediaUserFlagDisliked, Value: false},
		{SystemID: "PSX", Path: "a", Flag: database.MediaUserFlagLiked, Value: true},
	}, writes)

	// Disliking clears like and favorite; a disc holding both gets each
	// clear once.
	current = map[database.MediaUserRef]database.MediaUserData{
		{SystemID: "PSX", Path: "a"}: {IsLiked: true, IsFavorite: true},
	}
	writes = database.PlanDiscSetFlagWrites("PSX", discs, 1, current, flagSet{database.MediaUserFlagDisliked: true})
	assert.ElementsMatch(t, []database.MediaUserFlagWrite{
		{SystemID: "PSX", Path: "a", Flag: database.MediaUserFlagLiked, Value: false},
		{SystemID: "PSX", Path: "a", Flag: database.MediaUserFlagFavorite, Value: false},
	}, writes[:2])
	assert.Equal(t, database.MediaUserFlagWrite{
		SystemID: "PSX", Path: "b", Flag: database.MediaUserFlagDisliked, Value: true,
	}, writes[2])
}

func userTag(flag database.MediaUserFlag) database.TagInfo {
	return database.TagInfo{Type: string(tags.TagTypeUser), Tag: string(flag)}
}

func TestMergeDiscSetUserTags(t *testing.T) {
	t.Parallel()
	region := database.TagInfo{Type: "region", Tag: "us"}
	disc := database.TagInfo{Type: "disc", Tag: "1"}

	tests := []struct {
		name   string
		launch []database.TagInfo
		others [][]database.TagInfo
		want   []database.TagInfo
	}{
		{
			name:   "flag held by another disc shows",
			launch: []database.TagInfo{region, disc},
			others: [][]database.TagInfo{{userTag(database.MediaUserFlagFavorite)}},
			want:   []database.TagInfo{region, disc, userTag(database.MediaUserFlagFavorite)},
		},
		{
			name:   "dislike hides behind a sibling's favorite",
			launch: []database.TagInfo{userTag(database.MediaUserFlagDisliked)},
			others: [][]database.TagInfo{{userTag(database.MediaUserFlagFavorite)}},
			want:   []database.TagInfo{userTag(database.MediaUserFlagFavorite)},
		},
		{
			name:   "dislike hides behind a sibling's like",
			launch: []database.TagInfo{userTag(database.MediaUserFlagLiked)},
			others: [][]database.TagInfo{{userTag(database.MediaUserFlagDisliked)}},
			want:   []database.TagInfo{userTag(database.MediaUserFlagLiked)},
		},
		{
			name:   "dislike alone shows",
			launch: []database.TagInfo{region},
			others: [][]database.TagInfo{
				{userTag(database.MediaUserFlagDisliked), userTag(database.MediaUserFlagPlayLater)},
			},
			want: []database.TagInfo{
				region, userTag(database.MediaUserFlagDisliked), userTag(database.MediaUserFlagPlayLater),
			},
		},
		{
			name:   "hidden stays the launch disc's own",
			launch: []database.TagInfo{userTag(database.MediaUserFlagHidden)},
			others: [][]database.TagInfo{{userTag(database.MediaUserFlagHidden), userTag(database.MediaUserFlagLiked)}},
			want:   []database.TagInfo{userTag(database.MediaUserFlagHidden), userTag(database.MediaUserFlagLiked)},
		},
		{
			name:   "a sibling's hidden does not show",
			launch: []database.TagInfo{region},
			others: [][]database.TagInfo{{userTag(database.MediaUserFlagHidden)}},
			want:   []database.TagInfo{region},
		},
		{
			name:   "duplicates collapse",
			launch: []database.TagInfo{userTag(database.MediaUserFlagFavorite)},
			others: [][]database.TagInfo{
				{userTag(database.MediaUserFlagFavorite)}, {userTag(database.MediaUserFlagFavorite)},
			},
			want: []database.TagInfo{userTag(database.MediaUserFlagFavorite)},
		},
		{
			name: "no tags is an empty list",
			want: []database.TagInfo{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, database.MergeDiscSetUserTags(tt.launch, tt.others...))
		})
	}
}
