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

package fixtures

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
)

// NewSamplePlaylist creates a sample playlist with typical values
func NewSamplePlaylist() *playlists.Playlist {
	items := []playlists.PlaylistItem{
		{ZapScript: "**LAUNCH:zelda:botw", Name: "Zelda: Breath of the Wild"},
		{ZapScript: "**LAUNCH:mario:odyssey", Name: "Super Mario Odyssey"},
		{ZapScript: "**LAUNCH:metroid:dread", Name: "Metroid Dread"},
	}
	return playlists.NewPlaylist("sample-playlist", "Sample Gaming Playlist", items)
}

// NewRetroPlaylist creates a retro gaming playlist
func NewRetroPlaylist() *playlists.Playlist {
	items := []playlists.PlaylistItem{
		{ZapScript: "**LAUNCH:mario:bros", Name: "Super Mario Bros"},
		{ZapScript: "**LAUNCH:zelda:original", Name: "Legend of Zelda"},
		{ZapScript: "**LAUNCH:metroid:original", Name: "Metroid"},
		{ZapScript: "**LAUNCH:donkey:kong", Name: "Donkey Kong"},
	}
	return playlists.NewPlaylist("retro-playlist", "Retro Classics", items)
}

// NewEmptyPlaylist creates an empty playlist for testing
func NewEmptyPlaylist() *playlists.Playlist {
	return playlists.NewPlaylist("empty-playlist", "Empty Playlist", []playlists.PlaylistItem{})
}

// SamplePlaylists returns a collection of sample playlists for testing
func SamplePlaylists() []*playlists.Playlist {
	return []*playlists.Playlist{
		NewSamplePlaylist(),
		NewRetroPlaylist(),
		NewEmptyPlaylist(),
	}
}
