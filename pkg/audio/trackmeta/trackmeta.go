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

// Package trackmeta reads the tags and cover art of an audio track.
//
// Tag data is untrusted: every read is bounded by a byte cap checked against
// the size the file declares before anything is parsed, structures whose
// lengths do not fit the data they sit in are refused before the tag parser
// sees them, and a panic inside the parser is recovered.
package trackmeta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dhowden/tag"
)

// maxFieldBytes caps each text field, in bytes of UTF-8.
const maxFieldBytes = 256

// id3v1Size is the fixed size of an ID3v1 tag at the end of a file.
const id3v1Size = 128

var (
	// ErrNoTags is returned when a track has no tags this package reads.
	ErrNoTags = errors.New("no track tags")
	// ErrTagsTooLarge is returned when a track declares more tag data than
	// the cap allows.
	ErrTagsTooLarge = errors.New("track tags exceed size limit")
	// ErrMalformedTags is returned when a track's tags do not fit together.
	ErrMalformedTags = errors.New("malformed track tags")
	// ErrNoArt is returned by ReadPicture when a track has no cover art.
	ErrNoArt = errors.New("no cover art")
)

// Meta is what a track's tags say about it. Empty fields were not tagged.
type Meta struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int
	// ReplayGainDB is the track's ReplayGain adjustment, valid when
	// HasReplayGain is set.
	ReplayGainDB  float64
	HasReplayGain bool
	// HasArt reports cover art either embedded in the track or as an image
	// beside it.
	HasArt bool
}

// Picture source names reported by ReadPicture.
const (
	SourceEmbedded = "embedded"
	SourceFolder   = "folder"
)

// Picture is a track's cover art.
type Picture struct {
	// Path is the file the bytes came from: the track for embedded art, the
	// image file for folder art.
	Path     string
	MIMEType string
	Source   string
	Data     []byte
}

// folderArtNames are the base names of a cover image beside a track, in the
// order they are preferred. They are matched without regard to case.
var folderArtNames = []string{"cover", "folder", "front"}

// folderArtExts are the image extensions folder art is looked for with.
var folderArtExts = []string{".jpg", ".jpeg", ".png"}

// Read returns the tags of the track at path. Tag data larger than
// maxTagBytes is not parsed; the returned Meta then only reports folder art.
// It returns ErrNoTags when there is nothing to report at all.
func Read(path string, maxTagBytes int64) (*Meta, error) {
	md, err := readTags(path, maxTagBytes)
	meta := &Meta{}
	if err == nil && md != nil {
		fillMeta(meta, md)
		if pic := md.Picture(); pic != nil && len(pic.Data) > 0 {
			meta.HasArt = true
		}
	}
	if !meta.HasArt {
		if _, ok := findFolderArt(path); ok {
			meta.HasArt = true
		}
	}
	if *meta == (Meta{}) {
		if err != nil {
			return nil, err
		}
		return nil, ErrNoTags
	}
	return meta, nil
}

// ReadPicture returns the cover art of the track at path: the picture
// embedded in its tags, else a cover, folder or front image beside it. Art
// larger than maxBytes is not returned.
func ReadPicture(path string, maxBytes int64) (*Picture, error) {
	md, tagsErr := readTags(path, maxBytes)
	if tagsErr == nil && md != nil {
		if pic := md.Picture(); pic != nil && len(pic.Data) > 0 && int64(len(pic.Data)) <= maxBytes {
			return &Picture{
				Path:     path,
				MIMEType: pictureMIME(pic.MIMEType, pic.Data),
				Source:   SourceEmbedded,
				Data:     pic.Data,
			}, nil
		}
	}
	// Tags over the limit were not read, so embedded art may exist. Keep
	// that failure apart from the folder art's own, which can also be a
	// size failure.
	var limitErr error
	if errors.Is(tagsErr, ErrTagsTooLarge) {
		limitErr = tagsErr
	}
	artPath, ok := findFolderArt(path)
	if !ok {
		if limitErr != nil {
			return nil, limitErr
		}
		return nil, ErrNoArt
	}
	data, err := readFileLimited(artPath, maxBytes)
	if err != nil {
		if limitErr != nil {
			return nil, fmt.Errorf("folder art: %w (after tags: %w)", err, limitErr)
		}
		return nil, err
	}
	return &Picture{
		Path:     artPath,
		MIMEType: pictureMIME("", data),
		Source:   SourceFolder,
		Data:     data,
	}, nil
}

