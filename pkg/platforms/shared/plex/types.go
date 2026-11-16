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

package plex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// FlexInt decodes a JSON number that Plex sends as either a number or a
// string, depending on the server version and endpoint.
type FlexInt int64

// UnmarshalJSON accepts a number, a numeric string, an empty string or null.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = 0
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("plex number: %w", err)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("plex number %q: %w", s, err)
		}
		*f = FlexInt(n)
		return nil
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("plex number %s: %w", data, err)
	}
	*f = FlexInt(n)
	return nil
}

// Section is one library section of a Plex server.
type Section struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// GUID is one external identity Plex holds for an item, such as tmdb://603.
type GUID struct {
	ID string `json:"id"`
}

// Metadata is a library item as Plex lists it.
//
//nolint:tagliatelle // Plex's JSON names these fields.
type Metadata struct {
	RatingKey        string  `json:"ratingKey"`
	Key              string  `json:"key"`
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	ParentTitle      string  `json:"parentTitle"`
	GrandparentTitle string  `json:"grandparentTitle"`
	ParentRatingKey  string  `json:"parentRatingKey"`
	GUID             string  `json:"guid"`
	GUIDs            []GUID  `json:"Guid"`
	Year             FlexInt `json:"year"`
	Index            FlexInt `json:"index"`
	ParentIndex      FlexInt `json:"parentIndex"`
	PlayQueueItemID  FlexInt `json:"playQueueItemID"`
	LibrarySectionID FlexInt `json:"librarySectionID"`
}

// mediaContainer is the envelope every Plex server JSON response uses.
//
//nolint:tagliatelle // Plex's JSON names these fields.
type mediaContainer struct {
	MachineIdentifier       string         `json:"machineIdentifier"`
	Version                 string         `json:"version"`
	Directory               []Section      `json:"Directory"`
	Metadata                []Metadata     `json:"Metadata"`
	Server                  []ServerClient `json:"Server"`
	Size                    FlexInt        `json:"size"`
	TotalSize               FlexInt        `json:"totalSize"`
	PlayQueueID             FlexInt        `json:"playQueueID"`
	PlayQueueSelectedItemID FlexInt        `json:"playQueueSelectedItemID"`
}

//nolint:tagliatelle // Plex's JSON names this field.
type containerResponse struct {
	MediaContainer mediaContainer `json:"MediaContainer"`
}

// Identity is the server's own identity.
type Identity struct {
	MachineIdentifier string
	Version           string
}

// PlayQueue is a queue the server built for a player to play.
type PlayQueue struct {
	Items          []Metadata
	ID             int64
	SelectedItemID int64
}

// SelectedItem is the item the queue starts on: the selected one, or the
// first when the server named none.
func (q *PlayQueue) SelectedItem() *Metadata {
	if q == nil || len(q.Items) == 0 {
		return nil
	}
	for i := range q.Items {
		if int64(q.Items[i].PlayQueueItemID) == q.SelectedItemID {
			return &q.Items[i]
		}
	}
	return &q.Items[0]
}

// ServerClient is a player the server knows through the Companion protocol.
type ServerClient struct {
	Name                 string  `json:"name"`
	Host                 string  `json:"host"`
	Address              string  `json:"address"`
	MachineIdentifier    string  `json:"machineIdentifier"`
	Product              string  `json:"product"`
	ProtocolCapabilities string  `json:"protocolCapabilities"`
	Port                 FlexInt `json:"port"`
}

// Connection is one way to reach a device plex.tv lists.
type Connection struct {
	Protocol string  `json:"protocol"`
	Address  string  `json:"address"`
	URI      string  `json:"uri"`
	Port     FlexInt `json:"port"`
	Local    bool    `json:"local"`
	Relay    bool    `json:"relay"`
}

// Resource is a server or player registered to a Plex account.
type Resource struct {
	Name             string       `json:"name"`
	Product          string       `json:"product"`
	ClientIdentifier string       `json:"clientIdentifier"`
	Provides         string       `json:"provides"`
	AccessToken      string       `json:"accessToken"`
	Connections      []Connection `json:"connections"`
	Owned            bool         `json:"owned"`
}

// ProvidesRole reports whether the resource lists a role, such as server or
// player, in its comma separated provides field.
func (r *Resource) ProvidesRole(role string) bool {
	for _, p := range strings.Split(r.Provides, ",") {
		if strings.EqualFold(strings.TrimSpace(p), role) {
			return true
		}
	}
	return false
}

// mediaKind describes one of the six kinds of library item the launchers
// handle.
type mediaKind struct {
	scheme      string
	typeName    string
	sectionType string
	queueType   string
	controlType string
	plexType    int
	collection  bool
}

// Plex metadata type numbers used by the library endpoints' type filter.
const (
	plexTypeMovie   = 1
	plexTypeShow    = 2
	plexTypeEpisode = 4
	plexTypeArtist  = 8
	plexTypeAlbum   = 9
	plexTypeTrack   = 10
)

var (
	kindMovie = &mediaKind{
		scheme: shared.SchemePlexMovie, typeName: "movie", sectionType: "movie",
		queueType: "video", controlType: "video", plexType: plexTypeMovie,
	}
	kindEpisode = &mediaKind{
		scheme: shared.SchemePlexEpisode, typeName: "episode", sectionType: "show",
		queueType: "video", controlType: "video", plexType: plexTypeEpisode,
	}
	kindShow = &mediaKind{
		scheme: shared.SchemePlexShow, typeName: "show", sectionType: "show",
		queueType: "video", controlType: "video", plexType: plexTypeShow, collection: true,
	}
	kindTrack = &mediaKind{
		scheme: shared.SchemePlexSong, typeName: "track", sectionType: "artist",
		queueType: "audio", controlType: "music", plexType: plexTypeTrack,
	}
	kindAlbum = &mediaKind{
		scheme: shared.SchemePlexAlbum, typeName: "album", sectionType: "artist",
		queueType: "audio", controlType: "music", plexType: plexTypeAlbum, collection: true,
	}
	kindArtist = &mediaKind{
		scheme: shared.SchemePlexArtist, typeName: "artist", sectionType: "artist",
		queueType: "audio", controlType: "music", plexType: plexTypeArtist, collection: true,
	}
)
