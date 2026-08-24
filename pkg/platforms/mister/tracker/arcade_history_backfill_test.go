//go:build linux

package tracker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	testinghelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubSettledMediaDB makes the mock report an idle, ready-to-read MediaDB.
func stubSettledArcadeMediaDB(mediaDB *testinghelpers.MockMediaDBI) {
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
}

func TestRunArcadeHistoryBackfillAtInterval_SkipsWhenAlreadyDone(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	userDB.On("GetDeviceState", arcadeHistoryBackfillDeviceStateKey).Return("done", true, nil)

	runArcadeHistoryBackfillAtInterval(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{}, time.Millisecond,
	)

	userDB.AssertNotCalled(t, "GetMediaHistory", mock.Anything, mock.Anything, mock.Anything)
	mediaDB.AssertNotCalled(t, "GetIndexingStatus")
	userDB.AssertExpectations(t)
}

func TestRunArcadeHistoryBackfillAtInterval_MarksDoneAfterCleanPass(t *testing.T) {
	t.Parallel()

	mraPath := filepath.Join(t.TempDir(), "Pooyan.mra")
	writeTestMRA(t, mraPath, "pooyan", "Pooyan")

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	stubSettledArcadeMediaDB(mediaDB)
	userDB.On("GetDeviceState", arcadeHistoryBackfillDeviceStateKey).Return("", false, nil).Once()
	// A short page (fewer rows than the page size) already ends the walk, so
	// no follow-up call with the advanced cursor is expected.
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry{
			{DBID: 5, SystemID: ArcadeSystem, MediaPath: "pooyan"},
		}, nil).Once()
	mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
		Return([]database.SearchResultWithCursor{{Path: mraPath}}, nil)
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mraPath).
		Return([]database.SearchResult{{
			SystemID: ArcadeSystem, Name: "Pooyan", Path: mraPath, Slug: "pooyan", MediaID: 9,
		}}, nil)
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(9)).
		Return([]database.TagInfo{}, nil)
	userDB.On("UpdateMediaHistoryIdentityAndPath", int64(5), mraPath, mock.Anything).
		Return(true, nil).Once()
	userDB.On("SetDeviceState", arcadeHistoryBackfillDeviceStateKey, "done").Return(nil).Once()

	tr := &Tracker{
		db:      &database.Database{UserDB: userDB, MediaDB: mediaDB},
		NameMap: []NameMapping{{CoreName: "pooyan", System: ArcadeSystem, ArcadeName: "Pooyan"}},
	}
	runArcadeHistoryBackfillAtInterval(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, tr, time.Millisecond,
	)

	userDB.AssertExpectations(t)
	mediaDB.AssertExpectations(t)
}

func TestRunArcadeHistoryBackfillAtInterval_RetriesCompletionMarkerWrite(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	stubSettledArcadeMediaDB(mediaDB)
	userDB.On("GetDeviceState", arcadeHistoryBackfillDeviceStateKey).Return("", false, nil)
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry{}, nil)
	userDB.On("SetDeviceState", arcadeHistoryBackfillDeviceStateKey, "done").Return(assert.AnError).Once()
	userDB.On("SetDeviceState", arcadeHistoryBackfillDeviceStateKey, "done").Return(nil).Once()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runArcadeHistoryBackfillAtInterval(
		ctx, &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{}, time.Millisecond,
	)

	require.NoError(t, ctx.Err())
	userDB.AssertExpectations(t)
}

func TestRunArcadeHistoryBackfillAtInterval_RetriesWhileMediaDBUnsettled(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	userDB.On("GetDeviceState", arcadeHistoryBackfillDeviceStateKey).Return("", false, nil)
	mediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runArcadeHistoryBackfillAtInterval(
			ctx, &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{}, 5*time.Millisecond,
		)
	}()

	// Give it a couple of retry cycles to prove it never reads history while
	// unsettled, then stop it.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("backfill did not stop after cancellation")
	}

	userDB.AssertNotCalled(t, "GetMediaHistory", mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	mediaDB.AssertNotCalled(t, "GetOptimizationStatus")
}

func TestRunArcadeHistoryBackfillAtInterval_RetriesWhenRowUnresolved(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	stubSettledArcadeMediaDB(mediaDB)
	userDB.On("GetDeviceState", arcadeHistoryBackfillDeviceStateKey).Return("", false, nil)
	// No NameMap entry for "unknownset", so every pass leaves it unresolved.
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry{
			{DBID: 7, SystemID: ArcadeSystem, MediaPath: "unknownset"},
		}, nil)

	tr := &Tracker{db: &database.Database{UserDB: userDB, MediaDB: mediaDB}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runArcadeHistoryBackfillAtInterval(
			ctx, &database.Database{UserDB: userDB, MediaDB: mediaDB}, tr, 5*time.Millisecond,
		)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("backfill did not stop after cancellation")
	}

	userDB.AssertNotCalled(t, "SetDeviceState", mock.Anything, mock.Anything)
	userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentityAndPath", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunArcadeHistoryBackfillAtInterval_NilDependenciesReturnImmediately(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()

	// Each case is missing exactly one required dependency.
	runArcadeHistoryBackfillAtInterval(context.Background(), nil, &Tracker{}, time.Millisecond)
	runArcadeHistoryBackfillAtInterval(
		context.Background(), &database.Database{MediaDB: mediaDB}, &Tracker{}, time.Millisecond,
	)
	runArcadeHistoryBackfillAtInterval(
		context.Background(), &database.Database{UserDB: userDB}, &Tracker{}, time.Millisecond,
	)
	runArcadeHistoryBackfillAtInterval(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, nil, time.Millisecond,
	)

	userDB.AssertNotCalled(t, "GetDeviceState", mock.Anything)
	mediaDB.AssertNotCalled(t, "GetIndexingStatus")
}

