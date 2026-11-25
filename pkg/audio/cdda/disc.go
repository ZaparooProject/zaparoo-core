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
	"fmt"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// ErrBadCDAudioPath is returned for a cdaudio:// path that does not name a
// drive, and optionally a track, in the accepted form.
var ErrBadCDAudioPath = errors.New("cd audio path must be cdaudio://srN or cdaudio://srN/<track>")

const (
	// readAheadSectors is how many sectors one drive read pulls.
	readAheadSectors = 27
	// readAheadChunks is how many reads are kept ahead of playback, about
	// two seconds.
	readAheadChunks = 6
	// sessionGapSectors is the lead-out, lead-in and pregap between the
	// audio session of an enhanced CD and its data session. The TOC counts
	// it as part of the last audio track, but it cannot be read.
	sessionGapSectors = 11400
	// maxDriveDigits bounds the number in a drive name.
	maxDriveDigits = 3
	// defaultAlbumName names a disc with no CD-Text title.
	defaultAlbumName = "Audio CD"
)

// IsCDAudioPath reports whether path uses the cdaudio scheme, valid or not.
func IsCDAudioPath(path string) bool {
	prefix := readers.CDAudioScheme + "://"
	return len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}

// ParseCDAudioPath splits cdaudio://srN[/n] into the drive name and the
// track, 0 for the whole disc. Only drive names of the form srN are
// accepted, so no other device can be opened.
func ParseCDAudioPath(path string) (drive string, track int, err error) {
	if !IsCDAudioPath(path) {
		return "", 0, ErrBadCDAudioPath
	}
	rest := path[len(readers.CDAudioScheme)+3:]
	drive, num, hasTrack := strings.Cut(rest, "/")
	if !validDriveName(drive) {
		return "", 0, ErrBadCDAudioPath
	}
	if !hasTrack {
		return drive, 0, nil
	}
	n, convErr := strconv.Atoi(num)
	if convErr != nil || n < 1 || n > MaxTracks || strconv.Itoa(n) != num {
		return "", 0, ErrBadCDAudioPath
	}
	return drive, n, nil
}