// FolderArtPath returns the cover image beside the track at path, if any.
func FolderArtPath(path string) (string, bool) {
	return findFolderArt(path)
}

func pictureMIME(declared string, data []byte) string {
	if detected := sniffImage(data); detected != "" {
		return detected
	}
	return strings.ToLower(strings.TrimSpace(declared))
}

func sniffImage(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// readTags parses the tags of the track at path from a bounded copy of the
// bytes that hold them.
func readTags(path string, maxTagBytes int64) (md tag.Metadata, err error) {
	//nolint:gosec // G304: the path is the track Core is already playing.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open track: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat track: %w", err)
	}
	size := info.Size()

	head := make([]byte, 10)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read track header: %w", err)
	}
	head = head[:n]

	return parseTags(f, head, size, maxTagBytes)
}

// parseTags reads the tag block r declares, after head, and parses it.
func parseTags(r io.ReaderAt, head []byte, size, maxTagBytes int64) (md tag.Metadata, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			md = nil
			err = fmt.Errorf("%w: parser panic: %v", ErrMalformedTags, rec)
		}
	}()

	switch {
	case len(head) >= 10 && string(head[0:3]) == "ID3":
		tagSize := int64(syncsafe(head[6:10])) + 10
		if head[5]&0x10 != 0 {
			tagSize += 10 // footer
		}
		buf, readErr := readRegion(r, tagSize, size, maxTagBytes)
		if readErr != nil {
			return nil, readErr
		}
		if !validID3v2Frames(buf) {
			return nil, ErrMalformedTags
		}
		return parsed(tag.ReadID3v2Tags(bytes.NewReader(buf)))
	case len(head) >= 4 && string(head[0:4]) == "fLaC":
		end, scanErr := flacMetadataEnd(r, size, maxTagBytes)
		if scanErr != nil {
			return nil, scanErr
		}
		buf, readErr := readRegion(r, end, size, maxTagBytes)
		if readErr != nil {
			return nil, readErr
		}
		if !validFLACMetadata(buf) {
			return nil, ErrMalformedTags
		}
		return parsed(tag.ReadFLACTags(bytes.NewReader(buf)))
	case len(head) >= 4 && string(head[0:4]) == "OggS":
		end, scanErr := oggCommentEnd(r, size, maxTagBytes)
		if scanErr != nil {
			return nil, scanErr
		}
		buf, readErr := readRegion(r, end, size, maxTagBytes)
		if readErr != nil {
			return nil, readErr
		}
		if !validOggComments(buf) {
			return nil, ErrMalformedTags
		}
		return parsed(tag.ReadOGGTags(bytes.NewReader(buf)))
	case size >= id3v1Size:
		buf := make([]byte, id3v1Size)
		if _, readErr := r.ReadAt(buf, size-id3v1Size); readErr != nil {
			return nil, fmt.Errorf("read id3v1 tag: %w", readErr)
		}
		if string(buf[0:3]) != "TAG" {
			return nil, ErrNoTags
		}
		return parsed(tag.ReadID3v1Tags(bytes.NewReader(buf)))
	}
	return nil, ErrNoTags
}

func parsed(md tag.Metadata, err error) (tag.Metadata, error) {
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedTags, err)
	}
	return md, nil
}

func syncsafe(b []byte) uint32 {
	return uint32(b[0]&0x7f)<<21 | uint32(b[1]&0x7f)<<14 | uint32(b[2]&0x7f)<<7 | uint32(b[3]&0x7f)
}

// readRegion reads the first n bytes of r, refusing more than maxBytes or
// more than the file holds.
func readRegion(r io.ReaderAt, n, size, maxBytes int64) ([]byte, error) {
	if n > maxBytes {
		return nil, ErrTagsTooLarge
	}
	if n > size {
		return nil, ErrMalformedTags
	}
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, 0); err != nil {
		return nil, fmt.Errorf("read track tags: %w", err)
	}
	return buf, nil
}

