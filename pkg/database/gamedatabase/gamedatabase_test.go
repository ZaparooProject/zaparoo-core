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

package gamedatabase

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionNamesTheUpstreamCommit(t *testing.T) {
	t.Parallel()
	assert.Regexp(t, `^[0-9a-f]{40} \d{4}-\d{2}-\d{2}$`, Version())
}

func TestBundlesAreSortedAndReadable(t *testing.T) {
	t.Parallel()
	bundles := Bundles()
	require.Len(t, bundles, 24)
	assert.IsIncreasing(t, bundles)
	assert.Contains(t, bundles, "arcade")
	assert.Contains(t, bundles, "snes")

	total := 0
	for _, bundle := range bundles {
		rows := 0
		require.NoError(t, ForEach(bundle, func(row *Row) error {
			rows++
			if row.Title == "" || row.TitleScreen == "" {
				t.Errorf("%s row %d has no title", bundle, rows)
			}
			return nil
		}), bundle)
		assert.Positive(t, rows, bundle)
		total += rows
	}
	assert.Equal(t, 29394, total, "the row count of the pinned upstream commit")
}

func TestForEachReadsRowsInUpstreamOrder(t *testing.T) {
	t.Parallel()
	var first Row
	stop := errors.New("stop")
	err := ForEach("virtualboy", func(row *Row) error {
		first = *row
		return stop
	})
	require.ErrorIs(t, err, stop, "the callback's error ends the read and is returned")
	assert.Equal(t, Row{
		Title: "Galactic Pinball", TitleScreen: "Galactic Pinball", ID: "galacticpinball", Region: "Japan",
		Date: "1995-07-21", Developer: "Intelligent Systems", Publisher: "Nintendo",
		Tags: "#players:1 #genre:parlor>pinball #embedded:backup>battery #save:backup #search:3d>stereo #lang:en",
	}, first)
}

func TestScreenTitleResolvesTheSameAsTitleMarker(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Mario Clash", (&Row{Title: "Mario Clash", TitleScreen: SameAsTitle}).ScreenTitle())
	named := &Row{Title: "Super Alfred Chicken", TitleScreen: "Alfred Chicken"}
	assert.Equal(t, "Alfred Chicken", named.ScreenTitle())
}

func TestForEachRefusesAnUnknownBundle(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "nosuchplatform", "../data/snes", "snes.tsv.zst", "VERSION", `a\b`} {
		err := ForEach(name, func(*Row) error { return nil })
		require.ErrorIs(t, err, ErrUnknownBundle, "%q", name)
	}
}

func BenchmarkForEach(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if err := ForEach("snes", func(*Row) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}
