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

package memguard

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchTripsOnceWhenOverLimit(t *testing.T) {
	t.Parallel()

	samples := []uint64{10, 50, 101, 500}
	next := 0
	read := func() (uint64, error) {
		v := samples[next]
		next++
		return v, nil
	}
	var trips []uint64
	watch(100, time.Millisecond, read, func(rss, limit uint64) {
		trips = append(trips, rss)
		assert.Equal(t, uint64(100), limit)
	})

	assert.Equal(t, []uint64{101}, trips)
	assert.Equal(t, 3, next, "the watch stops after tripping")
}

func TestWatchStopsWithoutTrippingWhenReadFails(t *testing.T) {
	t.Parallel()

	read := func() (uint64, error) { return 0, errors.New("no proc") }
	watch(100, time.Millisecond, read, func(uint64, uint64) {
		t.Error("a failed read must not trip the guard")
	})
}

func TestMessageNamesBinaryAndSizes(t *testing.T) {
	t.Parallel()

	msg := message("midi.test", 3<<30, DefaultLimit)
	assert.Contains(t, msg, "midi.test")
	assert.Contains(t, msg, "3072 MiB")
	assert.Contains(t, msg, "2048 MiB")
}

func TestResidentBytesReadsThisProcess(t *testing.T) {
	t.Parallel()

	if !supported {
		t.Skip("resident memory is only read on Linux")
	}
	rss, err := residentBytes()
	require.NoError(t, err)
	assert.Greater(t, rss, uint64(1<<20), "a running test binary is over a megabyte")
}
