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

package database

// PlaylistMemoryStore is the optional UserDB capability behind playlist
// memory, restoring open playlists after a restart and resuming long audio.
// Database test doubles need none of it; without it nothing is remembered.
type PlaylistMemoryStore interface {
	GetPlaylistMemory(profileID, playlistKey string) (PlaylistMemory, bool, error)
	UpsertPlaylistMemory(mem *PlaylistMemory, keep int) error
	DeletePlaylistMemory(profileID, playlistKey string) error
	ListOpenPlaylists() ([]OpenPlaylist, error)
	UpsertOpenPlaylist(row *OpenPlaylist) error
	DeleteOpenPlaylist(slot string) error
	GetMediaPosition(profileID, systemID, path string) (MediaPosition, bool, error)
	UpsertMediaPosition(pos *MediaPosition, keep int) error
	DeleteMediaPosition(profileID, systemID, path string) error
}

// PlaylistMemory is where one playlist was left for one profile. ProfileID ""
// is the shared state used when no profile is active. Order is the item order
// a shuffled playlist was playing in, as item ZapScripts, and is empty for a
// playlist that plays in its own order.
type PlaylistMemory struct {
	ProfileID     string
	PlaylistKey   string
	ItemZapScript string
	ItemsHash     string
	Order         []string
	ItemIndex     int
	UpdatedAt     int64
}

// OpenPlaylist is the playlist open in one media slot, kept so it can be
// restored after a restart. Snapshot is an encoded playlist owned by the
// service that writes it.
type OpenPlaylist struct {
	Slot      string
	ProfileID string
	Snapshot  string
	UpdatedAt int64
}

// MediaPosition is how far into one piece of media playback got for one
// profile.
type MediaPosition struct {
	ProfileID  string
	SystemID   string
	Path       string
	PositionMs int64
	DurationMs int64
	UpdatedAt  int64
}
