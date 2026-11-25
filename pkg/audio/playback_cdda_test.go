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

package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/cdda"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietDevice installs a shared device that never opens real output.
func quietDevice(t *testing.T) {
	t.Helper()
	oldDevice := globalDevice
	testDevice := newSharedDevice()
	testDevice.opening = true
	globalDevice = testDevice
	t.Cleanup(func() { globalDevice = oldDevice })
}

func writeCueRip(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cue := "TITLE \"Album\"\nFILE \"album.bin\" BINARY\n" +
		"  TRACK 01 AUDIO\n    TITLE \"One\"\n    INDEX 01 00:00:00\n" +
		"  TRACK 02 AUDIO\n    TITLE \"Two\"\n    INDEX 01 00:01:00\n"
	cuePath := filepath.Join(dir, "album.cue")
	require.NoError(t, os.WriteFile(cuePath, []byte(cue), 0o600))
	bin := make([]byte, 150*cdda.SectorSize)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "album.bin"), bin, 0o600))
	return cuePath
}

// Not parallel: replaces the shared output device.
func TestPlayCueAlbumSkipsBetweenTracks(t *testing.T) {
	quietDevice(t)
	cuePath := writeCueRip(t)
	m := NewLongformPlaybackManager(false)

	require.NoError(t, m.Play(mediaslot.Primary, cuePath, PlaybackOptions{}))
	src := m.primary
	require.NotNil(t, src)
	t.Cleanup(src.stopAndDeregister)
	st := m.State(mediaslot.Primary)
	assert.True(t, st.MultiTrack)
	require.NotNil(t, st.Meta)
	assert.Equal(t, "One", st.Meta.Title)
	assert.Equal(t, "Album", st.Meta.Album)
	assert.Equal(t, 1, st.Meta.TrackNumber)
	assert.Equal(t, 2, st.Meta.TrackTotal)
	assert.InDelta(t, 2.0, st.Duration.Seconds(), 0.01)

	// The second track's tags follow playback into it.
	src.played.Store(int64(targetSampleRate) + 1)
	st = m.State(mediaslot.Primary)
	require.NotNil(t, st.Meta)
	assert.Equal(t, "Two", st.Meta.Title)
	assert.Equal(t, 2, st.Meta.TrackNumber)
}

// Not parallel: replaces the shared output device.
func TestPlayCueSingleTrack(t *testing.T) {
	quietDevice(t)
	cuePath := writeCueRip(t)
	m := NewLongformPlaybackManager(false)

	require.NoError(t, m.Play(mediaslot.Primary, cuePath+"/2", PlaybackOptions{}))
	src := m.primary
	require.NotNil(t, src)
	t.Cleanup(src.stopAndDeregister)
	st := m.State(mediaslot.Primary)
	assert.False(t, st.MultiTrack)
	require.NotNil(t, st.Meta)
	assert.Equal(t, "Two", st.Meta.Title)
	assert.InDelta(t, 1.0, st.Duration.Seconds(), 0.01)

	require.Error(t, m.Play(mediaslot.Primary, cuePath+"/3", PlaybackOptions{}))
}

// silentDisc is a one-track audio CD of silence.
type silentDisc struct{ closed chan struct{} }

func (silentDisc) Status() (cdrom.Status, error) { return cdrom.StatusDiscOK, nil }
func (silentDisc) MediaChanged() (bool, error)   { return false, nil }
func (silentDisc) IsDVD() bool                   { return false }
func (silentDisc) ReadTOC() (cdrom.TOC, error) {
	return cdrom.TOC{Tracks: []cdrom.Track{{Number: 1, End: 75}}, LeadOut: 75}, nil
}

func (silentDisc) ReadCDTextPacks() ([]byte, error) { return nil, errors.New("no cd-text") }
func (silentDisc) ReadAtContext(context.Context, []byte, int64) (int, error) {
	return 0, errors.New("not data")
}

func (silentDisc) ReadDataSector(context.Context, int, []byte) error { return errors.New("not data") }

func (silentDisc) ReadAudio(_ context.Context, _, _ int, p []byte) error {
	clear(p)
	return nil
}

func (d silentDisc) Close() error {
	close(d.closed)
	return nil
}

// Not parallel: replaces the shared output device.
func TestPlayCDAudioOpensDrive(t *testing.T) {
	quietDevice(t)
	disc := silentDisc{closed: make(chan struct{})}
	var opened string
	m := NewLongformPlaybackManager(false)
	m.openDrive = func(path string) (cdrom.Drive, error) {
		opened = path
		return disc, nil
	}

	require.NoError(t, m.Play(mediaslot.Primary, "cdaudio://sr3", PlaybackOptions{}))
	src := m.primary
	require.NotNil(t, src)
	assert.Equal(t, "/dev/sr3", opened)
	st := m.State(mediaslot.Primary)
	require.NotNil(t, st.Meta)
	assert.Equal(t, "Track 1", st.Meta.Title)
	assert.InDelta(t, 1.0, st.Duration.Seconds(), 0.01)

	src.stopAndDeregister()
	<-disc.closed
}
