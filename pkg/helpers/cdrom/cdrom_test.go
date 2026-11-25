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

package cdrom

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "no_disc", StatusNoDisc.String())
	assert.Equal(t, "tray_open", StatusTrayOpen.String())
	assert.Equal(t, "not_ready", StatusNotReady.String())
	assert.Equal(t, "disc_ok", StatusDiscOK.String())
	assert.Equal(t, "unknown", StatusUnknown.String())
	assert.Equal(t, "unknown", Status(99).String())
}

func TestTrackSectors(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 150, Track{Start: 100, End: 250}.Sectors())
	assert.Equal(t, 0, Track{Start: 100, End: 100}.Sectors())
	assert.Equal(t, 0, Track{Start: 100, End: 50}.Sectors())
}

func mixedTOC() TOC {
	return TOC{
		Tracks: []Track{
			{Number: 1, Start: 0, End: 1000},
			{Number: 2, Start: 1000, End: 5000, Data: true},
			{Number: 3, Start: 5000, End: 7000},
			{Number: 4, Start: 7000, End: 9000, Data: true},
		},
		LeadOut: 9000,
	}
}

func TestTOCFirstDataTrack(t *testing.T) {
	t.Parallel()

	tr, ok := mixedTOC().FirstDataTrack()
	require.True(t, ok)
	assert.Equal(t, 2, tr.Number)
	assert.Equal(t, 1000, tr.Start)

	_, ok = TOC{Tracks: []Track{{Number: 1}, {Number: 2}}}.FirstDataTrack()
	assert.False(t, ok)

	_, ok = TOC{}.FirstDataTrack()
	assert.False(t, ok)
}

func TestTOCAudioTracks(t *testing.T) {
	t.Parallel()

	audio := mixedTOC().AudioTracks()
	require.Len(t, audio, 2)
	assert.Equal(t, 1, audio[0].Number)
	assert.Equal(t, 3, audio[1].Number)

	assert.Empty(t, TOC{}.AudioTracks())
	assert.Empty(t, TOC{Tracks: []Track{{Number: 1, Data: true}}}.AudioTracks())
}

func TestTOCTrack(t *testing.T) {
	t.Parallel()
	toc := mixedTOC()

	tr, ok := toc.Track(3)
	require.True(t, ok)
	assert.Equal(t, Track{Number: 3, Start: 5000, End: 7000}, tr)

	_, ok = toc.Track(5)
	assert.False(t, ok)
	_, ok = toc.Track(0)
	assert.False(t, ok)
}

func TestTOCIsAudio(t *testing.T) {
	t.Parallel()
	assert.False(t, TOC{}.IsAudio(), "no tracks is not an audio disc")
	assert.True(t, TOC{Tracks: []Track{{Number: 1}, {Number: 2}}}.IsAudio())
	assert.False(t, mixedTOC().IsAudio())
	assert.False(t, TOC{Tracks: []Track{{Number: 1, Data: true}}}.IsAudio())
}

func TestTOCFingerprint(t *testing.T) {
	t.Parallel()

	base := TOC{
		Tracks:  []Track{{Number: 1, Start: 0}, {Number: 2, Start: 15000}},
		LeadOut: 30000,
	}
	fp := base.Fingerprint()
	assert.NotEmpty(t, fp)
	assert.Regexp(t, `^[0-9a-f]+$`, fp)

	same := TOC{
		Tracks:  []Track{{Number: 1, Start: 0, End: 15000}, {Number: 2, Start: 15000, End: 30000}},
		LeadOut: 30000,
	}
	assert.Equal(t, fp, same.Fingerprint(), "End and Number do not change the fingerprint")

	leadOut := base
	leadOut.LeadOut = 30001
	assert.NotEqual(t, fp, leadOut.Fingerprint())

	start := TOC{Tracks: []Track{{Number: 1, Start: 0}, {Number: 2, Start: 15001}}, LeadOut: 30000}
	assert.NotEqual(t, fp, start.Fingerprint())

	data := TOC{Tracks: []Track{{Number: 1, Start: 0}, {Number: 2, Start: 15000, Data: true}}, LeadOut: 30000}
	assert.NotEqual(t, fp, data.Fingerprint())

	extra := TOC{
		Tracks:  []Track{{Number: 1, Start: 0}, {Number: 2, Start: 15000}, {Number: 3, Start: 20000}},
		LeadOut: 30000,
	}
	assert.NotEqual(t, fp, extra.Fingerprint())

	assert.NotEmpty(t, TOC{}.Fingerprint())
}

func TestFinishTOC(t *testing.T) {
	t.Parallel()

	toc, err := finishTOC([]Track{
		{Number: 1, Start: 0},
		{Number: 2, Start: 1000, Data: true},
		{Number: 3, Start: 4000},
	}, 9000)
	require.NoError(t, err)
	assert.Equal(t, 9000, toc.LeadOut)
	assert.Equal(t, []Track{
		{Number: 1, Start: 0, End: 1000},
		{Number: 2, Start: 1000, End: 4000, Data: true},
		{Number: 3, Start: 4000, End: 9000},
	}, toc.Tracks)

	single, err := finishTOC([]Track{{Number: 1, Start: 150}}, 150)
	require.NoError(t, err, "a zero-length last track is allowed")
	assert.Equal(t, 0, single.Tracks[0].Sectors())
}

func TestFinishTOCRejects(t *testing.T) {
	t.Parallel()

	tooMany := make([]Track, MaxTracks+1)
	for i := range tooMany {
		tooMany[i] = Track{Number: i + 1, Start: i * 10}
	}
	maxed := make([]Track, MaxTracks)
	copy(maxed, tooMany)

	tests := []struct {
		name    string
		tracks  []Track
		leadOut int
		wantErr bool
	}{
		{name: "no tracks", tracks: nil, leadOut: 100, wantErr: true},
		{name: "too many tracks", tracks: tooMany, leadOut: 10000, wantErr: true},
		{name: "max tracks", tracks: maxed, leadOut: 10000},
		{name: "negative start", tracks: []Track{{Number: 1, Start: -1}}, leadOut: 100, wantErr: true},
		{
			name:    "out of order",
			tracks:  []Track{{Number: 1, Start: 500}, {Number: 2, Start: 100}},
			leadOut: 1000,
			wantErr: true,
		},
		{
			name:    "lead-out before last track",
			tracks:  []Track{{Number: 1, Start: 0}, {Number: 2, Start: 500}},
			leadOut: 400,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toc, err := finishTOC(tt.tracks, tt.leadOut)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, toc.Tracks)
				return
			}
			require.NoError(t, err)
			assert.Len(t, toc.Tracks, len(tt.tracks))
		})
	}
}
