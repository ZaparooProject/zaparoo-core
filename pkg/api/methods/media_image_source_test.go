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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sourceFileReaderTestPlatform struct {
	readErr error
	*mocks.MockPlatform
	content []byte
}

func (p *sourceFileReaderTestPlatform) ReadSourceFile(context.Context, string, int64) ([]byte, error) {
	if p.readErr != nil {
		return nil, p.readErr
	}
	return p.content, nil
}

func sourceImageTestRow() *database.MediaFullRow {
	return &database.MediaFullRow{Path: "source://abc/NES/game.nes", System: database.System{SystemID: "NES"}}
}

// A folder cover or media image whose Text is a source:// path is read
// through the platform's SourceFileReader, not afero - the bug this fix
// closes: those bytes live behind the host, not on a filesystem Core can
// open, so without this a HasCover:true row could never actually serve.
func TestLoadSourceMediaImageFileReadsThroughTheSourceFileReader(t *testing.T) {
	t.Parallel()

	content := []byte("not really a png, just bytes")
	platform := &sourceFileReaderTestPlatform{content: content}
	prop := &database.MediaProperty{Text: "source://abc/NES/media/boxart/game.png"}

	data, stale, err := loadSourceMediaImageFile(
		context.Background(), platform, sourceImageTestRow(), prop, true, "property:image-boxart", 1024,
	)
	require.NoError(t, err)
	assert.False(t, stale)
	assert.Equal(t, content, data)
}

// A platform with no SourceFileReader capability (no Android-style host, or
// any other platform that never implements it) must skip the row gracefully,
// the same way a missing real file already does - not panic on the type
// assertion or hard-error the whole media.image request.
func TestLoadSourceMediaImageFileSkipsAPlatformWithoutTheCapability(t *testing.T) {
	t.Parallel()

	platform := mocks.NewMockPlatform()
	prop := &database.MediaProperty{Text: "source://abc/NES/media/boxart/game.png"}

	data, stale, err := loadSourceMediaImageFile(
		context.Background(), platform, sourceImageTestRow(), prop, true, "property:image-boxart", 1024,
	)
	require.NoError(t, err)
	assert.True(t, stale)
	assert.Nil(t, data)
}

// A read failure (the root was revoked since the property was scraped, or
// the file was deleted) is also a graceful skip, not a hard error: the row
// is just stale now, same as a deleted real file.
func TestLoadSourceMediaImageFileSkipsOnAReadFailure(t *testing.T) {
	t.Parallel()

	platform := &sourceFileReaderTestPlatform{readErr: errors.New("source root is no longer granted")}
	prop := &database.MediaProperty{Text: "source://abc/NES/media/boxart/game.png"}

	data, stale, err := loadSourceMediaImageFile(
		context.Background(), platform, sourceImageTestRow(), prop, true, "property:image-boxart", 1024,
	)
	require.NoError(t, err)
	assert.True(t, stale)
	assert.Nil(t, data)
}

// A file that comes back larger than the cap is a real error, not a stale
// property: the host answered, the bytes are just too big to serve.
func TestLoadSourceMediaImageFileRejectsOversizedContent(t *testing.T) {
	t.Parallel()

	platform := &sourceFileReaderTestPlatform{content: make([]byte, 2048)}
	prop := &database.MediaProperty{Text: "source://abc/NES/media/boxart/game.png"}

	_, stale, err := loadSourceMediaImageFile(
		context.Background(), platform, sourceImageTestRow(), prop, true, "property:image-boxart", 1024,
	)
	require.Error(t, err)
	assert.False(t, stale)
}

// loadMediaImageFile's top-level dispatch must route a source:// prop.Text to
// the SourceFileReader path, never touching the (irrelevant, and for a
// source path meaningless) afero.Fs it's also given.
func TestLoadMediaImageFileRoutesSourcePathsToTheSourceFileReader(t *testing.T) {
	t.Parallel()

	content := []byte("boxart bytes")
	platform := &sourceFileReaderTestPlatform{content: content}
	prop := &database.MediaProperty{Text: "source://abc/NES/media/boxart/game.png"}

	data, stale, err := loadMediaImageFile(
		context.Background(), nil, platform, sourceImageTestRow(), prop, true, "property:image-boxart", 1024,
	)
	require.NoError(t, err)
	assert.False(t, stale)
	assert.Equal(t, content, data)
}
