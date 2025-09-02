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

package kodi

import "encoding/json"

// URL scheme constants for Kodi media
const (
	SchemeKodiMovie   = "kodi-movie"
	SchemeKodiEpisode = "kodi-episode"
	SchemeKodiSong    = "kodi-song"
	SchemeKodiAlbum   = "kodi-album"
	SchemeKodiArtist  = "kodi-artist"
	SchemeKodiShow    = "kodi-show"
)

// Player represents an active Kodi player
type Player struct {
	Type string `json:"type"`
	ID   int    `json:"playerid"`
}

// Movie represents a movie in Kodi's library
type Movie struct {
	Label string `json:"label"`
	File  string `json:"file,omitempty"`
	ID    int    `json:"movieid"`
}

// TVShow represents a TV show in Kodi's library
type TVShow struct {
	Label string `json:"label"`
	ID    int    `json:"tvshowid"`
}

// Episode represents a TV episode in Kodi's library
type Episode struct {
	Label    string `json:"label"`
	File     string `json:"file,omitempty"`
	ID       int    `json:"episodeid"`
	TVShowID int    `json:"tvshowid"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
}

// Song represents a song in Kodi's library
type Song struct {
	Label    string `json:"label"`
	File     string `json:"file,omitempty"`
	Artist   string `json:"artist"`
	ID       int    `json:"songid"`
	AlbumID  int    `json:"albumid"`
	Duration int    `json:"duration"`
}

// Album represents an album in Kodi's library
type Album struct {
	Label  string `json:"label"`
	Artist string `json:"artist"`
	ID     int    `json:"albumid"`
	Year   int    `json:"year"`
}

// Artist represents an artist in Kodi's library
type Artist struct {
	Label string `json:"label"`
	ID    int    `json:"artistid"`
}

// APIMethod represents Kodi JSON-RPC API methods
type APIMethod string

// Kodi API methods
const (
	APIMethodPlayerOpen              APIMethod = "Player.Open"
	APIMethodPlayerGetActivePlayers  APIMethod = "Player.GetActivePlayers"
	APIMethodPlayerStop              APIMethod = "Player.Stop"
	APIMethodVideoLibraryGetMovies   APIMethod = "VideoLibrary.GetMovies"
	APIMethodVideoLibraryGetTVShows  APIMethod = "VideoLibrary.GetTVShows"
	APIMethodVideoLibraryGetEpisodes APIMethod = "VideoLibrary.GetEpisodes"

	// Audio Library
	APIMethodAudioLibraryGetSongs   APIMethod = "AudioLibrary.GetSongs"
	APIMethodAudioLibraryGetAlbums  APIMethod = "AudioLibrary.GetAlbums"
	APIMethodAudioLibraryGetArtists APIMethod = "AudioLibrary.GetArtists"

	// Playlist Management (for collections)
	APIMethodPlaylistClear APIMethod = "Playlist.Clear"
	APIMethodPlaylistAdd   APIMethod = "Playlist.Add"
)

// APIPayload represents a Kodi JSON-RPC request
type APIPayload struct {
	Params  any       `json:"params,omitempty"`
	JSONRPC string    `json:"jsonrpc"`
	ID      string    `json:"id"`
	Method  APIMethod `json:"method"`
}

// APIError represents a Kodi JSON-RPC error
type APIError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// APIResponse represents a Kodi JSON-RPC response
type APIResponse struct {
	Error   *APIError       `json:"error,omitempty"`
	ID      string          `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
}

// Item represents a media item that can be played
type Item struct {
	Label      string `json:"label,omitempty"`
	File       string `json:"file,omitempty"`
	MovieID    int    `json:"movieid,omitempty"`
	TVShowID   int    `json:"tvshowid,omitempty"`
	EpisodeID  int    `json:"episodeid,omitempty"`
	SongID     int    `json:"songid,omitempty"`
	PlaylistID int    `json:"playlistid"`
}

// ItemOptions represents options for playing a media item
type ItemOptions struct {
	Resume bool `json:"resume"`
}

// PlayerOpenParams represents parameters for Player.Open API method
type PlayerOpenParams struct {
	Item    Item        `json:"item"`
	Options ItemOptions `json:"options,omitempty"`
}

// PlayerStopParams represents parameters for Player.Stop API method
type PlayerStopParams struct {
	PlayerID int `json:"playerid"`
}

// VideoLibraryGetMoviesResponse represents the response from VideoLibrary.GetMovies
type VideoLibraryGetMoviesResponse struct {
	Movies []Movie `json:"movies"`
}

// VideoLibraryGetTVShowsResponse represents the response from VideoLibrary.GetTVShows
type VideoLibraryGetTVShowsResponse struct {
	TVShows []TVShow `json:"tvshows"`
}

// VideoLibraryGetEpisodesParams represents parameters for VideoLibrary.GetEpisodes API method
type VideoLibraryGetEpisodesParams struct {
	TVShowID int `json:"tvshowid"`
}

// VideoLibraryGetEpisodesResponse represents the response from VideoLibrary.GetEpisodes
type VideoLibraryGetEpisodesResponse struct {
	Episodes []Episode `json:"episodes"`
}

// AudioLibraryGetSongsResponse represents the response from AudioLibrary.GetSongs
type AudioLibraryGetSongsResponse struct {
	Songs []Song `json:"songs"`
}

// AudioLibraryGetAlbumsResponse represents the response from AudioLibrary.GetAlbums
type AudioLibraryGetAlbumsResponse struct {
	Albums []Album `json:"albums"`
}

// AudioLibraryGetArtistsResponse represents the response from AudioLibrary.GetArtists
type AudioLibraryGetArtistsResponse struct {
	Artists []Artist `json:"artists"`
}

// PlaylistClearParams represents parameters for Playlist.Clear API method
type PlaylistClearParams struct {
	PlaylistID int `json:"playlistid"`
}

// PlaylistAddParams represents parameters for Playlist.Add API method
type PlaylistAddParams struct {
	Item       []PlaylistItemSongID `json:"item"`
	PlaylistID int                  `json:"playlistid"`
}

// PlaylistAddEpisodesParams represents parameters for Playlist.Add API method with episodes
type PlaylistAddEpisodesParams struct {
	Item       []PlaylistItemEpisodeID `json:"item"`
	PlaylistID int                     `json:"playlistid"`
}

// PlaylistItemSongID represents a song item for playlist operations
type PlaylistItemSongID struct {
	SongID int `json:"songid"`
}

// PlaylistItemEpisodeID represents an episode item for playlist operations
type PlaylistItemEpisodeID struct {
	EpisodeID int `json:"episodeid"`
}

// FilterRule represents a Kodi API filter rule
type FilterRule struct {
	Value    any    `json:"value"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
}

// AudioLibraryGetSongsParams represents parameters for AudioLibrary.GetSongs API method
type AudioLibraryGetSongsParams struct {
	Filter *FilterRule `json:"filter,omitempty"`
}
