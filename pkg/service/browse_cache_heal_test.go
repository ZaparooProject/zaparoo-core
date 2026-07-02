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

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
)

// runBrowseCacheHealOptimizingCycle drives checkAndHealBrowseCache through a
// needed rebuild whose PopulateBrowseCache returns populateErr, then asserts the
// rebuild is attempted and that the optimizing indicator is both set (true) and
// cleared (false). The clear must be emitted even when the rebuild fails so the
// client never shows a permanent "preparing library" spinner.
func runBrowseCacheHealOptimizingCycle(t *testing.T, populateErr error) {
	t.Helper()

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockDB.On("GetTotalMediaCount").Return(1000, nil)
	mockDB.On("BrowseCacheNeedsRebuild", mock.Anything).Return(true, nil)
	mockDB.On("TrackBackgroundOperation").Return()
	mockDB.On("BackgroundOperationDone").Return()

	attempted := make(chan struct{}, 1)
	mockDB.On("PopulateBrowseCache", mock.Anything).Return(populateErr).Run(func(_ mock.Arguments) {
		attempted <- struct{}{}
	})

	ns := make(chan models.Notification, 8)
	checkAndHealBrowseCache(context.Background(), &database.Database{MediaDB: mockDB}, ns, syncutil.NewPauser())

	select {
	case <-attempted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected the browse cache rebuild to be attempted in the background")
	}
	mockDB.AssertCalled(t, "PopulateBrowseCache", mock.Anything)

	// The self-heal must surface as an optimizing operation: a start notification
	// (optimizing:true) and a completion notification (optimizing:false) so the
	// client can show and then clear a "preparing library" indicator.
	sawOptimizing, sawCleared := false, false
	for !sawOptimizing || !sawCleared {
		select {
		case n := <-ns:
			switch {
			case strings.Contains(string(n.Params), `"optimizing":true`):
				sawOptimizing = true
			case strings.Contains(string(n.Params), `"optimizing":false`):
				sawCleared = true
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("expected optimizing start+clear notifications (start=%v clear=%v)", sawOptimizing, sawCleared)
		}
	}
}

func TestCheckAndHealBrowseCache_RebuildsWhenStale(t *testing.T) {
	runBrowseCacheHealOptimizingCycle(t, nil)
}

func TestCheckAndHealBrowseCache_ClearsOptimizingOnRebuildFailure(t *testing.T) {
	runBrowseCacheHealOptimizingCycle(t, errors.New("rebuild boom"))
}

func TestCheckAndHealBrowseCache_SkipsWhenFresh(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockDB.On("GetTotalMediaCount").Return(1000, nil)
	mockDB.On("BrowseCacheNeedsRebuild", mock.Anything).Return(false, nil)

	checkAndHealBrowseCache(context.Background(), &database.Database{MediaDB: mockDB}, nil, syncutil.NewPauser())

	mockDB.AssertNotCalled(t, "PopulateBrowseCache", mock.Anything)
	mockDB.AssertNotCalled(t, "TrackBackgroundOperation")
}

func TestCheckAndHealBrowseCache_SkipsWhenOptimizationRunning(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusRunning, nil)

	checkAndHealBrowseCache(context.Background(), &database.Database{MediaDB: mockDB}, nil, syncutil.NewPauser())

	mockDB.AssertNotCalled(t, "BrowseCacheNeedsRebuild", mock.Anything)
	mockDB.AssertNotCalled(t, "PopulateBrowseCache", mock.Anything)
}

func TestCheckAndHealBrowseCache_SkipsWhenIndexing(t *testing.T) {
	// An active in-process reindex owns the cache, so the self-heal must return
	// before touching the DB at all — even before checking the optimization status.
	methods.SetIndexingForTest()
	t.Cleanup(methods.ClearIndexingStatus)

	mockDB := helpers.NewMockMediaDBI()

	checkAndHealBrowseCache(context.Background(), &database.Database{MediaDB: mockDB}, nil, syncutil.NewPauser())

	mockDB.AssertNotCalled(t, "GetOptimizationStatus")
	mockDB.AssertNotCalled(t, "GetTotalMediaCount")
	mockDB.AssertNotCalled(t, "BrowseCacheNeedsRebuild", mock.Anything)
	mockDB.AssertNotCalled(t, "PopulateBrowseCache", mock.Anything)
}

func TestCheckAndHealBrowseCache_SkipsWhenNoMedia(t *testing.T) {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockDB.On("GetTotalMediaCount").Return(0, nil)

	checkAndHealBrowseCache(context.Background(), &database.Database{MediaDB: mockDB}, nil, syncutil.NewPauser())

	mockDB.AssertNotCalled(t, "BrowseCacheNeedsRebuild", mock.Anything)
	mockDB.AssertNotCalled(t, "PopulateBrowseCache", mock.Anything)
}

func TestCheckAndHealBrowseCache_NilDatabaseIsNoOp(_ *testing.T) {
	checkAndHealBrowseCache(context.Background(), &database.Database{}, nil, syncutil.NewPauser())
	checkAndHealBrowseCache(context.Background(), nil, nil, syncutil.NewPauser())
}
