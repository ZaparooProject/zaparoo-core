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

package mediadb

import (
	"context"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectBrowsePrefixPolicy_SamplesLargeFlatDir verifies that prefix-policy
// detection still recognises a rank-prefixed directory when it holds far more
// files than DefaultSampleLimit. The query samples (LIMIT) rather than scanning
// the whole partition, so a directory that would time out on a cold ~1M-row
// scan is still classified from a bounded read.
func TestDetectBrowsePrefixPolicy_SamplesLargeFlatDir(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	sys, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	parentDir := browseTestDir("roms", "nes")

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: sys.DBID,
		Slug:       slugs.Slugify(nesSystem.GetMediaType(), "Ranked"),
		Name:       "Ranked",
	})
	require.NoError(t, err)

	// Insert more rank-prefixed rows than the sample limit to prove detection
	// works from a partial read rather than a full scan.
	const total = browseprefix.DefaultSampleLimit + 500
	for i := range total {
		// Keep the rank prefix ≤3 digits (parseRank rejects longer) while making
		// each path unique via the trailing counter.
		name := fmt.Sprintf("%03d. Track %d.nes", i%1000, i)
		_, insErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     sys.DBID,
			MediaTitleDBID: title.DBID,
			Path:           browseTestPath("roms", "nes", name),
			ParentDir:      parentDir,
			SortName:       name,
		})
		require.NoError(t, insErr)
	}
	require.NoError(t, mediaDB.CommitTransaction())

	policy, err := detectBrowsePrefixPolicy(ctx, mediaDB.sql.Load(), parentDir, nil)
	require.NoError(t, err)
	assert.True(t, policy.Enabled, "rank policy should be detected from the sample")
	assert.Equal(t, browseprefix.KindRank, policy.Kind)
}
