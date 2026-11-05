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

package decks

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoversSaveReadPrune(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	covers := &Covers{FS: fs, Dir: filepath.Join("data", "decks", "covers")}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))

	name, err := covers.Save("0123456789AB", buf.Bytes())
	require.NoError(t, err)
	assert.Regexp(t, `^0123456789ab-[0-9a-f]{16}\.png$`, name)
	data, err := covers.Read(name)
	require.NoError(t, err)
	assert.Equal(t, buf.Bytes(), data)

	_, err = covers.Save("0123456789ab", []byte("plain text"))
	require.ErrorIs(t, err, ErrCoverNotImage)
	_, err = covers.Save("0123456789ab", make([]byte, CoverMaxBytes+1))
	require.ErrorIs(t, err, ErrCoverTooLarge)
	_, err = covers.Read("../escape.png")
	require.ErrorIs(t, err, ErrCoverNotFound)

	// A new file survives a prune; an old one no deck names does not.
	covers.Prune(map[string]struct{}{}, time.Now())
	_, err = covers.Read(name)
	require.NoError(t, err)
	covers.Prune(map[string]struct{}{name: {}}, time.Now().Add(time.Hour))
	_, err = covers.Read(name)
	require.NoError(t, err)
	covers.Prune(map[string]struct{}{}, time.Now().Add(time.Hour))
	_, err = covers.Read(name)
	require.ErrorIs(t, err, ErrCoverNotFound)
}
