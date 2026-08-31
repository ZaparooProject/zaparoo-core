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

package pn532

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// debugLogEntry is the subset of a zerolog JSON record these tests assert on.
type debugLogEntry struct {
	Level   string `json:"level"`
	Src     string `json:"src"`
	Message string `json:"message"`
}

// captureDebugLog redirects the global logger into a buffer for the duration of
// the test. These tests cannot run in parallel because of it.
func captureDebugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	originalLogger := log.Logger
	originalLevel := zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buf)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = originalLogger
		zerolog.SetGlobalLevel(originalLevel)
	})
	return &buf
}

func decodeLogEntries(t *testing.T, buf *bytes.Buffer) []debugLogEntry {
	t.Helper()
	output := strings.TrimSpace(buf.String())
	if output == "" {
		return nil
	}
	lines := strings.Split(output, "\n")
	entries := make([]debugLogEntry, 0, len(lines))
	for _, line := range lines {
		var entry debugLogEntry
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "log line is not JSON: %s", line)
		entries = append(entries, entry)
	}
	return entries
}

func TestZerologDebugWriterWrite(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single terminated line",
			input: "10:11:12.000 DEBUG: user data = [3 0 254]\n",
			want:  []string{"10:11:12.000 DEBUG: user data = [3 0 254]"},
		},
		{
			name:  "multiple lines in one write",
			input: "first\nsecond\nthird\n",
			want:  []string{"first", "second", "third"},
		},
		{
			name:  "blank lines skipped",
			input: "first\n\n\nsecond\n",
			want:  []string{"first", "second"},
		},
		{
			name:  "carriage returns trimmed",
			input: "first\r\nsecond\r\n",
			want:  []string{"first", "second"},
		},
		{
			// go-pn532 always terminates its messages, but an unterminated
			// chunk must still be logged rather than held back.
			name:  "unterminated line still logged",
			input: "no trailing newline",
			want:  []string{"no trailing newline"},
		},
		{
			name:  "only blank lines produces no output",
			input: "\n\r\n\n",
			want:  nil,
		},
		{
			name:  "empty input produces no output",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureDebugLog(t)

			written, err := zerologDebugWriter{}.Write([]byte(tt.input))

			// The io.Writer contract matters here: fmt.Fprintf treats a short
			// write as an error, which would suppress go-pn532's output.
			require.NoError(t, err)
			assert.Equal(t, len(tt.input), written)

			entries := decodeLogEntries(t, buf)
			require.Len(t, entries, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want, entries[i].Message)
				assert.Equal(t, "pn532", entries[i].Src)
				assert.Equal(t, "debug", entries[i].Level)
			}
		})
	}
}

func TestEnablePN532DebugLoggingInstallsWriterOnce(t *testing.T) {
	buf := captureDebugLog(t)

	debugWriterOnce = sync.Once{}
	t.Cleanup(func() {
		pn532.SetDebugWriter(nil)
		debugWriterOnce = sync.Once{}
	})

	enablePN532DebugLogging()
	pn532.Debugf("user data = %v", []byte{0x03, 0x00})

	entries := decodeLogEntries(t, buf)
	require.Len(t, entries, 1, "one go-pn532 debug call should log exactly one line")
	assert.Equal(t, "pn532", entries[0].Src)
	assert.Equal(t, "debug", entries[0].Level)
	assert.Contains(t, entries[0].Message, "user data = [3 0]")

	// A second call must not reinstall the writer. go-pn532 holds a single
	// writer, so overwriting a sentinel is the observable failure.
	var sentinel bytes.Buffer
	pn532.SetDebugWriter(&sentinel)
	enablePN532DebugLogging()
	pn532.Debugln("second message")

	assert.Contains(t, sentinel.String(), "second message",
		"repeat call replaced the installed writer")
}
