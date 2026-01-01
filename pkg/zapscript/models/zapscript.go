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

import "encoding/json"

const (
	ZapScriptCmdLaunch       = "launch"
	ZapScriptCmdLaunchSystem = "launch.system"
	ZapScriptCmdLaunchRandom = "launch.random"
	ZapScriptCmdLaunchSearch = "launch.search"
	ZapScriptCmdLaunchTitle  = "launch.title"

	ZapScriptCmdPlaylistPlay     = "playlist.play"
	ZapScriptCmdPlaylistStop     = "playlist.stop"
	ZapScriptCmdPlaylistNext     = "playlist.next"
	ZapScriptCmdPlaylistPrevious = "playlist.previous"
	ZapScriptCmdPlaylistGoto     = "playlist.goto"
	ZapScriptCmdPlaylistPause    = "playlist.pause"
	ZapScriptCmdPlaylistLoad     = "playlist.load"
	ZapScriptCmdPlaylistOpen     = "playlist.open"

	ZapScriptCmdExecute  = "execute"
	ZapScriptCmdDelay    = "delay"
	ZapScriptCmdEvaluate = "evaluate"
	ZapScriptCmdStop     = "stop"
	ZapScriptCmdEcho     = "echo"

	ZapScriptCmdMisterINI    = "mister.ini"
	ZapScriptCmdMisterCore   = "mister.core"
	ZapScriptCmdMisterScript = "mister.script"
	ZapScriptCmdMisterMGL    = "mister.mgl"

	ZapScriptCmdHTTPGet  = "http.get"
	ZapScriptCmdHTTPPost = "http.post"

	ZapScriptCmdInputKeyboard = "input.keyboard"
	ZapScriptCmdInputGamepad  = "input.gamepad"
	ZapScriptCmdInputCoinP1   = "input.coinp1"
	ZapScriptCmdInputCoinP2   = "input.coinp2"

	ZapScriptCmdUINotice = "ui.notice"
	ZapScriptCmdUIPicker = "ui.picker"

	ZapScriptCmdInputKey = "input.key" // DEPRECATED
	ZapScriptCmdKey      = "key"       // DEPRECATED
	ZapScriptCmdCoinP1   = "coinp1"    // DEPRECATED
	ZapScriptCmdCoinP2   = "coinp2"    // DEPRECATED
	ZapScriptCmdRandom   = "random"    // DEPRECATED
	ZapScriptCmdShell    = "shell"     // DEPRECATED
	ZapScriptCmdCommand  = "command"   // DEPRECATED
	ZapScriptCmdINI      = "ini"       // DEPRECATED
	ZapScriptCmdSystem   = "system"    // DEPRECATED
	ZapScriptCmdGet      = "get"       // DEPRECATED
)

type ZapScript struct {
	Name      *string        `json:"name"`
	Cmds      []ZapScriptCmd `json:"cmds"`
	ZapScript int            `json:"zapscript"`
}

type ZapScriptCmd struct {
	ID   string          `json:"id"`   // internal id of command instance
	Name *string         `json:"name"` // optional display name
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args"`
}

type CmdEvaluateArgs struct {
	ZapScript string `json:"zapscript" arg:"position=1"`
}

type CmdLaunchArgs struct {
	Launcher  *string `json:"launcher"`
	Name      *string `json:"name"`
	System    *string `json:"system"`
	URL       *string `json:"url"`
	PreNotice *string `json:"preNotice"`
	Path      string  `json:"path" arg:"position=1"`
}

type CmdNotice struct {
	Loader *bool  `json:"loader"`
	Text   string `json:"text" arg:"position=1"`
}

type CmdPicker struct {
	Items []ZapScript `json:"items"`
}
