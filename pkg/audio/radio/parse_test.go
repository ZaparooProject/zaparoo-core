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

package radio

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func TestParseStreamTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, meta, want string
		ok               bool
	}{
		{name: "plain", meta: "StreamTitle='Artist - Song';", want: "Artist - Song", ok: true},
		{name: "with url", meta: "StreamTitle='A - B';StreamUrl='http://x';", want: "A - B", ok: true},
		{name: "quote inside", meta: "StreamTitle='Rock 'n' Roll';StreamUrl='';", want: "Rock 'n' Roll", ok: true},
		{name: "padded", meta: "StreamTitle='X';\x00\x00\x00", want: "X", ok: true},
		{name: "no title", meta: "StreamUrl='x';", ok: false},
		{name: "latin1", meta: "StreamTitle='Caf\xe9';", want: "Café", ok: true},
		{name: "control chars", meta: "StreamTitle='a\x01b\nc';", want: "abc", ok: true},
		{name: "unterminated", meta: "StreamTitle='open", want: "open", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseStreamTitle([]byte(tt.meta))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeTextCapsLength(t *testing.T) {
	t.Parallel()
	got := sanitizeText(strings.Repeat("é", 300))
	assert.LessOrEqual(t, len(got), maxTextLen)
	assert.True(t, strings.HasPrefix(got, "éé"))
}

func TestSplitTitle(t *testing.T) {
	t.Parallel()
	title, artist := splitTitle("The Band - A Song - Live")
	assert.Equal(t, "A Song - Live", title)
	assert.Equal(t, "The Band", artist)
	title, artist = splitTitle("Just a title")
	assert.Equal(t, "Just a title", title)
	assert.Empty(t, artist)
}

// icyStream interleaves metadata blocks every metaint bytes of audio.
func icyStream(audio []byte, metaint int, metas []string) []byte {
	var out bytes.Buffer
	for i := 0; len(audio) > 0; i++ {
		n := min(metaint, len(audio))
		out.Write(audio[:n])
		audio = audio[n:]
		if n < metaint {
			break
		}
		meta := ""
		if i < len(metas) {
			meta = metas[i]
		}
		out.Write(icyBlock(meta))
	}
	return out.Bytes()
}

func icyBlock(meta string) []byte {
	if meta == "" {
		return []byte{0}
	}
	n := (len(meta) + 15) / 16
	b := make([]byte, 1+n*16)
	b[0] = u8(n)
	copy(b[1:], meta)
	return b
}

func TestICYReaderStripsMetadata(t *testing.T) {
	t.Parallel()
	audio := bytes.Repeat([]byte("0123456789"), 100)
	data := icyStream(audio, 64, []string{"StreamTitle='A - 1';", "", "StreamTitle='A - 1';", "StreamTitle='B - 2';"})
	var titles []string
	r := newICYReader(bytes.NewReader(data), 64, func(s string) { titles = append(titles, s) })
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, audio, got)
	assert.Equal(t, []string{"A - 1", "B - 2"}, titles)
}

func TestICYReaderTruncatedMetadata(t *testing.T) {
	t.Parallel()
	data := append(bytes.Repeat([]byte{1}, 16), 5, 'S', 't')
	r := newICYReader(bytes.NewReader(data), 16, nil)
	_, err := io.ReadAll(r)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestParseMetaint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 16000, parseMetaint(" 16000 "))
	assert.Zero(t, parseMetaint(""))
	assert.Zero(t, parseMetaint("-1"))
	assert.Zero(t, parseMetaint("abc"))
	assert.Zero(t, parseMetaint("99999999"))
}

