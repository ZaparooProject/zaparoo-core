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

package mocks_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
)

func TestNewMockKodiClient_ImplementsInterface(t *testing.T) {
	t.Parallel()

	// Test that our mock can be used as a KodiClient
	mock := mocks.NewMockKodiClient()

	// Verify it implements the interface
	var client kodi.KodiClient = mock
	assert.NotNil(t, client)

	// Test that basic mock functionality works
	err := client.LaunchFile("/test/path")
	assert.NoError(t, err) // Should succeed due to SetupBasicMock
}
