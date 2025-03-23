package models

import "encoding/json"

const (
	ZapLinkActionZapScript = "zapscript"
	ZapLinkActionMedia     = "media"
)

const (
	ZapScriptCmdLaunch       = "launch"
	ZapScriptCmdLaunchSystem = "launch.system"
	ZapScriptCmdLaunchRandom = "launch.random"
	ZapScriptCmdLaunchSearch = "launch.search"

	ZapScriptCmdPlaylistPlay     = "playlist.play"
	ZapScriptCmdPlaylistNext     = "playlist.next"
	ZapScriptCmdPlaylistPrevious = "playlist.previous"

	ZapScriptCmdExecute = "execute"
	ZapScriptCmdDelay   = "delay"

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

type ZapLinkAction struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type ZapLink struct {
	Version int             `json:"version"`
	Name    string          `json:"name"`
	Actions []ZapLinkAction `json:"actions"`
}

type ZapScriptParams struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type MediaParams struct {
	Name      string  `json:"name"`
	System    string  `json:"system"`
	Url       *string `json:"url"`
	PreNotice *string `json:"preNotice"`
}
