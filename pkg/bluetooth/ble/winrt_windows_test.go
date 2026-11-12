//go:build windows

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

package ble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferRoundTrip(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureRuntime())
	for _, data := range [][]byte{{0x00, 0x01, 0xff}, {}} {
		buf, err := bytesBuffer(data)
		require.NoError(t, err)
		got, err := bufferBytes(buf)
		releaseBuffer(buf)
		require.NoError(t, err)
		if len(data) == 0 {
			assert.Empty(t, got)
			continue
		}
		assert.Equal(t, data, got)
	}

	got, err := bufferBytes(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
	releaseBuffer(nil)
}
