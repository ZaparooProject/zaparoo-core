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
	"encoding/hex"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HLS limits.
const (
	// maxSegments bounds the segments kept from one media playlist.
	maxSegments = 4096
	// maxVariants bounds the variants and renditions kept from a master.
	maxVariants = 256
	// maxTargetDuration bounds EXT-X-TARGETDURATION, which paces reloads.
	maxTargetDuration = 60 * time.Second
	// hlsMaxBandwidth is the most a variant may need unless nothing lower
	// exists.
	hlsMaxBandwidth = 192000
)

var errNotHLS = errors.New("radio: not an HLS playlist")

// hlsKey is an EXT-X-KEY.
type hlsKey struct {
	method string // NONE, AES-128, SAMPLE-AES
	uri    string
	iv     []byte // 16 bytes, or nil to use the media sequence number
}

// hlsByteRange is an EXT-X-BYTERANGE or BYTERANGE attribute.
type hlsByteRange struct {
	length, offset int64
}

// hlsMap is an EXT-X-MAP: an fMP4 init segment.
type hlsMap struct {
	rng *hlsByteRange
	uri string
}

type hlsSegment struct {
	key           *hlsKey
	initMap       *hlsMap
	rng           *hlsByteRange
	uri           string
	duration      float64
	seq           uint64
	discontinuity bool
}

type hlsVariant struct {
	uri       string
	audio     string
	codecs    []string
	bandwidth int64
}

type hlsRendition struct {
	groupID   string
	uri       string
	isDefault bool
}

// hlsPlaylist is a parsed m3u8: a master playlist has variants, a media
// playlist segments.
type hlsPlaylist struct {
	variants       []hlsVariant
	renditions     []hlsRendition
	segments       []hlsSegment
	targetDuration time.Duration
	mediaSequence  uint64
	endList        bool
}

func (p *hlsPlaylist) isMaster() bool {
	return len(p.variants) > 0
}

// parseM3U8 parses an HLS playlist. URIs are resolved against base and
// only http and https ones are kept.
func parseM3U8(body []byte, base *url.URL) (*hlsPlaylist, error) {
	ls := lines(body)
	if len(ls) == 0 || !strings.HasPrefix(ls[0], "#EXTM3U") {
		return nil, errNotHLS
	}
	p := &hlsPlaylist{}
	var (
		key      *hlsKey
		initMap  *hlsMap
		duration float64
		rng      *hlsByteRange
		disc     bool
		pending  *hlsVariant
		seqSet   bool
		lastEnd  = map[string]int64{}
		segIndex uint64
	)
	for _, line := range ls[1:] {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			if pending != nil {
				if u, ok := resolveRef(base, line); ok && len(p.variants) < maxVariants {
					pending.uri = u
					p.variants = append(p.variants, *pending)
				}
				pending = nil
				continue
			}
			u, ok := resolveRef(base, line)
			seq := p.mediaSequence + segIndex
			segIndex++
			if ok {
				seg := hlsSegment{uri: u, duration: duration, seq: seq, key: key, initMap: initMap, discontinuity: disc}
				if rng != nil {
					r := *rng
					if r.offset < 0 {
						r.offset = lastEnd[u]
					}
					lastEnd[u] = r.offset + r.length
					seg.rng = &r
				}
				p.segments = append(p.segments, seg)
				if len(p.segments) > maxSegments {
					p.segments = p.segments[1:]
				}
			}
			duration, rng, disc = 0, nil, false
			continue
		}
		tag, value, _ := strings.Cut(line, ":")
		switch tag {
		case "#EXT-X-STREAM-INF":
			a := parseAttrs(value)
			v := hlsVariant{audio: a["AUDIO"]}
			v.bandwidth, _ = strconv.ParseInt(a["BANDWIDTH"], 10, 64)
			if c := a["CODECS"]; c != "" {
				for _, codec := range strings.Split(c, ",") {
					if codec = strings.ToLower(strings.TrimSpace(codec)); codec != "" {
						v.codecs = append(v.codecs, codec)
					}
				}
			}
			pending = &v
		case "#EXT-X-MEDIA":
			a := parseAttrs(value)
			if a["TYPE"] != "AUDIO" || a["URI"] == "" || len(p.renditions) >= maxVariants {
				continue
			}
			if u, ok := resolveRef(base, a["URI"]); ok {
				p.renditions = append(p.renditions, hlsRendition{
					groupID:   a["GROUP-ID"],
					uri:       u,
					isDefault: a["DEFAULT"] == "YES",
				})
			}
		case "#EXT-X-TARGETDURATION":
			if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && f > 0 && !math.IsInf(f, 0) {
				p.targetDuration = min(time.Duration(f*float64(time.Second)), maxTargetDuration)
			}
		case "#EXT-X-MEDIA-SEQUENCE":
			if n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err == nil && !seqSet && segIndex == 0 {
				p.mediaSequence = n
				seqSet = true
			}
		case "#EXTINF":
			d, _, _ := strings.Cut(value, ",")
			if f, err := strconv.ParseFloat(strings.TrimSpace(d), 64); err == nil && f >= 0 && !math.IsInf(f, 0) {
				duration = f
			}
		case "#EXT-X-BYTERANGE":
			rng = parseByteRange(value)
		case "#EXT-X-KEY":
			key = parseKey(value, base)
		case "#EXT-X-MAP":
			a := parseAttrs(value)
			initMap = nil
			if u, ok := resolveRef(base, a["URI"]); ok {
				initMap = &hlsMap{uri: u}
				if r := parseByteRange(a["BYTERANGE"]); r != nil {
					if r.offset < 0 {
						r.offset = 0
					}
					initMap.rng = r
				}
			}
		case "#EXT-X-DISCONTINUITY":
			disc = true
		case "#EXT-X-ENDLIST":
			p.endList = true
		}
	}
	if p.targetDuration == 0 {
		p.targetDuration = 10 * time.Second
	}
	return p, nil
}

