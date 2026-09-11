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

package methods

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScrapeJobPersistenceFences(t *testing.T) {
	for _, mode := range []string{
		"idle cancel", "cancel write fails", "cancel cleanup fails", "stale recovery",
		"manual conflict", "advance write fails", "shutdown",
	} {
		t.Run(mode, func(t *testing.T) {
			s := &scrapingStatus{scraperID: "first"}
			db := testhelpers.NewMockMediaDBI()
			op := database.ScrapingOperation{
				Version: 1, Status: mediadb.IndexingStatusPending,
				ScraperID: "first", RunID: "stable", FillMissing: true,
				Pending: []database.ScrapeJob{{ScraperID: "next", RunID: "next-run", FillMissing: true}},
			}
			failure := errors.New("disk full")
			if mode == "stale recovery" || mode == "manual conflict" || mode == "shutdown" {
				db.On("SetScrapingOperation", mock.Anything).Return(nil).Maybe()
			}
			if mode == "cancel write fails" {
				db.On("ClearScrapingOperation").Return(nil).Maybe()
				t.Cleanup(func() { db.AssertNotCalled(t, "ClearScrapingOperation") })
			}
			switch mode {
			case "idle cancel", "cancel write fails", "cancel cleanup fails":
				db.On("GetScrapingOperation").Return(op, true, nil).Once()
				cancelled := op
				cancelled.Status = mediadb.IndexingStatusCancelled
				var writeErr, clearErr error
				if mode == "cancel write fails" {
					writeErr = failure
				}
				if mode == "cancel cleanup fails" {
					clearErr = failure
				}
				db.On("SetScrapingOperation", cancelled).Return(writeErr).Once()
				if writeErr == nil {
					db.On("SetScrapingStatus", mediadb.IndexingStatusCancelled).Return(nil).Once()
					db.On("ClearScrapingOperation").Return(clearErr).Once()
				}
				ok, err := s.cancelPersisted(db)
				require.True(t, ok)
				if mode == "idle cancel" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, failure)
				}
			case "stale recovery":
				cancelled := op
				cancelled.Status = mediadb.IndexingStatusCancelled
				db.On("GetScrapingOperation").Return(cancelled, true, nil).Once()
				require.ErrorIs(t, s.persistStart(db, &op, true), errScrapeJobRetired)
			case "manual conflict":
				db.On("GetScrapingOperation").Return(op, true, nil).Once()
				request := database.ScrapingOperation{ScraperID: "manual"}
				require.ErrorContains(t, s.persistStart(db, &request, false), "jobs are pending")
			case "advance write fails":
				db.On("SetScrapingOperation", mock.Anything).Return(failure).Once()
				_, err := s.advance(t.Context(), db, &op)
				require.ErrorIs(t, err, failure)
				require.Equal(t, "first", s.scraperID)
				require.Equal(t, "stable", op.RunID)
				require.Len(t, op.Pending, 1)
			case "shutdown":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err := s.advance(ctx, db, &op)
				require.ErrorIs(t, err, context.Canceled)
				require.Len(t, op.Pending, 1)
			}
			db.AssertExpectations(t)
			if mode == "stale recovery" || mode == "manual conflict" || mode == "shutdown" {
				db.AssertNotCalled(t, "SetScrapingOperation", mock.Anything)
			}
		})
	}
}
