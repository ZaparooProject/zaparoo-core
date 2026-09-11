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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// History keeps the path a launch was given, and on Windows that can be the
// native backslash form, while Media.Path is always the canonical
// forward-slash form. The enrichment lookup must bridge the two or every
// Windows history entry for a file comes back without its tags. The
// canonicaliser folds backslashes on every OS, so this reproduces anywhere.
func TestMediaHistoryTagsResolveNativeWindowsPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	ids := addTestMediaPaths(t, mediaDB, "C:/roms/NES/Alpha.nes")
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, ids[0], nil,
		[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))

	_, err := userDB.AddMediaHistory(&database.MediaHistoryEntry{
		SystemID: "NES", SystemName: "NES", MediaPath: `C:\roms\NES\Alpha.nes`, MediaName: "Alpha",
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	env := requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Config: &config.Instance{},
	}
	history, err := HandleMediaHistory(withParams(&env, `{}`))
	require.NoError(t, err)
	response, ok := history.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, response.Entries, 1)
	assert.Contains(t, response.Entries[0].Tags, database.TagInfo{Type: "user", Tag: "hidden"})
}