// parseKey reads EXT-X-KEY attributes; a key it cannot use keeps its
// method so the segment is refused rather than played as noise.
func parseKey(value string, base *url.URL) *hlsKey {
	a := parseAttrs(value)
	k := &hlsKey{method: a["METHOD"]}
	if k.method == "NONE" || k.method == "" {
		return nil
	}
	if u, ok := resolveRef(base, a["URI"]); ok {
		k.uri = u
	}
	if iv := a["IV"]; iv != "" {
		iv = strings.TrimPrefix(strings.TrimPrefix(iv, "0x"), "0X")
		if b, err := hex.DecodeString(iv); err == nil && len(b) <= 16 {
			k.iv = make([]byte, 16)
			copy(k.iv[16-len(b):], b)
		} else {
			k.method = "INVALID"
		}
	}
	return k
}

// parseByteRange reads "<length>[@<offset>]"; offset -1 means it follows
// the previous range of the same resource.
func parseByteRange(v string) *hlsByteRange {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	l, o, hasOffset := strings.Cut(v, "@")
	length, err := strconv.ParseInt(l, 10, 64)
	if err != nil || length <= 0 || length > maxSegmentBytes {
		return nil
	}
	r := &hlsByteRange{length: length, offset: -1}
	if hasOffset {
		off, err := strconv.ParseInt(o, 10, 64)
		if err != nil || off < 0 {
			return nil
		}
		r.offset = off
	}
	return r
}

// parseAttrs reads an HLS attribute list: KEY=value or KEY="quoted, value"
// separated by commas.
func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for s != "" && len(out) < 64 {
		s = strings.TrimLeft(s, " ,")
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(s[:eq])
		s = s[eq+1:]
		var val string
		if strings.HasPrefix(s, "\"") {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				val, s = s[1:], ""
			} else {
				val, s = s[1:1+end], s[2+end:]
			}
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				val, s = s, ""
			} else {
				val, s = s[:end], s[end:]
			}
		}
		out[key] = strings.TrimSpace(val)
	}
	return out
}

// audioCodec reports whether an RFC 6381 codec string is audio this
// package decodes.
func audioCodec(c string) bool {
	switch c {
	case "mp4a.40.2", "mp4a.40.5", "mp4a.40.29", "mp4a.40.34", "mp4a.69", "mp4a.6b", "mp3":
		return true
	default:
		return false
	}
}

// isAudioCodecs reports whether every codec is decodable audio.
func isAudioCodecs(codecs []string) bool {
	if len(codecs) == 0 {
		return false
	}
	for _, c := range codecs {
		if !audioCodec(c) {
			return false
		}
	}
	return true
}

func hasAudioCodec(codecs []string) bool {
	for _, c := range codecs {
		if audioCodec(c) {
			return true
		}
	}
	return false
}

// pickVariant chooses the media playlist to play from a master: an
// audio-only variant if there is one, else an audio rendition, else the
// cheapest variant that carries audio. Constrained devices take the lowest
// bandwidth; others the highest at or below hlsMaxBandwidth.
func pickVariant(p *hlsPlaylist, constrained bool) (string, bool) {
	var audioOnly []hlsVariant
	for _, v := range p.variants {
		if isAudioCodecs(v.codecs) {
			audioOnly = append(audioOnly, v)
		}
	}
	if len(audioOnly) > 0 {
		return chooseBandwidth(audioOnly, constrained).uri, true
	}
	// An audio group rendition is audio only.
	var group string
	if len(p.variants) > 0 {
		group = chooseBandwidth(p.variants, true).audio
	}
	var fallback string
	for _, r := range p.renditions {
		if group != "" && r.groupID != group {
			continue
		}
		if r.isDefault {
			return r.uri, true
		}
		if fallback == "" {
			fallback = r.uri
		}
	}
	if fallback != "" {
		return fallback, true
	}
	var muxed []hlsVariant
	for _, v := range p.variants {
		if len(v.codecs) == 0 || hasAudioCodec(v.codecs) {
			muxed = append(muxed, v)
		}
	}
	if len(muxed) == 0 {
		return "", false
	}
	return chooseBandwidth(muxed, true).uri, true
}

func chooseBandwidth(vs []hlsVariant, constrained bool) hlsVariant {
	lowest := vs[0]
	for _, v := range vs[1:] {
		if v.bandwidth < lowest.bandwidth {
			lowest = v
		}
	}
	if constrained {
		return lowest
	}
	best := hlsVariant{bandwidth: -1}
	for _, v := range vs {
		if v.bandwidth <= hlsMaxBandwidth && v.bandwidth > best.bandwidth {
			best = v
		}
	}
	if best.bandwidth < 0 {
		return lowest
	}
	return best
}