func TestParsePLS(t *testing.T) {
	t.Parallel()
	body := "[playlist]\nNumberOfEntries=3\nFile2=http://b/stream\nTitle2=Second\n" +
		"File1=relative.mp3\nTitle1=First\nFile3=ftp://nope\nFile10=https://c/x\nVersion=2\n"
	got := parsePLS([]byte(body), mustURL(t, "http://host/dir/list.pls"))
	assert.Equal(t, []playlistEntry{
		{url: "http://host/dir/relative.mp3", title: "First"},
		{url: "http://b/stream", title: "Second"},
		{url: "https://c/x"},
	}, got)
	assert.True(t, isPLS([]byte("\xef\xbb\xbf [Playlist]\n")))
}

func TestParseM3U(t *testing.T) {
	t.Parallel()
	body := "#EXTM3U\r\n#EXTINF:-1,Station One\r\nhttp://a/1\r\n\r\n# comment\r\nfile:///etc/passwd\r\nhttp://a/2\r\n"
	got := parseM3U([]byte(body), mustURL(t, "http://host/x.m3u"))
	assert.Equal(t, []playlistEntry{{url: "http://a/1", title: "Station One"}, {url: "http://a/2"}}, got)
	assert.True(t, isM3U([]byte("http://a/1\n")))
	assert.False(t, isM3U([]byte("<html>")))
	assert.False(t, isHLS([]byte(body)))
	assert.True(t, isHLS([]byte("#EXTM3U\n#EXT-X-VERSION:3\n")))
}

func TestParsePlaylistEntryCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for range maxPlaylistEntries * 3 {
		sb.WriteString("http://a/x\n")
	}
	assert.Len(t, parseM3U([]byte(sb.String()), nil), maxPlaylistEntries)
}

func TestParseM3U8Media(t *testing.T) {
	t.Parallel()
	body := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:100
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x0102
#EXT-X-MAP:URI="init.mp4",BYTERANGE="500@0"
#EXTINF:5.5,
seg100.m4s
#EXT-X-DISCONTINUITY
#EXT-X-BYTERANGE:1000@200
#EXTINF:6,
all.m4s
#EXT-X-KEY:METHOD=NONE
#EXT-X-BYTERANGE:300
#EXTINF:6,
all.m4s
#EXT-X-ENDLIST
`
	p, err := parseM3U8([]byte(body), mustURL(t, "https://cdn/a/media.m3u8"))
	require.NoError(t, err)
	assert.False(t, p.isMaster())
	assert.True(t, p.endList)
	assert.Equal(t, 6*time.Second, p.targetDuration)
	require.Len(t, p.segments, 3)
	s0, s1, s2 := p.segments[0], p.segments[1], p.segments[2]
	assert.Equal(t, "https://cdn/a/seg100.m4s", s0.uri)
	assert.Equal(t, uint64(100), s0.seq)
	assert.InDelta(t, 5.5, s0.duration, 0.001)
	require.NotNil(t, s0.key)
	assert.Equal(t, "AES-128", s0.key.method)
	assert.Equal(t, "https://cdn/a/key.bin", s0.key.uri)
	assert.Equal(t, append(make([]byte, 14), 1, 2), s0.key.iv)
	require.NotNil(t, s0.initMap)
	assert.Equal(t, &hlsByteRange{length: 500, offset: 0}, s0.initMap.rng)
	assert.True(t, s1.discontinuity)
	assert.Equal(t, &hlsByteRange{length: 1000, offset: 200}, s1.rng)
	assert.Nil(t, s2.key)
	assert.Equal(t, &hlsByteRange{length: 300, offset: 1200}, s2.rng)
	assert.Equal(t, uint64(102), s2.seq)
}

func TestParseM3U8Master(t *testing.T) {
	t.Parallel()
	body := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="en",DEFAULT=YES,URI="audio/en.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2000000,CODECS="avc1.64001f,mp4a.40.2",AUDIO="aud"
video/hi.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS="mp4a.40.5"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=128000,CODECS="mp4a.40.2"
mid.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=320000,CODECS="mp4a.40.2"
high.m3u8
`
	p, err := parseM3U8([]byte(body), mustURL(t, "http://h/master.m3u8"))
	require.NoError(t, err)
	require.True(t, p.isMaster())
	assert.Len(t, p.variants, 4)
	u, ok := pickVariant(p, false)
	require.True(t, ok)
	assert.Equal(t, "http://h/mid.m3u8", u)
	u, ok = pickVariant(p, true)
	require.True(t, ok)
	assert.Equal(t, "http://h/low.m3u8", u)

	// Without audio-only variants, the audio rendition is chosen.
	p.variants = p.variants[:1]
	u, ok = pickVariant(p, false)
	require.True(t, ok)
	assert.Equal(t, "http://h/audio/en.m3u8", u)

	// A video-only master has nothing to play.
	p.renditions = nil
	p.variants[0].codecs = []string{"avc1.64001f"}
	_, ok = pickVariant(p, false)
	assert.False(t, ok)
}