// validID3v2Frames reports whether every frame an ID3v2 tag declares fits
// inside the tag, so the parser is never asked to read past the bounded
// buffer or allocate for a length the tag cannot hold. buf holds the whole
// tag: header, frames, padding and footer.
func validID3v2Frames(buf []byte) bool {
	if len(buf) < 10 {
		return false
	}
	version, flags := buf[3], buf[5]
	var idSize, headerSize int
	switch version {
	case 2:
		idSize, headerSize = 3, 6
	case 3, 4:
		idSize, headerSize = 4, 10
	default:
		// Not a version the parser reads; it refuses the tag itself.
		return true
	}
	body := buf[10:]
	if flags&0x10 != 0 && version == 4 {
		if len(body) < 10 {
			return false
		}
		body = body[:len(body)-10]
	}
	if version < 4 && flags&0x80 != 0 {
		// Whole-tag unsynchronization: frame sizes count the decoded bytes.
		body = bytes.ReplaceAll(body, []byte{0xFF, 0x00}, []byte{0xFF})
	}
	if version >= 3 && flags&0x40 != 0 {
		if len(body) < 4 {
			return false
		}
		var skip int64
		if version == 3 {
			skip = 4 + int64(binary.BigEndian.Uint32(body))
		} else {
			skip = int64(syncsafe(body))
		}
		if skip < 4 || skip > int64(len(body)) {
			return false
		}
		body = body[skip:]
	}
	for len(body) >= headerSize && body[0] != 0 {
		var size int64
		switch version {
		case 2:
			size = int64(body[3])<<16 | int64(body[4])<<8 | int64(body[5])
		case 3:
			size = int64(binary.BigEndian.Uint32(body[idSize:]))
		default:
			size = int64(syncsafe(body[idSize:]))
		}
		if size > int64(len(body)-headerSize) {
			return false
		}
		body = body[headerSize+int(size):]
	}
	return true
}

// flacMetadataEnd walks the FLAC metadata block headers and returns where the
// last block ends, without reading the blocks themselves.
func flacMetadataEnd(r io.ReaderAt, size, maxBytes int64) (int64, error) {
	offset := int64(4)
	header := make([]byte, 4)
	for {
		if offset+4 > size {
			return 0, ErrMalformedTags
		}
		if _, err := r.ReadAt(header, offset); err != nil {
			return 0, fmt.Errorf("read flac block header: %w", err)
		}
		last := header[0]&0x80 != 0
		length := int64(header[1])<<16 | int64(header[2])<<8 | int64(header[3])
		offset += 4 + length
		if offset > maxBytes {
			return 0, ErrTagsTooLarge
		}
		if last {
			return offset, nil
		}
	}
}

// oggHeaderSize is the fixed part of an Ogg page header.
const oggHeaderSize = 27

// oggCommentEnd walks the Ogg page headers and returns where the page that
// completes the second packet of the first stream ends. That packet is the
// comment header of a Vorbis or Opus stream.
func oggCommentEnd(r io.ReaderAt, size, maxBytes int64) (int64, error) {
	header := make([]byte, oggHeaderSize)
	table := make([]byte, 255)
	var offset int64
	packets := 0
	var serial uint32
	for first := true; ; first = false {
		if offset+oggHeaderSize > size {
			return 0, ErrMalformedTags
		}
		if _, err := r.ReadAt(header, offset); err != nil {
			return 0, fmt.Errorf("read ogg page header: %w", err)
		}
		if string(header[0:4]) != "OggS" {
			return 0, ErrMalformedTags
		}
		pageSerial := binary.LittleEndian.Uint32(header[14:18])
		if first {
			serial = pageSerial
		}
		segments := int(header[26])
		if offset+oggHeaderSize+int64(segments) > size {
			return 0, ErrMalformedTags
		}
		if _, err := r.ReadAt(table[:segments], offset+oggHeaderSize); err != nil {
			return 0, fmt.Errorf("read ogg segment table: %w", err)
		}
		dataLen := int64(0)
		for _, s := range table[:segments] {
			dataLen += int64(s)
			if pageSerial == serial && s < 255 {
				packets++
			}
		}
		offset += oggHeaderSize + int64(segments) + dataLen
		if offset > maxBytes {
			return 0, ErrTagsTooLarge
		}
		if packets >= 2 {
			return offset, nil
		}
	}
}

// validFLACMetadata reports whether every comment and picture block in a
// FLAC metadata region fits inside its own block.
func validFLACMetadata(buf []byte) bool {
	offset := 4
	for {
		if offset+4 > len(buf) {
			return false
		}
		blockType := buf[offset] & 0x7f
		last := buf[offset]&0x80 != 0
		length := int(buf[offset+1])<<16 | int(buf[offset+2])<<8 | int(buf[offset+3])
		start := offset + 4
		end := start + length
		if end > len(buf) {
			return false
		}
		switch blockType {
		case 4:
			if !validVorbisComment(buf[start:end]) {
				return false
			}
		case 6:
			if !validPictureBlock(buf[start:end]) {
				return false
			}
		}
		if last {
			return true
		}
		offset = end
	}
}

