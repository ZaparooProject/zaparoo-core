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

package database

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScrapingOperationBoundAndAuthority(t *testing.T) {
	t.Parallel()
	op := ScrapingOperation{Version: 1, Status: "cancelled", ScraperID: "local"}
	require.False(t, op.IsResumable("running"), "legacy presentation cannot revive cancelled work")
	op.Status = "pending"
	require.True(t, op.IsResumable("completed"), "accepted queue must survive legacy status write failure")
	for range 63 {
		op.Pending = append(op.Pending, ScrapeJob{ScraperID: "local"})
	}
	require.NoError(t, op.Validate())
	op.Pending = append(op.Pending, ScrapeJob{ScraperID: "local"})
	require.ErrorContains(t, op.Validate(), "64 jobs")
}

func FuzzScrapingOperation(f *testing.F) {
	for _, seed := range []string{
		`{"scraperId":"legacy","force":true}`,
		`{"version":1,"status":"pending","scraperId":"local","fillMissing":true,"pending":[{"scraperId":"next"}]}`,
		`{"version":99,"scraperId":"unknown"}`, `null`, `{}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var op ScrapingOperation
		if err := json.Unmarshal([]byte(raw), &op); err != nil {
			return
		}
		if op.Validate() != nil {
			return
		}
		require.Contains(t, []int{0, 1}, op.Version)
		require.NotEmpty(t, op.ScraperID)
		require.LessOrEqual(t, len(op.Pending), 63)
		require.False(t, op.Force && op.FillMissing)
		for _, job := range op.Pending {
			require.NotEmpty(t, job.ScraperID)
			require.False(t, job.Force && job.FillMissing)
		}
		op.Version = 99
		require.Error(t, op.Validate())
	})
}
