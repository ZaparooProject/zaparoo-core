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
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ICY metadata limits. The length byte counts 16-byte blocks, so a block
// is at most 255*16 bytes.
const (
	maxICYMeta = 255 * 16
	// maxMetaint bounds the audio bytes between metadata blocks; servers
	// use 8 to 64 KiB.
	maxMetaint = 1 << 20
	// maxTextLen caps a title, artist or station name, in bytes.
	maxTextLen = 256
)

// parseMetaint reads an icy-metaint header: 0 when absent or unusable.
func parseMetaint(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > maxMetaint {
		return 0
	}
	return n
}

// icyReader strips the metadata blocks a server interleaves with the audio
// every metaint bytes, reporting each StreamTitle that changes.
type icyReader struct {
	r       io.Reader
	onTitle func(string)
	last    string
	meta    [maxICYMeta]byte
	metaint int
	left    int
	titled  bool
}

func newICYReader(r io.Reader, metaint int, onTitle func(string)) io.Reader {
	if metaint <= 0 {
		return r
	}
	return &icyReader{r: r, metaint: metaint, left: metaint, onTitle: onTitle}
}

func (ir *icyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if ir.left == 0 {
		if err := ir.readMeta(); err != nil {
			return 0, err
		}
		ir.left = ir.metaint
	}
	n, err := ir.r.Read(p[:min(len(p), ir.left)])
	ir.left -= n
	return n, err //nolint:wrapcheck // passes the body's errors through
}

func (ir *icyReader) readMeta() error {
	var lenByte [1]byte
	if _, err := io.ReadFull(ir.r, lenByte[:]); err != nil {
		return fmt.Errorf("icy metadata length: %w", noEOF(err))
	}
	n := int(lenByte[0]) * 16
	if n == 0 {
		return nil
	}
	if _, err := io.ReadFull(ir.r, ir.meta[:n]); err != nil {
		return fmt.Errorf("icy metadata: %w", noEOF(err))
	}
	title, ok := parseStreamTitle(ir.meta[:n])
	if ok && (!ir.titled || title != ir.last) {
		ir.titled = true
		ir.last = title
		if ir.onTitle != nil {
			ir.onTitle(title)
		}
	}
	return nil
}

// noEOF turns a clean EOF inside a metadata block into an unexpected one:
// the connection dropped mid-block.
func noEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// parseStreamTitle finds StreamTitle='...'; in an ICY metadata block. The
// title may itself hold quotes, so it ends at the last "';" or, failing
// that, the last quote.
func parseStreamTitle(meta []byte) (string, bool) {
	meta = bytes.TrimRight(meta, "\x00")
	const key = "StreamTitle='"
	i := bytes.Index(meta, []byte(key))
	if i < 0 {
		return "", false
	}
	rest := meta[i+len(key):]
	end := bytes.Index(rest, []byte("';"))
	if next := bytes.Index(rest, []byte("';Stream")); next >= 0 {
		end = next
	} else if end < 0 {
		end = bytes.LastIndexByte(rest, '\'')
	}
	if end < 0 {
		end = len(rest)
	}
	return sanitizeText(decodeText(rest[:end])), true
}

// decodeText reads ICY text: UTF-8 when it is valid, else Latin-1, which
// older servers send.
func decodeText(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// sanitizeText makes untrusted text safe to show: valid UTF-8, no control
// characters, trimmed, and at most maxTextLen bytes.
func sanitizeText(s string) string {
	s = strings.ToValidUTF8(s, "")
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || r == utf8.RuneError {
			continue
		}
		if sb.Len()+utf8.RuneLen(r) > maxTextLen {
			break
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// splitTitle splits "Artist - Title" on the first separator; without one
// the whole is the title.
func splitTitle(s string) (title, artist string) {
	if a, t, ok := strings.Cut(s, " - "); ok {
		return strings.TrimSpace(t), strings.TrimSpace(a)
	}
	return s, ""
}