// validVorbisComment reports whether a Vorbis comment block's lengths all
// fit inside it, along with any picture it carries.
func validVorbisComment(b []byte) bool {
	rest, ok := skipLE32String(b)
	if !ok || len(rest) < 4 {
		return false
	}
	count := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	for range count {
		if len(rest) < 4 {
			return false
		}
		n, ok := lengthWithin(binary.LittleEndian.Uint32(rest), len(rest)-4)
		if !ok {
			return false
		}
		comment := rest[4 : 4+n]
		rest = rest[4+n:]
		key, value, found := bytes.Cut(comment, []byte("="))
		if !found {
			return false
		}
		if strings.EqualFold(string(key), "metadata_block_picture") {
			data, err := base64.StdEncoding.DecodeString(string(value))
			if err != nil || !validPictureBlock(data) {
				return false
			}
		}
	}
	return true
}

func skipLE32String(b []byte) ([]byte, bool) {
	if len(b) < 4 {
		return nil, false
	}
	n, ok := lengthWithin(binary.LittleEndian.Uint32(b), len(b)-4)
	if !ok {
		return nil, false
	}
	return b[4+n:], true
}

// lengthWithin returns a declared length as an int when that many bytes fit
// in avail.
func lengthWithin(n uint32, avail int) (int, bool) {
	if avail < 0 || int64(n) > int64(avail) {
		return 0, false
	}
	return int(n), true //nolint:gosec // G115: n is at most avail, an int
}

// validPictureBlock reports whether a FLAC picture block's lengths all fit
// inside it.
func validPictureBlock(b []byte) bool {
	// picture type, then the MIME type and description strings
	if len(b) < 4 {
		return false
	}
	rest := b[4:]
	for range 2 {
		if len(rest) < 4 {
			return false
		}
		n, ok := lengthWithin(binary.BigEndian.Uint32(rest), len(rest)-4)
		if !ok {
			return false
		}
		rest = rest[4+n:]
	}
	// width, height, color depth, colors used, then the data length
	if len(rest) < 20 {
		return false
	}
	_, ok := lengthWithin(binary.BigEndian.Uint32(rest[16:20]), len(rest)-20)
	return ok
}

// Ogg packet prefixes that hold Vorbis comments.
var (
	vorbisCommentPrefix = []byte("\x03vorbis")
	opusTagsPrefix      = []byte("OpusTags")
)

// validOggComments demuxes the Ogg pages in buf the way the tag parser does
// and reports whether every complete comment packet among them fits
// together. Pages that run off the end of buf are left for the parser to
// fail on.
func validOggComments(buf []byte) bool {
	const headerSize = oggHeaderSize
	pending := map[uint32][]byte{}
	for offset := 0; offset+headerSize <= len(buf); {
		page := buf[offset:]
		if string(page[0:4]) != "OggS" {
			return true
		}
		flags := page[5]
		serial := binary.LittleEndian.Uint32(page[14:18])
		segments := int(page[26])
		if headerSize+segments > len(page) {
			return true
		}
		table := page[headerSize : headerSize+segments]
		dataLen := 0
		for _, s := range table {
			dataLen += int(s)
		}
		dataStart := headerSize + segments
		if dataStart+dataLen > len(page) {
			return true
		}
		data := page[dataStart : dataStart+dataLen]

		var packet []byte
		if flags&0x1 != 0 {
			prev, ok := pending[serial]
			if !ok {
				return true
			}
			packet = prev
		}
		p := 0
		for _, s := range table {
			packet = append(packet, data[p:p+int(s)]...)
			p += int(s)
			if s < 255 {
				if !validOggPacket(packet) {
					return false
				}
				packet = nil
			}
		}
		pending[serial] = packet
		offset += dataStart + dataLen
	}
	return true
}

func validOggPacket(packet []byte) bool {
	switch {
	case bytes.HasPrefix(packet, vorbisCommentPrefix):
		return validVorbisComment(packet[len(vorbisCommentPrefix):])
	case bytes.HasPrefix(packet, opusTagsPrefix):
		return validVorbisComment(packet[len(opusTagsPrefix):])
	}
	return true
}

