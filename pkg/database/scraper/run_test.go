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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- stub ScraperLoop ---

type stubRecord struct {
	id string
}

type stubLoop struct {
	matchFn func(r stubRecord) (*scraper.MatchResult, error)
	mapFn   func(r stubRecord) scraper.MapResult
	id      string
	records []stubRecord
}

func (s *stubLoop) ID() string { return s.id }

func (s *stubLoop) LoadRecords(_ context.Context, _ scraper.ScrapeSystem) ([]stubRecord, error) {
	return s.records, nil
}

func (s *stubLoop) Match(
	_ context.Context, r stubRecord, _ scraper.ScrapeSystem, _ database.MediaDBI,
) (*scraper.MatchResult, error) {
	if s.matchFn != nil {
		return s.matchFn(r)
	}
	return &scraper.MatchResult{MediaDBID: 1, MediaTitleDBID: 1}, nil
}

func (s *stubLoop) MapToDB(r stubRecord) scraper.MapResult {
	if s.mapFn != nil {
		return s.mapFn(r)
	}
	return scraper.MapResult{}
}

// drainUpdates collects all updates from the channel and returns them.
func drainUpdates(ch <-chan scraper.ScrapeUpdate) []scraper.ScrapeUpdate {
	var updates []scraper.ScrapeUpdate
	for u := range ch {
		updates = append(updates, u)
	}
	return updates
}

