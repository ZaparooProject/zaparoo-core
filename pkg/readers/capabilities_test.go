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

package readers_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockReader creates a MockReader configured with the specified
// readerID and capabilities. The reader is configured as connected by default.
func createMockReader(readerID string, caps []readers.Capability) *mocks.MockReader {
	m := mocks.NewMockReader()
	m.On("ReaderID").Return(readerID)
	m.On("Capabilities").Return(caps)
	m.On("Connected").Return(true)
	return m
}

func TestHasCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		check        readers.Capability
		capabilities []readers.Capability
		expected     bool
	}{
		{
			name:         "has write capability",
			capabilities: []readers.Capability{readers.CapabilityWrite},
			check:        readers.CapabilityWrite,
			expected:     true,
		},
		{
			name:         "does not have write capability",
			capabilities: []readers.Capability{},
			check:        readers.CapabilityWrite,
			expected:     false,
		},
		{
			name:         "has multiple capabilities including write",
			capabilities: []readers.Capability{readers.CapabilityDisplay, readers.CapabilityWrite},
			check:        readers.CapabilityWrite,
			expected:     true,
		},
		{
			name:         "has display but not write",
			capabilities: []readers.Capability{readers.CapabilityDisplay},
			check:        readers.CapabilityWrite,
			expected:     false,
		},
		{
			name:         "nil capabilities slice",
			capabilities: nil,
			check:        readers.CapabilityWrite,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := mocks.NewMockReader()
			m.On("Capabilities").Return(tt.capabilities)
			result := readers.HasCapability(m, tt.check)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterByCapability(t *testing.T) {
	t.Parallel()

	t.Run("filters to write-capable readers", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("writer", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("display-only", []readers.Capability{readers.CapabilityDisplay}),
			createMockReader("both", []readers.Capability{readers.CapabilityDisplay, readers.CapabilityWrite}),
		}

		result := readers.FilterByCapability(rs, readers.CapabilityWrite)

		assert.Len(t, result, 2)
		assert.Equal(t, "writer", result[0].ReaderID())
		assert.Equal(t, "both", result[1].ReaderID())
	})

	t.Run("returns empty slice when no matches", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("display-only", []readers.Capability{readers.CapabilityDisplay}),
		}

		result := readers.FilterByCapability(rs, readers.CapabilityWrite)

		assert.Empty(t, result)
	})

	t.Run("handles nil entries", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			nil,
			createMockReader("writer", []readers.Capability{readers.CapabilityWrite}),
		}

		result := readers.FilterByCapability(rs, readers.CapabilityWrite)

		assert.Len(t, result, 1)
		assert.Equal(t, "writer", result[0].ReaderID())
	})

	t.Run("handles empty input", func(t *testing.T) {
		t.Parallel()

		result := readers.FilterByCapability([]readers.Reader{}, readers.CapabilityWrite)
		assert.Empty(t, result)

		result = readers.FilterByCapability(nil, readers.CapabilityWrite)
		assert.Empty(t, result)
	})
}

func TestSelectWriterStrict(t *testing.T) {
	t.Parallel()

	t.Run("finds reader by ID", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterStrict(rs, "reader-2")

		require.NoError(t, err)
		assert.Equal(t, "reader-2", result.ReaderID())
	})

	t.Run("errors if reader not found", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
		}

		_, err := readers.SelectWriterStrict(rs, "nonexistent")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader not found")
	})

	t.Run("errors if reader not connected", func(t *testing.T) {
		t.Parallel()

		m := createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite})
		m.ExpectedCalls = nil // Clear default Connected mock
		m.On("ReaderID").Return("reader-1")
		m.On("Connected").Return(false)

		rs := []readers.Reader{m}

		_, err := readers.SelectWriterStrict(rs, "reader-1")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader not connected")
	})

	t.Run("errors if reader lacks write capability", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("display-only", []readers.Capability{readers.CapabilityDisplay}),
		}

		_, err := readers.SelectWriterStrict(rs, "display-only")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not have write capability")
	})

	t.Run("skips nil readers in list", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			nil,
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			nil,
		}

		result, err := readers.SelectWriterStrict(rs, "reader-1")

		require.NoError(t, err)
		assert.Equal(t, "reader-1", result.ReaderID())
	})
}

func TestSelectWriterPreferred(t *testing.T) {
	t.Parallel()

	t.Run("selects first preferred ID", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"reader-2"})

		require.NoError(t, err)
		assert.Equal(t, "reader-2", result.ReaderID())
	})

	t.Run("tries preferences in order", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-3", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"reader-3", "reader-1"})

		require.NoError(t, err)
		assert.Equal(t, "reader-3", result.ReaderID())
	})

	t.Run("skips non-existent preferred IDs", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"nonexistent", "reader-2"})

		require.NoError(t, err)
		assert.Equal(t, "reader-2", result.ReaderID())
	})

	t.Run("skips non-write-capable preferred IDs", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("display-only", []readers.Capability{readers.CapabilityDisplay}),
			createMockReader("writer", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"display-only", "writer"})

		require.NoError(t, err)
		assert.Equal(t, "writer", result.ReaderID())
	})

	t.Run("falls back to first write-capable when no preferences match", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"nonexistent"})

		require.NoError(t, err)
		assert.Equal(t, "reader-1", result.ReaderID())
	})

	t.Run("falls back to first write-capable when no preferences given", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, nil)

		require.NoError(t, err)
		assert.Equal(t, "reader-1", result.ReaderID())
	})

	t.Run("errors when no write-capable readers", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("display-only", []readers.Capability{readers.CapabilityDisplay}),
		}

		_, err := readers.SelectWriterPreferred(rs, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no readers with write capability")
	})

	t.Run("errors when empty reader list", func(t *testing.T) {
		t.Parallel()

		_, err := readers.SelectWriterPreferred([]readers.Reader{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no readers with write capability")
	})

	t.Run("skips empty strings in preferred IDs", func(t *testing.T) {
		t.Parallel()

		rs := []readers.Reader{
			createMockReader("reader-1", []readers.Capability{readers.CapabilityWrite}),
			createMockReader("reader-2", []readers.Capability{readers.CapabilityWrite}),
		}

		result, err := readers.SelectWriterPreferred(rs, []string{"", "reader-2", ""})

		require.NoError(t, err)
		assert.Equal(t, "reader-2", result.ReaderID())
	})
}