func fillMeta(meta *Meta, md tag.Metadata) {
	meta.Title = sanitize(md.Title())
	meta.Artist = sanitize(md.Artist())
	meta.Album = sanitize(md.Album())
	meta.AlbumArtist = sanitize(md.AlbumArtist())
	meta.Genre = sanitize(md.Genre())
	meta.Year = positive(md.Year())
	track, trackTotal := md.Track()
	meta.TrackNumber, meta.TrackTotal = positive(track), positive(trackTotal)
	disc, discTotal := md.Disc()
	meta.DiscNumber, meta.DiscTotal = positive(disc), positive(discTotal)
	meta.ReplayGainDB, meta.HasReplayGain = replayGain(md.Raw())
}

// maxReplayGainDB bounds a ReplayGain tag; anything further out is not a
// real adjustment.
const maxReplayGainDB = 60

// replayGain finds the track gain in raw tags: a Vorbis comment
// (replaygain_track_gain) or an ID3v2 TXXX frame described
// REPLAYGAIN_TRACK_GAIN. The value reads like "-6.20 dB". A file with
// several takes the first valid one in key order, so the answer does not
// change between reads.
func replayGain(raw map[string]any) (float64, bool) {
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		var text string
		v := raw[key]
		switch val := v.(type) {
		case string:
			if !strings.EqualFold(key, "replaygain_track_gain") {
				continue
			}
			text = val
		case *tag.Comm:
			if !strings.EqualFold(val.Description, "replaygain_track_gain") {
				continue
			}
			text = val.Text
		default:
			continue
		}
		if gain, ok := parseGain(text); ok {
			return gain, true
		}
	}
	return 0, false
}

func parseGain(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if len(text) > 2 && strings.EqualFold(text[len(text)-2:], "db") {
		text = strings.TrimSpace(text[:len(text)-2])
	}
	gain, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(gain) || math.Abs(gain) > maxReplayGainDB {
		return 0, false
	}
	return gain, true
}

func positive(n int) int {
	return max(n, 0)
}

// repairDoubleEncoding undoes UTF-8 text that a file's tags label as
// Latin-1, which the tag reader then decodes byte by byte: "Mêlée" is
// stored as the UTF-8 bytes of "Mêlée" under an ISO-8859-1 flag and reads
// back as "MÃªlÃ©e". It repairs a value only when every character is in
// Latin-1 range, at least one is not ASCII, and its bytes read back as
// valid UTF-8, which Latin-1 text with accents almost never is: "Café"
// is the bytes 43 61 66 E9, and a lone E9 is not UTF-8, so it is left alone.
func repairDoubleEncoding(s string) string {
	ascii := true
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return s
		}
		if r > 0x7F {
			ascii = false
		}
		raw = append(raw, byte(r&0xFF)) //nolint:gosec // r is at most 0xFF here
	}
	if ascii || !utf8.Valid(raw) {
		return s
	}
	return string(raw)
}

// sanitize makes a tag value safe to show: valid UTF-8 (repairing text
// that was double encoded), no control characters, trimmed, and at most
// maxFieldBytes long without splitting a character.
func sanitize(s string) string {
	s = repairDoubleEncoding(strings.ToValidUTF8(s, ""))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) <= maxFieldBytes {
		return s
	}
	cut := maxFieldBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut])
}

// findFolderArt looks for a cover, folder or front image in the directory of
// the track at path.
// HasFolderArt reports whether a cover, folder or front image sits beside
// the track at path.
func HasFolderArt(path string) bool {
	_, ok := findFolderArt(path)
	return ok
}

func findFolderArt(path string) (string, bool) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	best := ""
	bestRank := len(folderArtNames) * len(folderArtExts)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		for ni, want := range folderArtNames {
			if base != want {
				continue
			}
			for ei, wantExt := range folderArtExts {
				if ext != wantExt {
					continue
				}
				rank := ni*len(folderArtExts) + ei
				if rank >= bestRank {
					continue
				}
				// A dangling symlink or a non-regular file must not hide a
				// lower-ranked image that can be read.
				info, statErr := os.Stat(filepath.Join(dir, entry.Name()))
				if statErr != nil || !info.Mode().IsRegular() {
					continue
				}
				best, bestRank = entry.Name(), rank
			}
		}
	}
	if best == "" {
		return "", false
	}
	return filepath.Join(dir, best), true
}

func readFileLimited(path string, maxBytes int64) ([]byte, error) {
	//nolint:gosec // G304: the path is an image beside the playing track.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cover art: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat cover art: %w", err)
	}
	if info.Size() > maxBytes {
		return nil, ErrTagsTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read cover art: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTagsTooLarge
	}
	return data, nil
}
