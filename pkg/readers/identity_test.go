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

package readers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateReaderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		driverName string
		stablePath string
	}{
		{
			name:       "pn532 with USB topology",
			driverName: "pn532",
			stablePath: "1-2.3.1",
		},
		{
			name:       "libnfc with device descriptor",
			driverName: "libnfc",
			stablePath: "pn532_uart:/dev/ttyUSB0",
		},
		{
			name:       "mqtt with broker and topic",
			driverName: "mqtt",
			stablePath: "tcp://localhost:1883/zaparoo/tokens",
		},
		{
			name:       "file with path",
			driverName: "file",
			stablePath: "/tmp/tokens.txt",
		},
		{
			name:       "acr122pcsc with reader name",
			driverName: "acr122pcsc",
			stablePath: "ACS ACR122U PICC Interface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := GenerateReaderID(tt.driverName, tt.stablePath)

			// Driver name is lowercased in output
			expectedPrefix := strings.ToLower(tt.driverName) + "-"
			assert.True(t, strings.HasPrefix(id, expectedPrefix),
				"ID should start with lowercase driver name, got %q", id)

			parts := strings.SplitN(id, "-", 2)
			require.Len(t, parts, 2, "ID should have format driver-hash")

			hash := parts[1]
			assert.Len(t, hash, 8, "Hash should be 8 base32 chars")

			// Verify hash contains only lowercase base32 characters (a-z, 2-7)
			for _, c := range hash {
				isLetter := c >= 'a' && c <= 'z'
				isDigit := c >= '2' && c <= '7'
				assert.True(t, isLetter || isDigit,
					"Hash contains invalid base32 character: %c", c)
			}
		})
	}
}

func TestGenerateReaderID_Determinism(t *testing.T) {
	t.Parallel()

	id1 := GenerateReaderID("pn532", "1-2.3.1")
	id2 := GenerateReaderID("pn532", "1-2.3.1")
	id3 := GenerateReaderID("pn532", "1-2.3.1")

	assert.Equal(t, id1, id2, "IDs should be deterministic")
	assert.Equal(t, id2, id3, "IDs should be deterministic")
}

func TestGenerateReaderID_Uniqueness(t *testing.T) {
	t.Parallel()

	t.Run("different paths same driver", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("pn532", "1-2.3.1")
		id2 := GenerateReaderID("pn532", "1-2.3.2")

		assert.NotEqual(t, id1, id2)
	})

	t.Run("different drivers same path", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("pn532", "/dev/ttyUSB0")
		id2 := GenerateReaderID("libnfc", "/dev/ttyUSB0")

		assert.NotEqual(t, id1, id2)
	})
}

func TestGenerateReaderID_Normalization(t *testing.T) {
	t.Parallel()

	t.Run("case insensitive driver", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("PN532", "/dev/ttyUSB0")
		id2 := GenerateReaderID("pn532", "/dev/ttyUSB0")

		assert.Equal(t, id1, id2)
		assert.True(t, strings.HasPrefix(id1, "pn532-"))
	})

	t.Run("case insensitive path", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("file", "/TMP/Tokens.txt")
		id2 := GenerateReaderID("file", "/tmp/tokens.txt")

		assert.Equal(t, id1, id2)
	})

	t.Run("windows backslashes normalized", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("file", `C:\Users\Test\tokens.txt`)
		id2 := GenerateReaderID("file", "c:/users/test/tokens.txt")

		assert.Equal(t, id1, id2)
	})
}

func TestGenerateReaderID_EmptyInputs(t *testing.T) {
	t.Parallel()

	t.Run("empty driver", func(t *testing.T) {
		t.Parallel()

		id := GenerateReaderID("", "path")
		assert.True(t, strings.HasPrefix(id, "-"))
	})

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()

		id := GenerateReaderID("driver", "")
		assert.True(t, strings.HasPrefix(id, "driver-"))
	})

	t.Run("empty inputs produce different IDs", func(t *testing.T) {
		t.Parallel()

		id1 := GenerateReaderID("", "path")
		id2 := GenerateReaderID("driver", "")

		assert.NotEqual(t, id1, id2)
	})
}
