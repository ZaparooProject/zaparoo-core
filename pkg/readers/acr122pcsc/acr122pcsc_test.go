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

package acr122pcsc

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAcr122Pcsc(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewAcr122Pcsc(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}
	metadata := reader.Metadata()

	assert.Equal(t, "acr122pcsc", metadata.ID)
	assert.Equal(t, "ACR122 NFC reader via PC/SC", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}
	ids := reader.IDs()

	require.Len(t, ids, 2)
	assert.Equal(t, "acr122pcsc", ids[0])
	assert.Equal(t, "acr122_pcsc", ids[1])
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}

	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "ACR122 PC/SC reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &ACR122PCSC{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		polling  bool
		expected bool
	}{
		{
			name:     "not polling",
			polling:  false,
			expected: false,
		},
		{
			name:     "polling",
			polling:  true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &ACR122PCSC{
				polling: tt.polling,
			}

			assert.Equal(t, tt.expected, reader.Connected())
		})
	}
}
