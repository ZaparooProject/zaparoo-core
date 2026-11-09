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
	"bufio"
	"bytes"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Playlist limits.
const (
	// maxPlaylistBytes bounds any playlist body, pls, m3u or HLS.
	maxPlaylistBytes = 1 << 20
	// maxPlaylistEntries bounds the stream URLs tried from one playlist.
	maxPlaylistEntries = 64
	// maxPlaylistDepth bounds playlists of playlists.
	maxPlaylistDepth = 3
)

// playlistEntry is one stream URL from a pls or m3u playlist.
type playlistEntry struct {
	url   string
	title string
}

// resolveRef resolves ref against base and keeps it only when it is an
// absolute http or https URL.
func resolveRef(base *url.URL, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	// A reference such as "//::" resolves to "http://::", which net/http
	// rejects; keep only URLs that survive a round trip.
	s := u.String()
	if _, err := url.Parse(s); err != nil {
		return "", false
	}
	return s, true
}

// lines splits a playlist body into trimmed lines, BOM and CRs removed.
func lines(body []byte) []string {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 4096), maxPlaylistBytes)
	for sc.Scan() {
		out = append(out, strings.TrimSpace(sc.Text()))
	}
	return out
}

// parsePLS reads a pls playlist: FileN, TitleN, in N order.
func parsePLS(body []byte, base *url.URL) []playlistEntry {
	type numbered struct {
		file, title string
	}
	byNum := map[int]*numbered{}
	for _, line := range lines(body) {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var field string
		switch {
		case strings.HasPrefix(key, "file"):
			field = "file"
		case strings.HasPrefix(key, "title"):
			field = "title"
		default:
			continue
		}
		n, err := strconv.Atoi(key[len(field):])
		if err != nil || n < 0 {
			continue
		}
		e := byNum[n]
		if e == nil {
			if len(byNum) >= maxPlaylistEntries*4 {
				continue
			}
			e = &numbered{}
			byNum[n] = e
		}
		if field == "file" {
			e.file = strings.TrimSpace(value)
		} else {
			e.title = strings.TrimSpace(value)
		}
	}
	nums := make([]int, 0, len(byNum))
	for n := range byNum {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []playlistEntry
	for _, n := range nums {
		e := byNum[n]
		u, ok := resolveRef(base, e.file)
		if !ok {
			continue
		}
		out = append(out, playlistEntry{url: u, title: sanitizeText(e.title)})
		if len(out) == maxPlaylistEntries {
			break
		}
	}
	return out
}

// parseM3U reads a plain m3u playlist: one URL per line, comments start
// with #, and #EXTINF:<duration>,<title> names the next entry.
func parseM3U(body []byte, base *url.URL) []playlistEntry {
	var out []playlistEntry
	title := ""
	for _, line := range lines(body) {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				if _, t, found := strings.Cut(rest, ","); found {
					title = t
				}
			}
			continue
		}
		u, ok := resolveRef(base, line)
		if ok {
			out = append(out, playlistEntry{url: u, title: sanitizeText(title)})
			if len(out) == maxPlaylistEntries {
				break
			}
		}
		title = ""
	}
	return out
}

// isPLS reports whether body looks like a pls playlist.
func isPLS(body []byte) bool {
	head := bytes.TrimLeft(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")), " \t\r\n")
	return len(head) >= 10 && strings.EqualFold(string(head[:10]), "[playlist]")
}

// isHLS reports whether body is an HLS playlist rather than a plain m3u.
func isHLS(body []byte) bool {
	head := bytes.TrimLeft(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")), " \t\r\n")
	return bytes.HasPrefix(head, []byte("#EXTM3U")) && bytes.Contains(head, []byte("#EXT-X-"))
}

// isM3U reports whether body looks like a plain m3u: the #EXTM3U header,
// or a first non-comment line that is a URL.
func isM3U(body []byte) bool {
	head := bytes.TrimLeft(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")), " \t\r\n")
	if bytes.HasPrefix(head, []byte("#EXTM3U")) {
		return true
	}
	for _, line := range lines(head) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l := strings.ToLower(line)
		return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
	}
	return false
}
