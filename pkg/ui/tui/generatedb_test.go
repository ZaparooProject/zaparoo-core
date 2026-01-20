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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestNewProgressBar(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	assert.NotNil(t, pb)
	assert.NotNil(t, pb.Box)
	assert.InDelta(t, 0.0, pb.progress, 0.001)
}

func TestProgressBar_SetProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{
			name:     "zero progress",
			input:    0.0,
			expected: 0.0,
		},
		{
			name:     "half progress",
			input:    0.5,
			expected: 0.5,
		},
		{
			name:     "full progress",
			input:    1.0,
			expected: 1.0,
		},
		{
			name:     "negative clamped to zero",
			input:    -0.5,
			expected: 0.0,
		},
		{
			name:     "over one clamped to one",
			input:    1.5,
			expected: 1.0,
		},
		{
			name:     "quarter progress",
			input:    0.25,
			expected: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pb := NewProgressBar()
			result := pb.SetProgress(tt.input)

			assert.InDelta(t, tt.expected, pb.GetProgress(), 0.001)
			assert.Equal(t, pb, result, "SetProgress should return self for chaining")
		})
	}
}

func TestProgressBar_GetProgress(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Initial progress
	assert.InDelta(t, 0.0, pb.GetProgress(), 0.001)

	// After setting
	pb.SetProgress(0.75)
	assert.InDelta(t, 0.75, pb.GetProgress(), 0.001)
}

func TestProgressBar_Chaining(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Verify chaining works
	result := pb.SetProgress(0.5)
	assert.Equal(t, pb, result)

	// Multiple chains
	pb.SetProgress(0.1).SetProgress(0.2).SetProgress(0.3)
	assert.InDelta(t, 0.3, pb.GetProgress(), 0.001)
}

func TestFormatDBStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		db       models.IndexingStatusResponse
		expected string
	}{
		{
			name: "no database exists",
			db: models.IndexingStatusResponse{
				Exists: false,
			},
			expected: "No database found. Run update to scan your media folders.",
		},
		{
			name: "database exists with media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(100),
			},
			expected: "Database contains 100 indexed media files.",
		},
		{
			name: "database exists with zero media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(0),
			},
			expected: "Database contains 0 indexed media files.",
		},
		{
			name: "database exists with nil TotalMedia",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: nil,
			},
			expected: "Database contains 0 indexed media files.",
		},
		{
			name: "database exists with large count",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(12345),
			},
			expected: "Database contains 12345 indexed media files.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatDBStats(tt.db)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// intPtr is a helper to create *int.
func intPtr(v int) *int {
	return &v
}
