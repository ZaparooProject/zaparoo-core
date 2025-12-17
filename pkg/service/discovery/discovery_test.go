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

package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		platformID string
	}{
		{"mister platform", "mister"},
		{"linux platform", "linux"},
		{"steamos platform", "steamos"},
		{"windows platform", "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := New(nil, tt.platformID)

			assert.NotNil(t, svc)
			assert.Equal(t, tt.platformID, svc.platformID)
		})
	}
}

func TestServiceType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "_zaparoo._tcp", ServiceType)
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()

	svc := New(nil, "test")

	// Stop should be safe to call multiple times even when not started
	svc.Stop()
	svc.Stop()
	svc.Stop()

	// No panic means success
	assert.Nil(t, svc.server)
}
