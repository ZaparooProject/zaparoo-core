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
	"net/url"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
)

// maxFuzzInput caps fuzz inputs; every parser here bounds its own work by
// the input size.
const maxFuzzInput = 64 << 10

var fuzzBase, _ = url.Parse("http://example.com/live/index.m3u8")

func FuzzM3U8(f *testing.F) {
	f.Add([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:7\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"k\",IV=0x01\n#EXT-X-MAP:URI=\"i.mp4\",BYTERANGE=\"10@0\"\n" +
		"#EXT-X-BYTERANGE:100@5\n#EXTINF:6.0,\na.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:6,\nb.ts\n#EXT-X-ENDLIST\n"))
	f.Add([]byte("#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",DEFAULT=YES,URI=\"a.m3u8\"\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS=\"mp4a.40.2\",AUDIO=\"a\"\nlow.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=900000,CODECS=\"avc1.4d401f,mp4a.40.2\"\nv.m3u8\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxFuzzInput {
			return
		}
		p, err := parseM3U8(b, fuzzBase)
		if err != nil {
			return
		}
		if len(p.segments) > maxSegments || len(p.variants) > maxVariants || len(p.renditions) > maxVariants {
			t.Fatalf("limits exceeded: %d segments, %d variants", len(p.segments), len(p.variants))
		}
		for _, s := range p.segments {
			if s.key != nil && s.key.iv != nil && len(s.key.iv) != 16 {
				t.Fatalf("iv length %d", len(s.key.iv))
			}
			if s.rng != nil && (s.rng.offset < 0 || s.rng.length <= 0) {
				t.Fatalf("bad range %+v", *s.rng)
			}
		}
		if p.isMaster() {
			_, _ = pickVariant(p, false)
			_, _ = pickVariant(p, true)
		}
	})
}

func FuzzPLS(f *testing.F) {
	f.Add([]byte("[playlist]\nFile1=http://a/stream\nTitle1=One\nFile2=b\nNumberOfEntries=2\n"))
	f.Add([]byte("#EXTM3U\n#EXTINF:-1,Station\nhttp://a/b\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxFuzzInput {
			return
		}
		for _, entries := range [][]playlistEntry{parsePLS(b, fuzzBase), parseM3U(b, fuzzBase)} {
			if len(entries) > maxPlaylistEntries {
				t.Fatalf("%d entries", len(entries))
			}
			for _, e := range entries {
				u, err := url.Parse(e.url)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					t.Fatalf("bad entry url %q", e.url)
				}
				if len(e.title) > maxTextLen {
					t.Fatal("title too long")
				}
			}
		}
		_ = isPLS(b)
		_ = isHLS(b)
		_ = isM3U(b)
	})
}

func FuzzICYMeta(f *testing.F) {
	f.Add([]byte("abcd\x02StreamTitle='A - B';StreamUrl='';\x00\x00\x00\x00\x00\x00efgh\x00"), 4)
	f.Add([]byte("\x01StreamTitle='x\xe9';"), 0)
	f.Fuzz(func(t *testing.T, b []byte, metaint int) {
		if len(b) > maxFuzzInput {
			return
		}
		metaint = 1 + abs(metaint)%64
		var titles int
		r := newICYReader(bytes.NewReader(b), metaint, func(s string) {
			titles++
			if len(s) > maxTextLen {
				t.Fatal("title too long")
			}
			_, _ = splitTitle(s)
		})
		buf := make([]byte, 37)
		for range len(b) + 2 {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		if s, ok := parseStreamTitle(b); ok && len(s) > maxTextLen {
			t.Fatal("title too long")
		}
	})
}

func abs(n int) int {
	if n < 0 {
		if n == -n {
			return 0
		}
		return -n
	}
	return n
}

func FuzzTSDemux(f *testing.F) {
	frames := adtsSplit(f, fixture(f, "tone.aac"))[:4]
	f.Add(newTSBuilder().segment(frames, 0x0F))
	f.Add(newTSBuilder().segment(frames, 0x03))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxFuzzInput {
			return
		}
		var total int
		d := newTSDemuxer(func(_ nativedec.Kind, es []byte) error {
			total += len(es)
			return nil
		})
		_ = looksLikeTS(b)
		// Split the input so packets straddle writes.
		mid := len(b) / 3
		_ = d.write(b[:mid])
		_ = d.write(b[mid:])
		if total > len(b) {
			t.Fatalf("emitted %d bytes from %d", total, len(b))
		}
		d.reset()
	})
}

func FuzzFMP4(f *testing.F) {
	asc := []byte{0x12, 0x10}
	frames := adtsSplit(f, fixture(f, "tone.aac"))[:4]
	f.Add(fmp4Init(ascFromADTS(frames[0])), fmp4Media(frames))
	f.Fuzz(func(t *testing.T, init, media []byte) {
		if len(init)+len(media) > maxFuzzInput {
			return
		}
		_ = boxes(init)
		tr, err := parseInit(init)
		if err != nil {
			tr = &mp4Track{}
			if tr2, ok := parseASC(asc, 2); ok {
				tr = tr2
			}
		}
		var out int
		_, _ = fmp4Samples(media, tr, func(b []byte) error {
			out += len(b)
			if out > 2*maxFuzzInput+7*len(media) {
				t.Fatal("emitted too much")
			}
			return nil
		})
		_ = tr.adtsHeader(len(media))
	})
}
