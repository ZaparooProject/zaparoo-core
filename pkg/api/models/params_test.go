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

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestReaderConnection_IsEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rc       ReaderConnection
		expected bool
	}{
		{
			name:     "nil enabled means enabled (default)",
			rc:       ReaderConnection{Driver: "pn532"},
			expected: true,
		},
		{
			name:     "explicit true means enabled",
			rc:       ReaderConnection{Driver: "pn532", Enabled: boolPtr(true)},
			expected: true,
		},
		{
			name:     "explicit false means disabled",
			rc:       ReaderConnection{Driver: "pn532", Enabled: boolPtr(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.rc.IsEnabled())
		})
	}
}
