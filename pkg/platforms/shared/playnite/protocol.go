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

package playnite

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ProtocolVersion is the wire protocol this build of Core speaks. The
// extension reports its own in Hello.
const ProtocolVersion = 1

// MaxLineSize bounds one message from the extension. A games chunk carrying
// descriptions is the largest message; the extension sends libraries in
// chunks so a line stays well inside this.
const MaxLineSize = 8 * 1024 * 1024

// Event names sent by the extension.
const (
	EventHello           = "Hello"
	EventGames           = "Games"
	EventLaunchResult    = "LaunchResult"
	EventMediaStarted    = "MediaStarted"
	EventMediaStopped    = "MediaStopped"
	EventMediaStopResult = "MediaStopResult"
	EventWrite           = "Write"
	EventError           = "Error"
)

// Command names sent to the extension.
const (
	CommandPing     = "Ping"
	CommandGetGames = "GetGames"
	CommandLaunch   = "Launch"
	CommandStop     = "Stop"
)

// Result statuses reported by the extension.
const (
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusUnsupported = "unsupported"
)

// Platform is one of a game's Playnite platforms.
//
//nolint:tagliatelle // JSON tags match the C# extension's property names.
type Platform struct {
	SpecificationID string `json:"SpecificationId,omitempty"`
	Name            string `json:"Name,omitempty"`
}

// Game is one Playnite library entry as the extension reports it. The
// metadata and image fields are only filled when details were requested.
// Image fields are absolute paths to files in Playnite's library folder.
//
//nolint:tagliatelle // JSON tags match the C# extension's property names.
type Game struct {
	ID              string     `json:"Id"`
	Name            string     `json:"Name"`
	LibraryPluginID string     `json:"LibraryPluginId,omitempty"`
	RomPath         string     `json:"RomPath,omitempty"`
	Description     string     `json:"Description,omitempty"`
	Cover           string     `json:"Cover,omitempty"`
	Background      string     `json:"Background,omitempty"`
	Icon            string     `json:"Icon,omitempty"`
	Platforms       []Platform `json:"Platforms,omitempty"`
	Developers      []string   `json:"Developers,omitempty"`
	Publishers      []string   `json:"Publishers,omitempty"`
	Genres          []string   `json:"Genres,omitempty"`
	ReleaseYear     int        `json:"ReleaseYear,omitempty"`
	IsInstalled     bool       `json:"IsInstalled,omitempty"`
	Hidden          bool       `json:"Hidden,omitempty"`
}

// Command is a message from Core to the extension.
//
//nolint:tagliatelle // JSON tags match the C# extension's property names.
type Command struct {
	Command   string `json:"Command"`
	ID        string `json:"Id,omitempty"`
	RequestID string `json:"RequestId,omitempty"`
	Details   bool   `json:"Details,omitempty"`
}

// Event is a message from the extension. One struct carries every event's
// fields; which are set depends on Event.
//
//nolint:tagliatelle // JSON tags match the C# extension's property names.
type Event struct {
	// Game describes the game a MediaStarted event is for.
	Game *Game `json:"Game,omitempty"`
	// Event names the message.
	Event string `json:"Event"`
	// ID is the game a lifecycle or result event concerns.
	ID string `json:"Id,omitempty"`
	// Name is the game's name on a Write event.
	Name string `json:"Name,omitempty"`
	// RequestID echoes the GetGames request a Games chunk answers.
	RequestID string `json:"RequestId,omitempty"`
	// Status is the outcome on LaunchResult and MediaStopResult.
	Status string `json:"Status,omitempty"`
	// Error explains a failed result, a failed games request or an Error.
	Error string `json:"Error,omitempty"`
	// Command names the command an Error event rejects.
	Command string `json:"Command,omitempty"`
	// Exe is the image path of the started process, when Pid is set.
	Exe string `json:"Exe,omitempty"`
	// PluginVersion, PlayniteVersion and Mode describe the extension in Hello.
	PluginVersion   string `json:"PluginVersion,omitempty"`
	PlayniteVersion string `json:"PlayniteVersion,omitempty"`
	Mode            string `json:"Mode,omitempty"`
	// Games is one chunk of the library on a Games event.
	Games []Game `json:"Games,omitempty"`
	// ProtocolVersion is the extension's wire protocol, in Hello.
	ProtocolVersion int `json:"ProtocolVersion,omitempty"`
	// Pid is the process Playnite started for the game; zero when Playnite
	// did not report one.
	Pid int `json:"Pid,omitempty"`
	// Session numbers one run of a game inside the extension. MediaStarted
	// and the MediaStopped that ends the same run carry the same value.
	Session int `json:"Session,omitempty"`
	// Final marks the last Games chunk of a request.
	Final bool `json:"Final,omitempty"`
}

// ParseEvent decodes one line from the extension. The line is untrusted:
// anything that is not a JSON object naming an event is rejected.
func ParseEvent(line []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(line, &event); err != nil {
		return Event{}, fmt.Errorf("decode Playnite event: %w", err)
	}
	if event.Event == "" {
		return Event{}, errors.New("event from Playnite has no name")
	}
	return event, nil
}
