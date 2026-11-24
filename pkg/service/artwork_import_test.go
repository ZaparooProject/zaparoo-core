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
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/artwork"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestImportScreenshotArtworkLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return(ids.Mister)
	pl.On("Settings").Return(platforms.Settings{HostManagedPaths: true, DataDir: dir})

	game := &database.MediaFullRow{
		System: database.System{SystemID: "Arcade", DBID: 1},
		Media:  database.Media{Path: "/games/example.mra", DBID: 2},
		Title:  database.MediaTitle{Slug: "example", DBID: 3, SystemDBID: 1},
	}
	db := testhelpers.NewMockMediaDBI()
	db.On("FindMediaIDsByPaths", mock.Anything, []string{game.Path}).
		Return([]database.MediaPathID{{DBID: game.DBID}}, nil).Once()
	db.On("GetMediaWithTitleAndSystem", mock.Anything, game.DBID).Return(game, nil).Once()

	bundle := filepath.Join(dir, artwork.InboxDir, "capture-loop01")
	require.NoError(t, os.MkdirAll(bundle, 0o750))
	manifest := `{"version":1,"gamePath":"/games/example.mra","imageType":"screenshot","scope":"version"}`
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "request.json"), []byte(manifest), 0o600))
	var pixels bytes.Buffer
	require.NoError(t, png.Encode(&pixels, image.NewGray(image.Rect(0, 0, 2, 2))))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "capture.png"), pixels.Bytes(), 0o600))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		importScreenshotArtwork(ctx, pl, db)
	}()
	assert.Eventually(t, func() bool {
		_, err := os.Stat(bundle)
		return os.IsNotExist(err)
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done

	has, err := artwork.HasOverride(t.Context(), db, dir, "Arcade", game.Path, game.DBID)
	require.NoError(t, err)
	assert.True(t, has)
	db.AssertExpectations(t)
}
