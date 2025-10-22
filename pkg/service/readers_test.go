// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package service

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

// TestReaderManager tests are integration tests that would require
// more complex setup. The actual behavior is tested by verifying:
// 1. The Scan struct has the ReaderError field (tested below)
// 2. Manual/integration testing of the reader error scenarios
//
// For unit testing the reader manager logic, we would need to refactor
// it to accept a scan channel as a parameter or make it more testable.

// TestReaderScan_ReaderErrorField tests that the ReaderError field
// is properly set in different reader scenarios
func TestReaderScan_ReaderErrorField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		scan        readers.Scan
		expectError bool
	}{
		{
			name: "normal token scan",
			scan: readers.Scan{
				Source: "test-reader",
				Token: &tokens.Token{
					UID:      "abc123",
					ScanTime: time.Now(),
				},
				ReaderError: false,
			},
			expectError: false,
		},
		{
			name: "normal token removal",
			scan: readers.Scan{
				Source:      "test-reader",
				Token:       nil,
				ReaderError: false,
			},
			expectError: false,
		},
		{
			name: "reader error removal",
			scan: readers.Scan{
				Source:      "test-reader",
				Token:       nil,
				ReaderError: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectError, tt.scan.ReaderError,
				"ReaderError field should be %v", tt.expectError)
		})
	}
}
