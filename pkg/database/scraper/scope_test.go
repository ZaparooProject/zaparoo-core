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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestApplyScopedTargets(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "resume", "cancel", "outside", "zero"} {
		t.Run(mode, func(t *testing.T) {
			mdb := helpers.NewMockMediaDBI()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := scraper.ScrapeOptions{Scope: &database.ScrapeScope{SystemID: "NES"}}
			selection := scraper.ScopedSelection{Media: []database.MediaWithFullPath{{DBID: 1, MediaTitleDBID: 10}}}
			write := &database.ScrapeWrite{Sentinel: scraper.SentinelTagInfo("test")}
			target := database.ScrapeWriteTarget{MediaDBID: 1, MediaTitleDBID: 10, Write: write}
			targets := []database.ScrapeWriteTarget{target, target}
			switch mode {
			case "normal":
				mdb.On("ApplyScrapeResult", mock.Anything, int64(1), int64(10), write).Return(nil).Once()
			case "resume":
				selection.Completed = map[int64]struct{}{1: {}}
			case "cancel":
				cancel()
			case "outside":
				targets[0].MediaTitleDBID = 99
			case "zero":
				selection.Media, targets = nil, nil
			}
			ch := make(chan scraper.ScrapeUpdate, 8)
			scraper.ApplyScopedTargets(ctx, mdb, opts, selection, targets, ch)
			close(ch)
			var final scraper.ScrapeUpdate
			for update := range ch {
				final = update
			}
			require.True(t, final.Done)
			if mode == "outside" {
				require.Error(t, final.FatalErr)
			} else {
				require.Equal(t, len(selection.Media), final.Total)
				if mode != "cancel" {
					require.Equal(t, final.Total, final.Processed)
				}
			}
			if mode == "normal" {
				require.Equal(t, 1, final.Matched)
			}
			if mode == "resume" {
				require.Equal(t, 1, final.Skipped)
			}
			mdb.AssertExpectations(t)
		})
	}
}
