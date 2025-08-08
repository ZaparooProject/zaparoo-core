//go:build linux

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

package cores

import (
	"testing"
)

func TestPathToMGLDef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantMgl  *MGLParams
		name     string
		systemID string
		path     string
		wantErr  bool
	}{
		{
			name:     "Exact extension match",
			systemID: "Atari5200",
			path:     "shooter_game.a52",
			wantMgl:  Systems["Atari5200"].Slots[0].Mgl,
		},
		{
			name:     "Case-insensitive path",
			systemID: "NES",
			path:     "PLATFORMER_GAME.NES",
			wantMgl:  Systems["NES"].Slots[0].Mgl,
		},
		{
			name:     "Nil MGL allowed (Arcade .mra)",
			systemID: "Arcade",
			path:     "maze_game.mra",
			wantMgl:  nil,
		},
		{
			name:     "Multiple extensions - first match wins",
			systemID: "Atari7800",
			path:     "game.bin",
			wantMgl:  Systems["Atari7800"].Slots[0].Mgl,
		},
		{
			name:     "PSX CD format",
			systemID: "PSX",
			path:     "rpg_game.cue",
			wantMgl:  Systems["PSX"].Slots[0].Mgl,
		},
		{
			name:     "PSX executable format",
			systemID: "PSX",
			path:     "homebrew.exe",
			wantMgl:  Systems["PSX"].Slots[1].Mgl,
		},
		{
			name:     "No matching extension returns error",
			systemID: "NES",
			path:     "unknown.zip",
			wantErr:  true,
		},
		{
			name:     "Empty path returns error",
			systemID: "NES",
			path:     "",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sys := Systems[tc.systemID] // Safe copy; map holds value not pointer.
			got, err := PathToMGLDef(&sys, tc.path)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantMgl == nil && got != nil {
				t.Fatalf("expected nil MGLParams, got %#v", got)
			}
			if tc.wantMgl != nil && (got == nil ||
				*got != *tc.wantMgl) {
				t.Fatalf("mismatch.\nwant: %#v\ngot:  %#v", tc.wantMgl, got)
			}
		})
	}
}

func TestGetCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name: "Known system",
			id:   "Genesis",
		},
		{
			name: "Another known system",
			id:   "NES",
		},
		{
			name:    "Unknown system",
			id:      "DoesNotExist",
			wantErr: true,
		},
		{
			name:    "Empty id",
			id:      "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := GetCore(tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.ID != tc.id {
				t.Fatalf("unexpected core: %#v", got)
			}
		})
	}
}

func TestGetGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		groupID   string
		wantSlots int
		wantErr   bool
	}{
		{
			name:      "Two-system group (Atari7800)",
			groupID:   "Atari7800",
			wantSlots: len(Systems["Atari7800"].Slots) + len(Systems["Atari2600"].Slots),
		},
		{
			name:      "Two-system group (Gameboy)",
			groupID:   "Gameboy",
			wantSlots: len(Systems["Gameboy"].Slots) + len(Systems["GameboyColor"].Slots),
		},
		{
			name:      "Three-system group (NES)",
			groupID:   "NES",
			wantSlots: len(Systems["NES"].Slots) + len(Systems["NESMusic"].Slots) + len(Systems["FDS"].Slots),
		},
		{
			name:      "Two-system group (SNES)",
			groupID:   "SNES",
			wantSlots: len(Systems["SNES"].Slots) + len(Systems["SNESMusic"].Slots),
		},
		{
			name:    "Unknown group id",
			groupID: "DoesNotExist",
			wantErr: true,
		},
		{
			name:    "Empty group id",
			groupID: "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			merged, err := GetGroup(tc.groupID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := len(merged.Slots); got != tc.wantSlots {
				t.Fatalf("slot count mismatch: want %d, got %d", tc.wantSlots, got)
			}
		})
	}
}

func TestLookupCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		expectGroup bool
		wantErr     bool
	}{
		{
			name:        "Resolves to merged group when id appears in both maps",
			query:       "Atari7800",
			expectGroup: true,
		},
		{
			name:  "Case-insensitive system lookup",
			query: "neS",
		},
		{
			name:        "Case-insensitive group lookup",
			query:       "gameboy",
			expectGroup: true,
		},
		{
			name:  "Exact system match",
			query: "Genesis",
		},
		{
			name:        "Group with multiple systems",
			query:       "SNES",
			expectGroup: true,
		},
		{
			name:    "Unknown id",
			query:   "TotallyUnknown",
			wantErr: true,
		},
		{
			name:    "Empty id",
			query:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := LookupCore(tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expectGroup {
				// For group result we expect the merged group behavior
				// Check that we have a valid group result
				if _, ok := CoreGroups[got.ID]; !ok {
					t.Fatalf("expected a group core, got %+v", got)
				}
			} else {
				// For system result, verify it's a known system
				if _, ok := Systems[got.ID]; !ok {
					t.Fatalf("expected a system core, got %+v", got)
				}
			}
		})
	}
}
