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

package scraper_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type batchFailingMediaDB struct {
	*testhelpers.MockMediaDBI
	onBatch  func()
	batchErr error
}

func (m *batchFailingMediaDB) ApplyScrapeResults(context.Context, []database.ScrapeWriteTarget) error {
	if m.onBatch != nil {
		m.onBatch()
	}
	return m.batchErr
}

func TestApplyTargets_BatchFailureFallsBackToPerRecordWrites(t *testing.T) {
	t.Parallel()

	db := &batchFailingMediaDB{
		MockMediaDBI: testhelpers.NewMockMediaDBI(),
		batchErr:     errors.New("batch failed"),
	}
	db.On("ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	targets := []database.ScrapeWriteTarget{
		{MediaDBID: 1, MediaTitleDBID: 10, Write: &database.ScrapeWrite{}},
		{MediaDBID: 2, MediaTitleDBID: 20, Write: &database.ScrapeWrite{}},
	}

	var committed [][2]int
	err := scraper.ApplyTargets(context.Background(), db, scraper.ScrapeOptions{}, "test", targets,
		func(from, to int) { committed = append(committed, [2]int{from, to}) })

	require.NoError(t, err)
	db.AssertNumberOfCalls(t, "ApplyScrapeResult", len(targets))
	assert.Equal(t, [][2]int{{0, 2}}, committed)
}

func TestApplyTargets_CancelledBatchSkipsPerRecordFallback(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := &batchFailingMediaDB{
		MockMediaDBI: testhelpers.NewMockMediaDBI(),
		onBatch:      cancel,
		batchErr:     errors.New("batch interrupted"),
	}
	targets := []database.ScrapeWriteTarget{
		{MediaDBID: 1, MediaTitleDBID: 10, Write: &database.ScrapeWrite{}},
	}

	batchCommitted := false
	err := scraper.ApplyTargets(ctx, db, scraper.ScrapeOptions{}, "test", targets,
		func(_, _ int) { batchCommitted = true })

	require.ErrorIs(t, err, context.Canceled)
	db.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	assert.False(t, batchCommitted)
}