func TestRunScraper_NoRecords(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	loop := &stubLoop{id: "test", records: nil}

	ch := scraper.RunScraper(
		context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	require.NotEmpty(t, updates)
	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	db.AssertExpectations(t)
}

func TestRunScraper_NoMatch_IsSkipped(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	loop := &stubLoop{
		id:      "test",
		records: []stubRecord{{id: "mario"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			return nil, nil //nolint:nilnil // no match; nil result is the "skip" sentinel
		},
	}

	ch := scraper.RunScraper(
		context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	// No writes should occur for an unmatched record.
	db.AssertNotCalled(t, "UpsertMediaTags")
	db.AssertNotCalled(t, "UpsertMediaTitleTags")
	db.AssertExpectations(t)
}

func TestRunScraper_SentinelSkip(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	// Record matches media DBID=5; it already has the sentinel tag.
	db.On("MediaHasTag", mock.Anything, int64(5), "scraper.test:scraped").Return(true, nil)

	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	loop := &stubLoop{
		id:      "test",
		records: []stubRecord{{id: "mario"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			return &scraper.MatchResult{MediaDBID: 5, MediaTitleDBID: 10}, nil
		},
	}

	ch := scraper.RunScraper(
		context.Background(), scraper.ScrapeOptions{Force: false}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	// Sentinel-skipped record must not trigger any DB writes.
	db.AssertNotCalled(t, "UpsertMediaTags")
	db.AssertNotCalled(t, "UpsertMediaTitleTags")
	db.AssertExpectations(t)
}

func TestRunScraper_Force_IgnoresSentinel(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	// MediaHasTag should NOT be called when Force=true.
	db.On("UpsertMediaTags", mock.Anything, int64(5), mock.Anything).Return(nil).Times(2) // tags + sentinel
	db.On("UpsertMediaTitleTags", mock.Anything, int64(10), mock.Anything).Return(nil).Once()

	system := scraper.ScrapeSystem{DBID: 1, ID: "test"}
	loop := &stubLoop{
		id:      "test",
		records: []stubRecord{{id: "mario"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			return &scraper.MatchResult{MediaDBID: 5, MediaTitleDBID: 10}, nil
		},
		mapFn: func(_ stubRecord) scraper.MapResult {
			return scraper.MapResult{
				MediaTags: []database.TagInfo{{Type: "genre", Tag: "platform"}},
				TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
			}
		},
	}

	ch := scraper.RunScraper(
		context.Background(), scraper.ScrapeOptions{Force: true}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	var processedSeen bool
	for _, u := range updates {
		if u.Processed == 1 && u.Matched == 1 {
			processedSeen = true
		}
	}
	assert.True(t, processedSeen, "force should process record regardless of sentinel")
	db.AssertNotCalled(t, "MediaHasTag")
	db.AssertExpectations(t)
}

func TestRunScraper_NonFatalMatchError_ContinuesLoop(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	matchErr := errors.New("lookup failed")

	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	callCount := 0
	loop := &stubLoop{
		id:      "test",
		records: []stubRecord{{id: "mario"}, {id: "zelda"}},
		matchFn: func(r stubRecord) (*scraper.MatchResult, error) {
			callCount++
			if r.id == "mario" {
				return nil, matchErr
			}
			return nil, nil //nolint:nilnil // no match; nil result is the "skip" sentinel
		},
	}

	ch := scraper.RunScraper(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	require.NoError(t, last.FatalErr, "match errors must not be fatal")

	var errSeen bool
	for _, u := range updates {
		if errors.Is(u.Err, matchErr) {
			errSeen = true
		}
	}
	assert.True(t, errSeen, "non-fatal match error should be emitted on the update channel")
	assert.Equal(t, 2, callCount, "loop should process both records")
	db.AssertExpectations(t)
}

func TestRunScraper_CtxCancel_EmitsDone(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	ctx, cancel := context.WithCancel(context.Background())

	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	loop := &stubLoop{
		id:      "test",
		records: []stubRecord{{id: "mario"}, {id: "zelda"}, {id: "metroid"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			cancel()        // cancel on first match attempt
			return nil, nil //nolint:nilnil // no match; nil result is the "skip" sentinel
		},
	}

	ch := scraper.RunScraper(ctx, scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done, "cancelled run must emit Done=true")
}

func TestRunScraper_FullWrite_HappyPath(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockMediaDBI()
	db.On("MediaHasTag", mock.Anything, int64(1), "scraper.gl:scraped").Return(false, nil)
	db.On("UpsertMediaTags", mock.Anything, int64(1), mock.Anything).Return(nil).Times(2)
	db.On("UpsertMediaTitleTags", mock.Anything, int64(2), mock.Anything).Return(nil).Once()
	db.On("UpsertMediaTitleProperties", mock.Anything, int64(2), mock.Anything).Return(nil).Once()
	db.On("UpsertMediaProperties", mock.Anything, int64(1), mock.Anything).Return(nil).Once()

	system := scraper.ScrapeSystem{DBID: 10, ID: "NES"}
	loop := &stubLoop{
		id:      "gl",
		records: []stubRecord{{id: "mario"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			return &scraper.MatchResult{MediaDBID: 1, MediaTitleDBID: 2}, nil
		},
		mapFn: func(_ stubRecord) scraper.MapResult {
			return scraper.MapResult{
				MediaTags: []database.TagInfo{{Type: "region", Tag: "usa"}},
				TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
				TitleProps: []database.MediaProperty{
					{TypeTag: "property:description", Text: "A classic"},
				},
				MediaProps: []database.MediaProperty{
					{TypeTag: "property:video", Text: "/roms/nes/mario.mp4"},
				},
			}
		},
	}

	ch := scraper.RunScraper(
		context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	updates := drainUpdates(ch)

	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	// Fix 4: the Done update must carry accumulated totals.
	assert.Equal(t, 1, last.Processed, "Done update must carry total processed count")
	assert.Equal(t, 1, last.Matched, "Done update must carry total matched count")
	db.AssertExpectations(t)
}

func TestSentinelTag(t *testing.T) {
	// sentinelTag is unexported — test its effect through RunScraper by checking
	// what tag string is passed to MediaHasTag.
	t.Parallel()
	var capturedTag string
	db := helpers.NewMockMediaDBI()
	db.On("MediaHasTag", mock.Anything, int64(1), mock.AnythingOfType("string")).
		Run(func(args mock.Arguments) { capturedTag = args.String(2) }).
		Return(false, nil)
	db.On("UpsertMediaTags", mock.Anything, int64(1), mock.Anything).Return(nil).Times(2)

	system := scraper.ScrapeSystem{DBID: 1, ID: "NES"}
	loop := &stubLoop{
		id:      "myscr",
		records: []stubRecord{{id: "mario"}},
		matchFn: func(_ stubRecord) (*scraper.MatchResult, error) {
			return &scraper.MatchResult{MediaDBID: 1, MediaTitleDBID: 1}, nil
		},
	}

	ch := scraper.RunScraper(context.Background(), scraper.ScrapeOptions{}, []scraper.ScrapeSystem{system}, db, loop)
	drainUpdates(ch)

	assert.Equal(t, "scraper.myscr:scraped", capturedTag)
}
