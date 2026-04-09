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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveMedia_Equal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a        *ActiveMedia
		b        *ActiveMedia
		name     string
		expected bool
	}{
		{
			name: "identical media",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			expected: true,
		},
		{
			name: "same name different paths (Kodi virtual vs real path) - episode format normalized",
			a: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "kodi-episode://1892/Attack%20on%20Titan%20-%201x02",
				Name:       "Attack on Titan - 1x02. That Day: The Fall of Shiganshina",
			},
			b: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "smb://marge.lan/TV/Attack on Titan/Season 1/Attack on Titan (2013) - S01E02 (2).mkv",
				Name:       "Attack on Titan - S01E02 - That Day: The Fall of Shiganshina (2)",
			},
			expected: true, // Now matches because episode normalization: 1x02 → s01e02, S01E02 → s01e02
		},
		{
			name: "same name minor formatting differences",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/different/path/mario.nes",
				Name:       "Super Mario Brothers",
			},
			expected: true,
		},
		{
			name: "same path different names",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Different Game",
			},
			expected: true,
		},
		{
			name: "different system",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b: &ActiveMedia{
				SystemID:   "snes",
				SystemName: "SNES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			expected: false,
		},
		{
			name: "different name and path",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/zelda.nes",
				Name:       "The Legend of Zelda",
			},
			expected: false,
		},
		{
			name: "nil comparison",
			a: &ActiveMedia{
				SystemID:   "nes",
				SystemName: "NES",
				Path:       "/roms/mario.nes",
				Name:       "Super Mario Bros.",
			},
			b:        nil,
			expected: false,
		},
		{
			name: "episode formatting variations - episode format normalized",
			a: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "kodi-episode://123",
				Name:       "Breaking Bad - 1x05 - Gray Matter",
			},
			b: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "/tv/breaking-bad-s01e05.mkv",
				Name:       "Breaking Bad - S01E05 - Gray Matter",
			},
			expected: true, // Now matches because episode normalization: 1x05 → s01e05, S01E05 → s01e05
		},
		{
			name: "episode same format - should match",
			a: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "kodi-episode://123",
				Name:       "Breaking Bad - S01E05 - Gray Matter",
			},
			b: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "/tv/breaking-bad-s01e05.mkv",
				Name:       "Breaking Bad - S01E05 - Gray Matter (alternate)",
			},
			expected: true, // Same because S01E05 format matches after slugification
		},
		{
			name: "different MediaTypes - Game vs TVShow - should not match",
			a: &ActiveMedia{
				SystemID:   "PS2",
				SystemName: "PlayStation 2",
				Path:       "/games/inception.iso",
				Name:       "Inception",
			},
			b: &ActiveMedia{
				SystemID:   "TVShow",
				SystemName: "TV Show",
				Path:       "kodi-show://456/Inception",
				Name:       "Inception",
			},
			expected: false, // Different systems (and different MediaTypes) should not match
		},
		{
			name: "different MediaTypes - Game vs Movie with episode-like title",
			a: &ActiveMedia{
				SystemID:   "NES",
				SystemName: "NES",
				Path:       "/games/lost-s01e01.nes",
				Name:       "Lost - S01E01",
			},
			b: &ActiveMedia{
				SystemID:   "TVEpisode",
				SystemName: "TV Episode",
				Path:       "kodi-episode://789/Lost%20-%20S01E01",
				Name:       "Lost - S01E01 - Pilot",
			},
			expected: false, // Different systems despite having similar episode-like title
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.a.Equal(tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPairedClient_JSONShape pins the wire shape of the PairedClient
// type returned by the `clients` RPC method. This test exists to catch a
// future regression where someone embeds *database.Client (which contains
// AuthToken and PairingKey fields) into PairedClient — that would silently
// leak the auth token and the long-term pairing key over the API.
//
// The pin works by asserting the marshalled JSON has EXACTLY the expected
// keys and that several variants of the sensitive field names are absent.
func TestPairedClient_JSONShape(t *testing.T) {
	t.Parallel()

	pc := PairedClient{
		ClientID:   "client-123",
		ClientName: "Test Client",
		CreatedAt:  1700000000,
		LastSeenAt: 1700001000,
	}
	raw, err := json.Marshal(pc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	expectedKeys := map[string]bool{
		"clientId":   true,
		"clientName": true,
		"createdAt":  true,
		"lastSeenAt": true,
	}
	for k := range got {
		assert.True(t, expectedKeys[k],
			"unexpected JSON key %q in PairedClient — could indicate a struct embed leaking sensitive fields", k)
	}
	for k := range expectedKeys {
		_, ok := got[k]
		assert.True(t, ok, "expected JSON key %q missing from PairedClient", k)
	}

	// Defense in depth: explicitly assert sensitive field names are absent
	// even if some future serializer adds them under a different naming.
	forbidden := []string{
		"authToken", "auth_token", "AuthToken",
		"pairingKey", "pairing_key", "PairingKey",
	}
	for _, k := range forbidden {
		_, present := got[k]
		assert.False(t, present,
			"sensitive field %q must NEVER appear in PairedClient JSON", k)
	}
}

// TestClientsResponse_JSONShape pins the wire shape of the `clients` RPC
// list response. Catches accidental key renames.
func TestClientsResponse_JSONShape(t *testing.T) {
	t.Parallel()

	cr := ClientsResponse{
		Clients: []PairedClient{
			{ClientID: "a", ClientName: "First", CreatedAt: 1, LastSeenAt: 2},
			{ClientID: "b", ClientName: "Second", CreatedAt: 3, LastSeenAt: 4},
		},
	}
	raw, err := json.Marshal(cr)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	clients, ok := got["clients"].([]any)
	require.True(t, ok, "ClientsResponse must marshal to {clients: [...]}")
	assert.Len(t, clients, 2)
}

// TestClientsDeleteParams_JSONShape pins the wire shape of the
// `clients.delete` RPC parameters object.
func TestClientsDeleteParams_JSONShape(t *testing.T) {
	t.Parallel()

	p := ClientsDeleteParams{ClientID: "abc"}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	assert.JSONEq(t, `{"clientId":"abc"}`, string(raw))
}

// TestClientsPairedNotification_JSONShape pins the wire shape of the
// `clients.paired` notification payload broadcast on successful pairing.
func TestClientsPairedNotification_JSONShape(t *testing.T) {
	t.Parallel()

	n := ClientsPairedNotification{
		ClientID:   "id-1",
		ClientName: "App",
	}
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	assert.JSONEq(t, `{"clientId":"id-1","clientName":"App"}`, string(raw))

	// Also assert no sensitive fields can leak from this notification.
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	for _, k := range []string{"authToken", "pairingKey"} {
		_, present := got[k]
		assert.False(t, present,
			"clients.paired notification must never include %q", k)
	}
}