func TestChooseBandwidthAllAboveCap(t *testing.T) {
	t.Parallel()
	vs := []hlsVariant{{uri: "a", bandwidth: 500000}, {uri: "b", bandwidth: 300000}}
	assert.Equal(t, "b", chooseBandwidth(vs, false).uri)
}

func TestParseM3U8Rejects(t *testing.T) {
	t.Parallel()
	_, err := parseM3U8([]byte("<html>"), nil)
	require.ErrorIs(t, err, errNotHLS)
	assert.Nil(t, parseByteRange("99999999999"))
	assert.Nil(t, parseByteRange("-5"))
	assert.Nil(t, parseByteRange("10@-1"))
	k := parseKey(`METHOD=AES-128,URI="k",IV=0xZZ`, mustURL(t, "http://h/"))
	assert.Equal(t, "INVALID", k.method)
	assert.Nil(t, parseKey("METHOD=NONE", nil))
}

func TestParseM3U8SegmentCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:2\n")
	for range maxSegments + 10 {
		sb.WriteString("#EXTINF:2,\nhttp://h/s.ts\n")
	}
	p, err := parseM3U8([]byte(sb.String()), nil)
	require.NoError(t, err)
	assert.Len(t, p.segments, maxSegments)
	assert.Equal(t, uint64(maxSegments+9), p.segments[len(p.segments)-1].seq)
}

func TestParseAttrs(t *testing.T) {
	t.Parallel()
	got := parseAttrs(`BANDWIDTH=1000,CODECS="mp4a.40.2,avc1",NAME="a b", X=y`)
	assert.Equal(t, map[string]string{"BANDWIDTH": "1000", "CODECS": "mp4a.40.2,avc1", "NAME": "a b", "X": "y"}, got)
}

func TestSniffCodec(t *testing.T) {
	t.Parallel()
	assert.Equal(t, nativedec.Vorbis, sniffCodec(fixture(t, "tone.ogg")[:512]))
	assert.Equal(t, nativedec.AAC, sniffCodec(fixture(t, "tone.aac")[:512]))
	mp3 := fixture(t, "tone.mp3")
	assert.Equal(t, nativedec.MP3, sniffCodec(mp3[:512]))
	// Mid-stream, past any ID3 tag.
	assert.Equal(t, nativedec.MP3, sniffCodec(mp3[len(mp3)/2:len(mp3)/2+512]))
	assert.Equal(t, codecNone, sniffCodec([]byte("<!doctype html><html>")))
	assert.Equal(t, codecNone, sniffCodec(bytes.Repeat([]byte{0xFF}, 64)))
}

func TestLooksLikeText(t *testing.T) {
	t.Parallel()
	assert.True(t, looksLikeText([]byte("\r\n#EXTM3U\n")))
	assert.True(t, looksLikeText([]byte("[playlist]\nFile1=x")))
	assert.True(t, looksLikeText([]byte("HTTP://a/b\n")))
	assert.False(t, looksLikeText([]byte("<html>")))
}

