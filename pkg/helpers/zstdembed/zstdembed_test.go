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

package zstdembed

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "one line\n", strings.Repeat("title\tgenre:action\n", 5000)} {
		packed, err := Compress([]byte(input))
		require.NoError(t, err)
		r, err := NewReader(bytes.NewReader(packed))
		require.NoError(t, err)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, input, string(got))
	}
}

func TestCompressIsDeterministic(t *testing.T) {
	t.Parallel()
	input := []byte(strings.Repeat("dkong\tDonkey Kong\t1981\tNintendo\n", 2000))
	first, err := Compress(input)
	require.NoError(t, err)
	second, err := Compress(input)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Less(t, len(first), len(input)/10)
}

func TestReaderRejectsGarbage(t *testing.T) {
	t.Parallel()
	r, err := NewReader(strings.NewReader("not a zstd stream"))
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	require.Error(t, err)
	require.NoError(t, r.Close())
}
