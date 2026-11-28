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

package artwork_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/artwork"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

// testRow builds a fixture unique to the calling test. The media.image handler
// keeps a process-wide negative cache keyed by system and path, so tests that
// shared a fixture would leak "no image" results into each other.
func testRow(t *testing.T) *database.MediaFullRow {
	t.Helper()
	unique := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return &database.MediaFullRow{
		Media:  database.Media{DBID: 4242, Path: filepath.Join("games", unique+".rom")},
		Title:  database.MediaTitle{DBID: 4243, Slug: "artwork-fixture", Name: "Artwork Fixture"},
		System: database.System{DBID: 424, SystemID: "zd-" + unique, Name: "Test System"},
	}
}

func newSource(t *testing.T, titleProps []database.MediaProperty) (*artwork.Source, *database.MediaFullRow) {
	t.Helper()

	row := testRow(t)
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).Return(&row.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).Return(row, nil)
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).Return(titleProps, nil)
	// Any other system is simply unknown to this database.
	mockDB.On("FindSystemBySystemID", mock.Anything).Return(database.System{}, sql.ErrNoRows).Maybe()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	return artwork.New(pl, cfg, &database.Database{MediaDB: mockDB}), row
}

func TestArtworkReturnsStoredCover(t *testing.T) {
	t.Parallel()

	source, row := newSource(t, []database.MediaProperty{
		{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: pngBytes(t)},
	})

	art, err := source.Artwork(context.Background(), row.System.SystemID, row.Path, 256)
	require.NoError(t, err)
	require.NotNil(t, art)
	assert.NotEmpty(t, art.Data)
	assert.Contains(t, art.TypeTag, "boxart")
	assert.NotEmpty(t, art.ContentType)
}

func TestArtworkReportsNoArtworkWhenMediaHasNone(t *testing.T) {
	t.Parallel()

	source, row := newSource(t, []database.MediaProperty{})

	// Plenty of media is never scraped. That is an ordinary outcome, and the
	// driver renders a coverless scene rather than treating it as a failure.
	_, err := source.Artwork(context.Background(), row.System.SystemID, row.Path, 256)
	require.Error(t, err)
	require.ErrorIs(t, err, readers.ErrNoArtwork)
}

func TestArtworkReportsNoArtworkForUnknownSystem(t *testing.T) {
	t.Parallel()

	source, _ := newSource(t, []database.MediaProperty{})

	_, err := source.Artwork(context.Background(), "system-that-does-not-exist", "whatever.rom", 256)
	require.Error(t, err)
	require.ErrorIs(t, err, readers.ErrNoArtwork)
}

func TestArtworkWithoutDatabaseReportsNoArtwork(t *testing.T) {
	t.Parallel()

	source := artwork.New(nil, nil, nil)
	_, err := source.Artwork(context.Background(), "any-system", "any/path.rom", 256)
	require.ErrorIs(t, err, readers.ErrNoArtwork)
}

func TestArtworkClampsOutOfRangeMaxSize(t *testing.T) {
	t.Parallel()

	// An out-of-range hint must not be rejected by the image handler's own
	// parameter validation; it is clamped to the top thumbnail tier instead.
	for _, maxSize := range []int{0, -1, 1 << 20} {
		t.Run(fmt.Sprintf("maxSize %d", maxSize), func(t *testing.T) {
			t.Parallel()
			source, row := newSource(t, []database.MediaProperty{
				{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: pngBytes(t)},
			})
			art, err := source.Artwork(context.Background(), row.System.SystemID, row.Path, maxSize)
			require.NoError(t, err)
			assert.NotEmpty(t, art.Data)
		})
	}
}

func TestArtworkRejectsIncompleteRequests(t *testing.T) {
	t.Parallel()

	source, row := newSource(t, []database.MediaProperty{})

	// A caller that forgot to fill in the media is a bug here, not media that
	// happens to have no artwork, so it must not be reported as ErrNoArtwork.
	_, err := source.Artwork(context.Background(), "", row.Path, 256)
	require.Error(t, err)
	require.NotErrorIs(t, err, readers.ErrNoArtwork)

	_, err = source.Artwork(context.Background(), row.System.SystemID, "", 256)
	require.Error(t, err)
	require.NotErrorIs(t, err, readers.ErrNoArtwork)
}