func TestReadLimited(t *testing.T) {
	t.Parallel()
	b, err := readLimited(strings.NewReader("12345"), 5)
	require.NoError(t, err)
	assert.Equal(t, "12345", string(b))
	_, err = readLimited(strings.NewReader("123456"), 5)
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestTSDemux(t *testing.T) {
	t.Parallel()
	frames := adtsSplit(t, fixture(t, "tone.aac"))
	b := newTSBuilder()
	seg1 := b.segment(frames[:10], 0x0F)
	seg2 := b.segment(frames[10:20], 0x0F)
	var got []byte
	var codecs []nativedec.Kind
	d := newTSDemuxer(func(c nativedec.Kind, es []byte) error {
		codecs = append(codecs, c)
		got = append(got, es...)
		return nil
	})
	require.NoError(t, d.write(seg1))
	require.NoError(t, d.write(seg2))
	assert.Equal(t, bytes.Join(frames[:20], nil), got)
	for _, c := range codecs {
		assert.Equal(t, nativedec.AAC, c)
	}

	// A lost packet drops the rest of that PES, not the stream.
	got = nil
	seg3 := b.segment(frames[20:30], 0x0F)
	seg4 := b.segment(frames[30:32], 0x0F)
	broken := append(append([]byte{}, seg3[:3*tsPacketSize]...), seg3[4*tsPacketSize:]...)
	require.NoError(t, d.write(append(broken, seg4...)))
	assert.Equal(t, bytes.Join(frames[30:32], nil), got[len(got)-len(bytes.Join(frames[30:32], nil)):])
	assert.Less(t, len(got), len(bytes.Join(frames[20:32], nil)))

	// Errors from the consumer stop the demuxer.
	stop := errors.New("stop")
	d2 := newTSDemuxer(func(nativedec.Kind, []byte) error { return stop })
	require.ErrorIs(t, d2.write(newTSBuilder().segment(frames[:3], 0x0F)), stop)
}

func TestTSDemuxResyncAndMP3(t *testing.T) {
	t.Parallel()
	b := newTSBuilder()
	payload := bytes.Repeat([]byte{0xAB}, 500)
	seg := b.segment([][]byte{payload}, 0x03)
	var got []byte
	var codec nativedec.Kind
	d := newTSDemuxer(func(c nativedec.Kind, es []byte) error {
		codec = c
		got = append(got, es...)
		return nil
	})
	// Leading garbage is skipped to the sync.
	require.NoError(t, d.write(append([]byte{1, 2, 3}, seg...)))
	assert.Equal(t, nativedec.MP3, codec)
	assert.Equal(t, payload, got)
	assert.True(t, looksLikeTS(seg))
	assert.False(t, looksLikeTS([]byte("ID3")))
}

func TestFMP4Samples(t *testing.T) {
	t.Parallel()
	frames := adtsSplit(t, fixture(t, "tone.aac"))
	track, err := parseInit(fmp4Init(ascFromADTS(frames[0])))
	require.NoError(t, err)
	assert.Equal(t, nativedec.AAC, track.codec)
	assert.Equal(t, uint32(1), track.trackID)
	var got [][]byte
	n, err := fmp4Samples(fmp4Media(frames[:8]), track, func(f []byte) error {
		got = append(got, append([]byte{}, f...))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 8, n)
	// The synthesized ADTS headers match the originals, up to the buffer
	// fullness field, which the encoder sets and the decoder ignores.
	for i, f := range got {
		assert.Equal(t, frames[i][:5], f[:5], "frame %d header", i)
		assert.Equal(t, frames[i][7:], f[7:], "frame %d payload", i)
	}
}

func TestParseASC(t *testing.T) {
	t.Parallel()
	// HE-AAC v1 with explicit SBR: AOT 5, 24 kHz core, stereo, 48 kHz ext,
	// core AOT 2.
	asc := []byte{0x2B, 0x12, 0x08, 0x80}
	tr, ok := parseASC(asc, 2)
	require.True(t, ok)
	assert.Equal(t, byte(1), tr.profile)
	assert.Equal(t, byte(6), tr.freqIdx)
	assert.Equal(t, byte(2), tr.chanConf)
	tr, ok = parseASC([]byte{0x11, 0x00}, 1) // AOT 2, 48 kHz, channels from PCE
	require.True(t, ok)
	assert.Equal(t, byte(1), tr.chanConf)
	tr, ok = parseASC([]byte{0x8A, 0x00}, 2) // AOT 17 (ER AAC LC)
	assert.False(t, ok)
	assert.Nil(t, tr)
	_, ok = parseASC(nil, 2)
	assert.False(t, ok)
	tr, ok = parseASC([]byte{0xF8, 0x44, 0x10}, 2) // escape to AOT 32 + 2 = 34: MP3
	require.True(t, ok)
	assert.Equal(t, nativedec.MP3, tr.codec)
}

func TestParseInitRejectsVideo(t *testing.T) {
	t.Parallel()
	init := fmp4Init([]byte{0x12, 0x10})
	init = bytes.Replace(init, []byte("soun"), []byte("vide"), 1)
	_, err := parseInit(init)
	require.ErrorIs(t, err, errNoAudioTrack)
	_, err = parseInit([]byte("garbage"))
	require.ErrorIs(t, err, errNoAudioTrack)
}

func TestSkipID3(t *testing.T) {
	t.Parallel()
	tag := id3Tag("Song", "Band")
	rest, title, artist := skipID3(append(append(tag, id3Tag("Later", "")...), 0xFF, 0xF1))
	assert.Equal(t, []byte{0xFF, 0xF1}, rest)
	assert.Equal(t, "Later", title)
	assert.Equal(t, "Band", artist)
	rest, _, _ = skipID3(tag[:len(tag)-3])
	assert.Nil(t, rest)
	assert.Equal(t, "Hé", id3Text([]byte{1, 0xFF, 0xFE, 'H', 0, 0xE9, 0, 0, 0}))
	assert.Equal(t, "Hi", id3Text([]byte{2, 0, 'H', 0, 'i'}))
	assert.Equal(t, "Café", id3Text([]byte{0, 'C', 'a', 'f', 0xE9, 0, 'x'}))
}

func TestDecryptAES128(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 16)
	iv := bytes.Repeat([]byte{9}, 16)
	plain := []byte("hello, segment")
	enc := encryptAES128(t, plain, key, iv)
	got, err := decryptAES128(append([]byte{}, enc...), key, iv)
	require.NoError(t, err)
	assert.Equal(t, plain, got)
	_, err = decryptAES128(enc[:15], key, iv)
	require.Error(t, err)
	_, err = decryptAES128(append([]byte{}, enc...), bytes.Repeat([]byte{8}, 16), iv)
	require.Error(t, err)
}

func encryptAES128(t testing.TB, plain, key, iv []byte) []byte {
	t.Helper()
	pad := 16 - len(plain)%16
	data := append(append([]byte{}, plain...), bytes.Repeat([]byte{u8(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
	return data
}

func TestResampler(t *testing.T) {
	t.Parallel()
	in := make([][2]float64, 1000)
	for i := range in {
		in[i] = [2]float64{float64(i), -float64(i)}
	}
	var r resampler
	var out [][2]float64
	require.NoError(t, r.process(22050, 44100, in, func(f [][2]float64) error {
		out = append(out, f...)
		return nil
	}))
	assert.InDelta(t, 2000, len(out), 4)
	assert.InDelta(t, 0.5, out[1][0], 1e-9)
	assert.InDelta(t, -0.5, out[1][1], 1e-9)

	out = nil
	r.reset()
	require.NoError(t, r.process(48000, 8000, in, func(f [][2]float64) error {
		out = append(out, f...)
		return nil
	}))
	assert.InDelta(t, 1000/6, len(out), 2)
	assert.InDelta(t, 6, out[1][0], 1e-9)
}