func validDriveName(name string) bool {
	digits, ok := strings.CutPrefix(name, "sr")
	if !ok || digits == "" || len(digits) > maxDriveDigits {
		return false
	}
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// DevicePath is the device node of a validated drive name.
func DevicePath(drive string) string {
	return "/dev/" + drive
}

// DiscOptions configure OpenDisc.
type DiscOptions struct {
	// Open opens a drive; nil is cdrom.Open.
	Open func(path string) (cdrom.Drive, error)
}

// albumNames remembers the album title of the disc last opened in each
// drive, for naming what is playing.
type albumNames struct {
	names map[string]string
	mu    syncutil.Mutex
}

var discAlbums = albumNames{names: make(map[string]string)}

// AlbumName is the title of the disc last opened in drive, or "Audio CD".
func AlbumName(drive string) string {
	discAlbums.mu.Lock()
	defer discAlbums.mu.Unlock()
	if name := discAlbums.names[drive]; name != "" {
		return name
	}
	return defaultAlbumName
}

func rememberAlbum(drive, title string) {
	discAlbums.mu.Lock()
	defer discAlbums.mu.Unlock()
	if title == "" {
		delete(discAlbums.names, drive)
		return
	}
	discAlbums.names[drive] = title
}

// OpenDisc opens the audio of the disc in the drive a cdaudio:// path names.
func OpenDisc(path string, opts DiscOptions) (*Decoder, error) {
	drive, track, err := ParseCDAudioPath(path)
	if err != nil {
		return nil, err
	}
	open := opts.Open
	if open == nil {
		open = cdrom.Open
	}
	d, err := open(DevicePath(drive))
	if err != nil {
		return nil, fmt.Errorf("open drive %s: %w", drive, err)
	}
	album, err := discAlbum(d)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	rememberAlbum(drive, album.Title)
	if track > 0 {
		if err := album.only(track); err != nil {
			_ = d.Close()
			return nil, err
		}
	}
	return newDecoder(album, newDiscReader(d, album)), nil
}

// discAlbum lays out a disc's audio tracks from its TOC and names them from
// its CD-Text.
func discAlbum(d cdrom.Drive) (*Album, error) {
	toc, err := d.ReadTOC()
	if err != nil {
		return nil, fmt.Errorf("read toc: %w", err)
	}
	var text cdrom.CDText
	if packs, err := d.ReadCDTextPacks(); err == nil {
		text = cdrom.ParseCDText(packs)
	} else {
		log.Debug().Err(err).Msg("audio cd has no cd-text")
	}
	album := &Album{
		Title:     clipText(text.Title(0)),
		Performer: clipText(text.Artist(0)),
	}
	for i, t := range toc.Tracks {
		if t.Data || t.Number < 1 || t.Number > MaxTracks || len(album.Tracks) >= MaxTracks {
			continue
		}
		end := t.End
		if i+1 < len(toc.Tracks) && toc.Tracks[i+1].Data && end-sessionGapSectors > t.Start {
			end -= sessionGapSectors
		}
		if end <= t.Start || t.Start < 0 {
			continue
		}
		title := clipText(text.Title(t.Number))
		if title == "" {
			title = "Track " + strconv.Itoa(t.Number)
		}
		artist := clipText(text.Artist(t.Number))
		if artist == "" {
			artist = album.Performer
		}
		album.Tracks = append(album.Tracks, Track{
			Title:     title,
			Performer: artist,
			Number:    t.Number,
			Start:     int64(t.Start) * FramesPerSector,
			End:       int64(end) * FramesPerSector,
		})
		album.Total = max(album.Total, t.Number)
	}
	if len(album.Tracks) == 0 {
		return nil, ErrNoAudioTracks
	}
	return album, nil
}

// discChunk is one read of consecutive sectors.
type discChunk struct {
	data  []byte
	lba   int
	count int
}

// discReader reads a disc a couple of seconds ahead of playback on a
// goroutine of its own, so a slow drive does not starve the audio.
type discReader struct {
	drive  cdrom.Drive
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// producerWake and consumerWake hold a pending wake-up for each side.
	producerWake chan struct{}
	consumerWake chan struct{}
	err          error
	// runs are the playable sector ranges, merged where tracks touch.
	runs  [][2]int
	queue []discChunk
	free  [][]byte
	mu    syncutil.Mutex
	// next is the sector the producer reads next; gen counts restarts, so a
	// read that finishes after one is dropped.
	next   int
	gen    int
	closed bool
}

func newDiscReader(d cdrom.Drive, album *Album) *discReader {
	ctx, cancel := context.WithCancel(context.Background())
	r := &discReader{
		drive:        d,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		producerWake: make(chan struct{}, 1),
		consumerWake: make(chan struct{}, 1),
		next:         -1,
	}
	for i := range album.Tracks {
		start := int(album.Tracks[i].Start / FramesPerSector)
		end := int(album.Tracks[i].End / FramesPerSector)
		if n := len(r.runs); n > 0 && r.runs[n-1][1] == start {
			r.runs[n-1][1] = end
			continue
		}
		r.runs = append(r.runs, [2]int{start, end})
	}
	go r.produce()
	return r
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// runEnd is the end of the playable range holding lba, or -1.
func (r *discReader) runEnd(lba int) int {
	for _, run := range r.runs {
		if lba >= run[0] && lba < run[1] {
			return run[1]
		}
	}
	return -1
}

func (r *discReader) produce() {
	defer close(r.done)
	for {
		r.mu.Lock()
		end := -1
		if r.next >= 0 {
			end = r.runEnd(r.next)
		}
		if r.closed {
			r.mu.Unlock()
			return
		}
		if r.err != nil || end < 0 || len(r.queue) >= readAheadChunks {
			r.mu.Unlock()
			select {
			case <-r.producerWake:
				continue
			case <-r.ctx.Done():
				return
			}
		}
		lba, gen := r.next, r.gen
		count := min(readAheadSectors, end-lba)
		var buf []byte
		if n := len(r.free); n > 0 {
			buf, r.free = r.free[n-1], r.free[:n-1]
		} else {
			buf = make([]byte, readAheadSectors*SectorSize)
		}
		r.mu.Unlock()

		err := r.drive.ReadAudio(r.ctx, lba, count, buf[:count*SectorSize])
		if err != nil && r.ctx.Err() == nil && r.ejected() {
			err = fmt.Errorf("%w: %w", errMediaGone, err)
		}

		r.mu.Lock()
		switch {
		case gen != r.gen || r.closed:
			r.free = append(r.free, buf)
		case err != nil:
			r.free = append(r.free, buf)
			r.err = err
		default:
			r.queue = append(r.queue, discChunk{data: buf, lba: lba, count: count})
			r.next = lba + count
		}
		r.mu.Unlock()
		wake(r.consumerWake)
	}
}

// ejected reports whether a read failed because the disc is gone.
func (r *discReader) ejected() bool {
	status, err := r.drive.Status()
	return err == nil && status != cdrom.StatusDiscOK && status != cdrom.StatusUnknown
}

// readFrames copies frames from the read-ahead queue, waiting for the
// producer when it is behind and restarting it after a seek.
func (r *discReader) readFrames(_ *Track, off int64, p []byte) error {
	for len(p) > 0 {
		lba := int(off / FramesPerSector)
		within := int(off%FramesPerSector) * bytesPerFrame
		n, err := r.take(lba, within, p)
		if err != nil {
			return err
		}
		p = p[n:]
		off += int64(n / bytesPerFrame)
	}
	return nil
}

// take copies what it can of sector lba, from byte within, into p.
func (r *discReader) take(lba, within int, p []byte) (int, error) {
	for {
		r.mu.Lock()
		for len(r.queue) > 0 && r.queue[0].lba+r.queue[0].count <= lba {
			r.free = append(r.free, r.queue[0].data)
			r.queue = r.queue[1:]
		}
		if len(r.queue) > 0 && r.queue[0].lba <= lba {
			c := &r.queue[0]
			start := (lba-c.lba)*SectorSize + within
			n := copy(p, c.data[start:c.count*SectorSize])
			r.mu.Unlock()
			wake(r.producerWake)
			return n, nil
		}
		if len(r.queue) == 0 && r.next == lba && r.err != nil {
			err := r.err
			r.mu.Unlock()
			return 0, err
		}
		if len(r.queue) != 0 || r.next != lba {
			// A seek, or the first read: drop what was read ahead and start
			// over from lba.
			for _, c := range r.queue {
				r.free = append(r.free, c.data)
			}
			r.queue = r.queue[:0]
			r.next = lba
			r.err = nil
			r.gen++
			wake(r.producerWake)
		}
		r.mu.Unlock()
		select {
		case <-r.consumerWake:
		case <-r.ctx.Done():
			return 0, fmt.Errorf("read disc: %w", r.ctx.Err())
		}
	}
}

// close stops the read-ahead, waits for a read in flight, and closes the
// drive.
func (r *discReader) close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	r.cancel()
	<-r.done
	if err := r.drive.Close(); err != nil {
		return fmt.Errorf("close drive: %w", err)
	}
	return nil
}
