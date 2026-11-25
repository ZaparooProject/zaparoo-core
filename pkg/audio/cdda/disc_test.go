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

package cdda

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeRead = errors.New("fake read failed")

// fakeDrive serves a TOC and PCM laid out by pcm, numbered by absolute frame.
type fakeDrive struct {
	path  string
	text  []byte
	reads []int
	toc   cdrom.TOC
	// failFrom fails reads at or past this LBA when it is above zero.
	failFrom int
	status   cdrom.Status
	mu       syncutil.Mutex
	closed   bool
}

func (f *fakeDrive) Status() (cdrom.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}
func (*fakeDrive) MediaChanged() (bool, error)   { return false, nil }
func (*fakeDrive) IsDVD() bool                   { return false }
func (f *fakeDrive) ReadTOC() (cdrom.TOC, error) { return f.toc, nil }
func (f *fakeDrive) ReadCDTextPacks() ([]byte, error) {
	if f.text == nil {
		return nil, errFakeRead
	}
	return f.text, nil
}

func (*fakeDrive) ReadAtContext(context.Context, []byte, int64) (int, error) {
	return 0, errFakeRead
}

func (*fakeDrive) ReadDataSector(context.Context, int, []byte) error { return errFakeRead }

func (f *fakeDrive) ReadAudio(_ context.Context, lba, count int, p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, lba)
	if count > readAheadSectors || len(p) != count*SectorSize {
		return errors.New("bad read size")
	}
	if f.failFrom > 0 && lba+count > f.failFrom {
		return errFakeRead
	}
	copy(p, pcm(lba*FramesPerSector, count*FramesPerSector))
	return nil
}

func (f *fakeDrive) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeDrive) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeDrive) open(path string) (cdrom.Drive, error) {
	f.path = path
	return f, nil
}

// textPacks lays NUL-separated strings out as CD-Text packs from track 0.
func textPacks(packType byte, text string) []byte {
	raw := []byte(text)
	if pad := len(raw) % 12; pad != 0 {
		raw = append(raw, make([]byte, 12-pad)...)
	}
	out := make([]byte, 0, len(raw)/12*18)
	for off := 0; off < len(raw); off += 12 {
		pack := make([]byte, 18)
		pack[0] = packType
		copy(pack[4:], raw[off:off+12])
		out = append(out, pack...)
	}
	return out
}

func threeTrackDisc() *fakeDrive {
	return &fakeDrive{
		status: cdrom.StatusDiscOK,
		toc: cdrom.TOC{Tracks: []cdrom.Track{
			{Number: 1, Start: 0, End: 40},
			{Number: 2, Start: 40, End: 100},
			{Number: 3, Start: 100, End: 130},
		}, LeadOut: 130},
	}
}

func TestParseCDAudioPath(t *testing.T) {
	t.Parallel()
	drive, track, err := ParseCDAudioPath("cdaudio://sr0")
	require.NoError(t, err)
	assert.Equal(t, "sr0", drive)
	assert.Equal(t, 0, track)
	drive, track, err = ParseCDAudioPath("cdaudio://sr12/7")
	require.NoError(t, err)
	assert.Equal(t, "sr12", drive)
	assert.Equal(t, 7, track)
	assert.Equal(t, "/dev/sr12", DevicePath(drive))

	for _, bad := range []string{
		"cdaudio://", "cdaudio://sr", "cdaudio://sr1234", "cdaudio://hda", "cdaudio://sr0/0",
		"cdaudio://sr0/01", "cdaudio://sr0/100", "cdaudio://sr0/../x", "cdaudio://sr0/1/2",
		"cdaudio://SR0", "cdaudio://sr0?x=1", "file://sr0", "cdaudio://sr-1",
	} {
		_, _, err := ParseCDAudioPath(bad)
		require.ErrorIs(t, err, ErrBadCDAudioPath, bad)
	}
	assert.True(t, IsCDAudioPath("CDAUDIO://sr0"))
	assert.False(t, IsCDAudioPath("/roms/cdaudio"))
}

func TestOpenDiscNamesFromCDText(t *testing.T) {
	t.Parallel()
	fd := threeTrackDisc()
	fd.text = append(textPacks(0x80, "Disc Title\x00First\x00\x00Third\x00"),
		textPacks(0x81, "Disc Artist\x00")...)
	d, err := OpenDisc("cdaudio://sr7", DiscOptions{Open: fd.open})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()

	assert.Equal(t, "/dev/sr7", fd.path)
	assert.Equal(t, "Disc Title", AlbumName("sr7"))
	assert.Equal(t, []int{0, 40 * FramesPerSector, 100 * FramesPerSector}, d.TrackStarts())
	assert.Equal(t, "First", d.TrackMeta(0).Title)
	assert.Equal(t, "Track 2", d.TrackMeta(1).Title)
	assert.Equal(t, "Disc Artist", d.TrackMeta(1).Artist)
	assert.Equal(t, "Disc Title", d.TrackMeta(2).Album)
	assert.Equal(t, 3, d.TrackMeta(2).TrackTotal)
}

