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

package simpleproto

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	tests := []struct {
		wantRemovable *bool
		name          string
		line          string
		wantUID       string
		wantText      string
		wantToken     bool
	}{
		{name: "empty line", line: ""},
		{name: "whitespace only", line: "   \r\n"},
		{name: "no SCAN prefix", line: "invalid format"},
		{name: "SCAN with no args", line: "SCAN\t"},
		{name: "text only", line: "SCAN\t**launch.system:nes", wantToken: true, wantText: "**launch.system:nes"},
		{name: "uid", line: "SCAN\tuid=abc123", wantToken: true, wantUID: "abc123"},
		{name: "text", line: "SCAN\ttext=hello", wantToken: true, wantText: "hello"},
		{
			name: "uid and text", line: "SCAN\tuid=abc123\ttext=hello world",
			wantToken: true, wantUID: "abc123", wantText: "hello world",
		},
		{
			name: "removable=no", line: "SCAN\tuid=abc123\tremovable=no",
			wantToken: true, wantUID: "abc123", wantRemovable: &no,
		},
		{
			name: "removable=yes", line: "SCAN\tuid=abc123\tremovable=yes",
			wantToken: true, wantUID: "abc123", wantRemovable: &yes,
		},
		{
			name: "all args", line: "SCAN\tuid=xyz789\ttext=test message\tremovable=no",
			wantToken: true, wantUID: "xyz789", wantText: "test message", wantRemovable: &no,
		},
		{name: "trailing carriage return", line: "SCAN\tuid=abc123\r", wantToken: true, wantUID: "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, ok := ParseLine(tt.line, "reader-1")
			if !tt.wantToken {
				assert.False(t, ok)
				assert.Nil(t, parsed.Token)
				return
			}

			require.True(t, ok)
			require.NotNil(t, parsed.Token)
			assert.Equal(t, tt.wantUID, parsed.Token.UID)
			assert.Equal(t, tt.wantText, parsed.Token.Text)
			assert.Equal(t, tokens.SourceReader, parsed.Token.Source)
			assert.Equal(t, "reader-1", parsed.Token.ReaderID)
			assert.Equal(t, strings.TrimSpace(tt.line), parsed.Token.Data)
			assert.False(t, parsed.Token.ScanTime.IsZero())
			assert.Equal(t, tt.wantRemovable, parsed.Removable)
		})
	}
}

func TestLineSplitter(t *testing.T) {
	t.Parallel()

	t.Run("splits complete lines and keeps the remainder", func(t *testing.T) {
		t.Parallel()
		s := NewLineSplitter(0)
		assert.Equal(t, []string{"SCAN\tuid=1"}, s.Feed([]byte("SCAN\tuid=1\nSCAN\tuid=2")))
		assert.Empty(t, s.Feed([]byte("3")))
		assert.Equal(t, []string{"SCAN\tuid=23"}, s.Feed([]byte("\n")))
	})

	t.Run("keeps carriage returns for the parser", func(t *testing.T) {
		t.Parallel()
		s := NewLineSplitter(0)
		assert.Equal(t, []string{"SCAN\tuid=1\r", ""}, s.Feed([]byte("SCAN\tuid=1\r\n\n")))
	})

	t.Run("drops a line over the limit and resumes after the newline", func(t *testing.T) {
		t.Parallel()
		s := NewLineSplitter(8)
		assert.Empty(t, s.Feed([]byte("0123456789abcdef")))
		assert.Equal(t, []string{"ok"}, s.Feed([]byte("still dropped\nok\n")))
	})

	t.Run("bytes are delivered one at a time", func(t *testing.T) {
		t.Parallel()
		s := NewLineSplitter(0)
		stream := []byte("SCAN\ttext=a\nSCAN\ttext=b\n")
		got := make([]string, 0, 2)
		for _, b := range stream {
			got = append(got, s.Feed([]byte{b})...)
		}
		assert.Equal(t, []string{"SCAN\ttext=a", "SCAN\ttext=b"}, got)
	})
}