func TestRunArcadeHistoryBackfillPass_SkipsAlreadyResolvedRows(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	absPath, err := filepath.Abs(filepath.Join("_Arcade", "AlreadyReal.mra"))
	require.NoError(t, err)

	// A short page already ends the walk, so no follow-up call is expected.
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry{
			{DBID: 1, SystemID: ArcadeSystem, MediaPath: "some.mra", MediaIdentity: &database.MediaIdentity{}},
			{DBID: 2, SystemID: ArcadeSystem, MediaPath: absPath},
		}, nil).Once()

	clean := runArcadeHistoryBackfillPass(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{},
	)

	assert.True(t, clean, "rows already resolved or already a real path need no work")
	userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentityAndPath", mock.Anything, mock.Anything, mock.Anything)
	userDB.AssertExpectations(t)
}

// A full page (== page size) does not by itself end the walk - only a page
// shorter than the page size does. The next call must carry the last row's
// DBID as its cursor.
func TestRunArcadeHistoryBackfillPass_PaginatesFullPages(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()

	fullPage := make([]database.MediaHistoryEntry, arcadeHistoryBackfillPageSize)
	for i := range fullPage {
		fullPage[i] = database.MediaHistoryEntry{
			DBID: int64(i + 1), SystemID: ArcadeSystem,
			MediaPath: "resolved.mra", MediaIdentity: &database.MediaIdentity{},
		}
	}
	lastDBID := fullPage[len(fullPage)-1].DBID

	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return(fullPage, nil).Once()
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, lastDBID, arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry{}, nil).Once()

	clean := runArcadeHistoryBackfillPass(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{},
	)

	assert.True(t, clean)
	userDB.AssertExpectations(t)
}

func TestRunArcadeHistoryBackfillPass_ReadFailureIsNotClean(t *testing.T) {
	t.Parallel()

	userDB := testinghelpers.NewMockUserDBI()
	mediaDB := testinghelpers.NewMockMediaDBI()
	userDB.On("GetMediaHistory", []string{ArcadeSystem}, int64(0), arcadeHistoryBackfillPageSize).
		Return([]database.MediaHistoryEntry(nil), assert.AnError).Once()

	clean := runArcadeHistoryBackfillPass(
		context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, &Tracker{},
	)

	assert.False(t, clean)
}

func TestResolveAndFixArcadeHistoryRow(t *testing.T) {
	t.Parallel()

	t.Run("unresolved set name", func(t *testing.T) {
		t.Parallel()
		userDB := testinghelpers.NewMockUserDBI()
		mediaDB := testinghelpers.NewMockMediaDBI()
		tr := &Tracker{db: &database.Database{UserDB: userDB, MediaDB: mediaDB}}

		ok := resolveAndFixArcadeHistoryRow(
			context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, tr,
			&database.MediaHistoryEntry{DBID: 1, SystemID: ArcadeSystem, MediaPath: "unknownset"},
		)

		assert.False(t, ok)
		userDB.AssertNotCalled(t, "UpdateMediaHistoryIdentityAndPath", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("write failure leaves row unresolved", func(t *testing.T) {
		t.Parallel()
		mraPath := filepath.Join(t.TempDir(), "Pooyan.mra")
		writeTestMRA(t, mraPath, "pooyan", "Pooyan")

		userDB := testinghelpers.NewMockUserDBI()
		mediaDB := testinghelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaBySlug", mock.Anything, ArcadeSystem, "Pooyan", []zapscript.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{{Path: mraPath}}, nil)
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mraPath).
			Return([]database.SearchResult{{
				SystemID: ArcadeSystem, Name: "Pooyan", Path: mraPath, Slug: "pooyan", MediaID: 3,
			}}, nil)
		mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(3)).
			Return([]database.TagInfo{}, nil)
		userDB.On("UpdateMediaHistoryIdentityAndPath", int64(6), mraPath, mock.Anything).
			Return(false, assert.AnError)

		tr := &Tracker{
			db:      &database.Database{UserDB: userDB, MediaDB: mediaDB},
			NameMap: []NameMapping{{CoreName: "pooyan", System: ArcadeSystem, ArcadeName: "Pooyan"}},
		}
		ok := resolveAndFixArcadeHistoryRow(
			context.Background(), &database.Database{UserDB: userDB, MediaDB: mediaDB}, tr,
			&database.MediaHistoryEntry{DBID: 6, SystemID: ArcadeSystem, MediaPath: "pooyan"},
		)

		assert.False(t, ok)
	})
}