func TestOpenDiscWithoutCDText(t *testing.T) {
	t.Parallel()
	d, err := OpenDisc("cdaudio://sr8/2", DiscOptions{Open: threeTrackDisc().open})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	assert.Equal(t, "Audio CD", AlbumName("sr8"))
	assert.Equal(t, 60*FramesPerSector, d.Len())
	assert.Equal(t, "Track 2", d.TrackMeta(0).Title)
	assert.Equal(t, 2, d.TrackMeta(0).TrackNumber)

	_, err = OpenDisc("cdaudio://sr8/4", DiscOptions{Open: threeTrackDisc().open})
	require.Error(t, err)
}

func TestOpenDiscSkipsDataAndSessionGap(t *testing.T) {
	t.Parallel()
	fd := &fakeDrive{status: cdrom.StatusDiscOK, toc: cdrom.TOC{Tracks: []cdrom.Track{
		{Number: 1, Start: 0, End: 20000},
		{Number: 2, Start: 20000, End: 30000, Data: true},
	}}}
	d, err := OpenDisc("cdaudio://sr9", DiscOptions{Open: fd.open})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	assert.Equal(t, (20000-sessionGapSectors)*FramesPerSector, d.Len())

	data := &fakeDrive{toc: cdrom.TOC{Tracks: []cdrom.Track{{Number: 1, End: 10, Data: true}}}}
	_, err = OpenDisc("cdaudio://sr9", DiscOptions{Open: data.open})
	require.ErrorIs(t, err, ErrNoAudioTracks)
	assert.True(t, data.isClosed())
}

func TestDiscStreamsAcrossChunksAndSeeks(t *testing.T) {
	t.Parallel()
	fd := threeTrackDisc()
	d, err := OpenDisc("cdaudio://sr10", DiscOptions{Open: fd.open})
	require.NoError(t, err)

	out := streamAll(t, d)
	require.Len(t, out, 130*FramesPerSector)
	frames := []int{0, 27*FramesPerSector - 1, 27 * FramesPerSector, 40 * FramesPerSector, 130*FramesPerSector - 1}
	for _, f := range frames {
		assert.InDelta(t, leftAt(f), out[f][0], 1e-9, "frame %d", f)
	}

	require.NoError(t, d.Seek(100*FramesPerSector+17))
	buf := make([][2]float64, 3)
	n, ok := d.Stream(buf)
	require.True(t, ok)
	require.Equal(t, 3, n)
	assert.InDelta(t, leftAt(100*FramesPerSector+17), buf[0][0], 1e-9)

	require.NoError(t, d.Close())
	assert.True(t, fd.isClosed())
	fd.mu.Lock()
	defer fd.mu.Unlock()
	assert.Contains(t, fd.reads, 100)
}

func TestDiscEjectEndsCleanly(t *testing.T) {
	t.Parallel()
	fd := threeTrackDisc()
	fd.failFrom = 60
	fd.status = cdrom.StatusTrayOpen
	d, err := OpenDisc("cdaudio://sr11", DiscOptions{Open: fd.open})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	out := streamAll(t, d)
	assert.Less(t, len(out), 60*FramesPerSector)
	assert.GreaterOrEqual(t, len(out), 27*FramesPerSector)
}

func TestDiscReadErrorWithDiscPresentFails(t *testing.T) {
	t.Parallel()
	fd := threeTrackDisc()
	fd.failFrom = 1
	d, err := OpenDisc("cdaudio://sr12", DiscOptions{Open: fd.open})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	buf := make([][2]float64, 100)
	_, ok := d.Stream(buf)
	assert.False(t, ok)
	require.ErrorIs(t, d.Err(), errFakeRead)
}

func TestDiscCloseStopsReadAhead(t *testing.T) {
	t.Parallel()
	fd := threeTrackDisc()
	d, err := OpenDisc("cdaudio://sr13", DiscOptions{Open: fd.open})
	require.NoError(t, err)
	buf := make([][2]float64, 10)
	_, ok := d.Stream(buf)
	require.True(t, ok)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	assert.True(t, fd.isClosed())
	fd.mu.Lock()
	reads := len(fd.reads)
	fd.mu.Unlock()
	// Read-ahead is bounded, and nothing reads after close returns.
	assert.LessOrEqual(t, reads, readAheadChunks+1)
	fd.mu.Lock()
	defer fd.mu.Unlock()
	assert.Len(t, fd.reads, reads)
}

func TestOpenDiscUnsupported(t *testing.T) {
	t.Parallel()
	open := func(string) (cdrom.Drive, error) { return nil, cdrom.ErrUnsupported }
	_, err := OpenDisc("cdaudio://sr0", DiscOptions{Open: open})
	require.ErrorIs(t, err, cdrom.ErrUnsupported)
}
