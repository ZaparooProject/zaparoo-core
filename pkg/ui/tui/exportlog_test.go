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

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLogEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "error level",
			input:    `{"level":"error","time":"2025-11-20T13:04:23Z","message":"service failed to start"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:23 service failed to start",
		},
		{
			name:     "warn level",
			input:    `{"level":"warn","time":"2025-11-20T13:04:22Z","message":"config file not found"}`,
			expected: "[yellow::b] WARN[-:-:-] 13:04:22 config file not found",
		},
		{
			name:     "info level",
			input:    `{"level":"info","time":"2025-11-20T13:04:21Z","message":"service starting"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:21 service starting",
		},
		{
			name:     "debug level",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"loading config"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 loading config",
		},
		{
			name:     "invalid JSON",
			input:    "this is not json",
			expected: "this is not json",
		},
		{
			name:     "stack trace line",
			input:    "    at some.function (file.go:123)",
			expected: "    at some.function (file.go:123)",
		},
		{
			name:     "unknown level",
			input:    `{"level":"trace","time":"2025-11-20T13:04:20Z","message":"test"}`,
			expected: "[white::b]TRACE[-:-:-] 13:04:20 test",
		},
		{
			name:     "missing message field",
			input:    `{"level":"info","time":"2025-11-20T13:04:20Z"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:20 ",
		},
		{
			name:     "malformed timestamp",
			input:    `{"level":"info","time":"invalid","message":"test"}`,
			expected: "[green::b] INFO[-:-:-] invalid test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogEntry(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLogContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "multiple lines newest first",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"first"}
{"level":"warn","time":"2025-11-20T13:04:21Z","message":"second"}
{"level":"error","time":"2025-11-20T13:04:22Z","message":"third"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 third\n" +
				"[yellow::b] WARN[-:-:-] 13:04:21 second\n" +
				"[green::b] INFO[-:-:-] 13:04:20 first",
		},
		{
			name:     "empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "\n\n  \n\n",
			expected: "",
		},
		{
			name: "mixed valid and invalid JSON",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"valid"}
not json line
{"level":"error","time":"2025-11-20T13:04:22Z","message":"valid2"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 valid2\nnot json line\n[green::b] INFO[-:-:-] 13:04:20 valid",
		},
		{
			name:     "single line",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"single"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogContent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadLastLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		expected    string
		numLines    int
		expectError bool
	}{
		{
			name:     "read last 3 lines from 5",
			content:  "line1\nline2\nline3\nline4\nline5\n",
			numLines: 3,
			expected: "line3\nline4\nline5",
		},
		{
			name:     "read more lines than exist",
			content:  "line1\nline2\n",
			numLines: 10,
			expected: "line1\nline2",
		},
		{
			name:     "read all lines",
			content:  "line1\nline2\nline3\n",
			numLines: 3,
			expected: "line1\nline2\nline3",
		},
		{
			name:     "empty file",
			content:  "",
			numLines: 10,
			expected: "",
		},
		{
			name:     "single line no newline",
			content:  "single",
			numLines: 10,
			expected: "single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.log")
			err := os.WriteFile(tmpFile, []byte(tt.content), 0o600)
			require.NoError(t, err)

			// Test readLastLines
			result, err := readLastLines(tmpFile, tt.numLines)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestReadLastLinesNonexistentFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "doesnotexist.log")

	result, err := readLastLines(nonexistent, 10)

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "failed to read log file")
}
